<h1 align="center">Media Sequencer</h1>

<p align="center">
  <em>Multi-window media playback with runtime playlist edits and instant, clock-aligned sync across every screen.</em>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white">
  <img alt="React" src="https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black">
  <img alt="TypeScript" src="https://img.shields.io/badge/TypeScript-6-3178C6?logo=typescript&logoColor=white">
  <img alt="Postgres" src="https://img.shields.io/badge/Postgres-16-4169E1?logo=postgresql&logoColor=white">
  <img alt="Realtime" src="https://img.shields.io/badge/realtime-SSE-FF6F00">
</p>

Several display windows each play their own playlist continuously on a 5-hour
cycle. Playlists can be edited while they are running, and any media item can be
pushed to **every** window at the same instant.

The interesting constraint is that no window is ever *told* what to play. The
backend stores playlists and a couple of timeline anchors; every client works out
the current frame itself from `(window state, server time)`. Two tabs, two
devices, a late joiner and a hard refresh all land on the same item at the same
offset without exchanging a single message.

---

## Live URLs

Every deployment config file is committed and the exact click-path is in
[Deployment](#deployment). Fill these in once the stack is up:

| What | URL |
| --- | --- |
| Dashboard | `https://<your-app>.vercel.app` |
| Single window | `https://<your-app>.vercel.app/window/W1` |
| Backend health | `https://<your-api>.onrender.com/api/health` |

---

## What it does

- **Independent windows.** Each window owns a playlist and plays it back-to-back,
  looping until the cycle ends.
- **A 5-hour cycle.** Every window restarts at item 0 on the boundary, cutting
  off whatever straddles it. `CYCLE_MS` is configurable, which is what makes the
  behaviour demonstrable in under a minute.
- **Live playlist edits.** Add, remove or reorder items at runtime. Every open
  client reflects the change in about a second, and **the item on screen keeps
  playing** — same frame, same clock.
- **Global sync.** Push one media item to every window at a scheduled instant.
  Windows switch together, not in a ripple, and resume their own schedules when
  it ends.
- **Correct late joins.** Open a window mid-item or mid-sync and it starts at the
  right offset, seeking video to match, rather than restarting the clip.
- **Deliberate blanks.** Black appears only when a blank item is scheduled or a
  playlist is empty — never as an accidental gap between items.
- **Durable.** Playlists, edits and sync history live in Postgres and survive a
  restart or a redeploy.
- **Self-checking.** `?debug=1` exposes what each client computed;
  `GET /api/windows/{id}/now` returns what the server computes. Compare the two.

---

## How it works

```
┌──────────────────────────┐        REST (JSON)        ┌─────────────────────────┐
│ React (Vite, TS)         │ ────────────────────────▶ │ Go HTTP server          │
│  - WindowGrid            │                           │  - handlers (net/http)  │
│  - WindowPlayer x N      │ ◀──── SSE /api/events ─── │  - scheduler (pure fn)  │
│  - Controls (add, sync)  │                           │  - broadcaster (SSE hub)│
│  - scheduler.ts (pure)   │                           │  - store (Postgres)     │
└──────────────────────────┘                           └───────────┬─────────────┘
                                                                   │
                                                           ┌───────▼───────┐
                                                           │  PostgreSQL   │
                                                           └───────────────┘
```

The central design decision is that **the backend never pushes "the next item"**.
What a window shows is a pure function:

```
resolve(window, items, cycleMs, serverNow) -> { index, elapsedInItemMs, remainingMs, ... }
```

The server stores playlists plus a couple of timeline anchors; each client
fetches that state, estimates the server's clock, and computes locally what to
show. Every client for a window therefore computes the *same* answer, so extra
tabs, reloads, late joiners and separate devices line up with no coordination at
all — and a dropped message can never desynchronise anything. It can only make a
client's copy of the *playlist* briefly stale.

That same function exists twice —
[`backend/internal/scheduler/scheduler.go`](backend/internal/scheduler/scheduler.go)
and [`frontend/src/lib/scheduler.ts`](frontend/src/lib/scheduler.ts) — as a
line-for-line port. They are kept in step by hand, so
[`GET /api/windows/{id}/now`](#get-apiwindowsidnow) returns the server's own
`resolve()` result: comparing it with the `?debug=1` overlay shows immediately
whether the two agree.

### Playback and the cycle

Each window has a `cycle_epoch` (unix ms). The cycle containing an instant `t`
starts at

```
cycleStart(t) = cycle_epoch + floor((t - cycle_epoch) / CYCLE_MS) * CYCLE_MS
```

Floor division (not truncation) so cycles tile evenly even for `t < epoch`.

Within a cycle the playlist runs back-to-back and loops. Resolving walks the list
from the starting index, then uses `elapsed % totalDuration` to skip every
complete loop in one step — so the cost is O(list length), not O(elapsed),
whether the window has been running for a second or a month.

**At every boundary the list restarts at item 0**, even if the last loop was
part-way through. This is implemented by capping `remainingMs` at the cycle end:
the item straddling the boundary is simply cut off there. A playlist longer than
the cycle therefore has a tail that never plays — a consequence of the restart
rule, not a bug.

**Blank** appears only when it is genuinely correct:

- a playlist item whose media `type` is `blank` (a normal item with a duration),
- an empty playlist, which shows blank for the remainder of the cycle, or
- media that failed to load — the panel goes neutral and names the item in debug
  mode, while the schedule keeps running underneath so the next item arrives on
  time.

As long as a playlist has one item with a positive duration, `resolve` always
returns an item. The rest of the cycle can never silently go dark.

**Video.** Videos are `muted` (browsers block autoplay with sound), never use
`loop`, and are joined at `currentTime = (serverNow - itemStartedAt) / 1000`
rather than at 0 — which is what makes a reload or a late joiner land on the same
frame as everyone else. A check every 2 s re-seeks if the element has drifted
more than 0.5 s from the schedule. If a clip is shorter than its configured
duration, its last frame is held until the schedule advances. The `<img>` /
`<video>` element is keyed on *segment identity*
(`windowId + itemId + startedAt`), so the 250 ms render tick never recreates it —
without that, every video would restart four times a second. The next item is
preloaded (`new Image()` / a hidden `<video preload="auto">`) so switches have no
gap.

### Sync across every window

`POST /api/sync { mediaId, durationMs? }` writes a row:
`{ id, media_id, start_at, end_at, cancelled_at }`.

**Lead time.** `start_at = serverNow + SYNC_LEAD_MS` (default 1500 ms). The sync
does not begin "now", it begins a moment in the future. That is the whole trick:
the SSE event reaches every client *before* the switch is due, so all of them
flip at the same wall-clock instant instead of "whenever my message arrived".

**Server clock offset.** Client clocks cannot be trusted, so
[`frontend/src/lib/clock.ts`](frontend/src/lib/clock.ts) runs a small NTP-style
handshake against `GET /api/time`: five samples,
`offset = serverTime - (t0+t1)/2`, keeping the sample with the smallest round
trip (the fastest exchange has the least room for error). It re-syncs every 60 s.
All scheduling uses `serverNow() = Date.now() + offset`.

**Client rule.** If `start_at <= serverNow < end_at` and the sync is not
cancelled, show the sync media (video at offset `(serverNow - start_at)/1000`);
otherwise fall back to the window's normal `resolve`.

**Replacement.** A new sync cancels any active or pending one (`cancelled_at` is
set on the old row) — there is only ever one sync in flight.
`DELETE /api/sync/active` ends it early.

**Afterwards**, the underlying timeline has kept running the whole time
(wall-clock semantics), so each window simply shows whatever its own schedule
says for that moment. Playlists are never touched by a sync, so this needs no
extra state and stays deterministic. The active sync is part of `GET /api/state`,
so a client arriving mid-sync joins at the correct offset instead of starting the
clip over.

### Live edits, and why nothing jumps

The subtle part of a runtime playlist edit is not the update, it is *not
disturbing what is on screen*. If the server simply recomputed from `cycleStart`
after an edit, inserting an item would shift every index and each screen would
jump to different media mid-item.

So every playlist mutation goes through `store.mutatePlaylist`, which does all of
this in **one transaction**, with the window row locked:

1. Read the current list and `resolve()` it — this gives the item on screen and
   its start time `s = now - elapsedInItem`.
2. Apply the mutation.
3. Re-read the list. If the item that was playing still exists (matched on
   `playlist_items.id`, **not** `media_id` — the same media may appear several
   times in one list), set `anchor_at = s`, `anchor_index = j`. It keeps playing
   uninterrupted, and the *new* list continues from `j+1`.
4. If it was removed, set `anchor_at = now`,
   `anchor_index = min(oldIndex, len-1)` so the replacement starts immediately.
5. Bump `version` and broadcast `window.updated`.

Anchors are scoped to the cycle they were set in — `resolve` ignores an anchor
whose `anchor_at` is before the current `cycleStart` (or in the future), so the
next cycle boundary still restarts cleanly at item 0.

The cases that matter are append, insert-before-current, delete-another and
delete-current: in the first three the current item's id and start time must be
unchanged, and in the last the replacement starts immediately at `now`.

### Realtime transport

`GET /api/events` is a Server-Sent Events stream emitting `window.updated`,
`media.created`, `sync.started` and `sync.cancelled`. Clients treat every event
the same way: re-fetch `GET /api/state`. The payload is small, and "always re-read
the truth" is far easier to get right than applying incremental patches. A
`: ping` comment every 20 s stops hosting proxies closing the idle stream, and
after 3 consecutive failures the client gives up on SSE and polls `/api/state`
every 5 s instead. The status dot in the top bar shows which mode it is in.

---

## Quick start

Prerequisites: **Go 1.25+**, **Node 20+**, **Docker Desktop**.

```powershell
# 1. Database
docker compose up -d

# 2. Backend  (new terminal)
cd backend
Copy-Item .env.example .env
go run ./cmd/server
#  -> listening on :8080, migrations applied, seed inserted once

# 3. Frontend (new terminal)
cd frontend
Copy-Item .env.example .env.local
npm install
npm run dev
#  -> http://localhost:5173
```

<details>
<summary>bash / zsh equivalent</summary>

```bash
docker compose up -d

cd backend && cp .env.example .env && go run ./cmd/server

cd frontend && cp .env.example .env.local && npm install && npm run dev
```

</details>

The real cycle is 5 hours, which is not something you can sit and watch. To see
the boundary restart, run the backend with the cycle compressed to a minute:

```powershell
cd backend
$env:CYCLE_MS = "60000"; go run ./cmd/server
```

> This is a **local, temporary** override set in one shell. The committed default
> is 5 hours (`CYCLE_MS=18000000` in `config.go`, `.env.example` and
> `render.yaml`) and that is what deploys. Close the terminal and it is gone.

### Using your own media

Media is referenced by URL; the database stores only metadata (id, name, type,
url, duration) and the browser fetches the bytes itself. The backend never sees a
pixel.

For files with no public URL, drop them in `frontend/public/media/`. Vite (dev)
and Vercel/Netlify (prod) both serve that folder at `/media/`, same origin as the
app and with HTTP range support — which matters, because seeking to a mid-clip
offset is exactly what a late joiner or a mid-sync reload does.

Then create the media with a site-root path rather than a full URL:

```
/media/clip.mp4
```

Stored that way, one row works in every environment: the browser resolves the
path against whatever origin the page is on, so the same value means
`http://localhost:5173/media/clip.mp4` locally and
`https://<your-app>.vercel.app/media/clip.mp4` once deployed.

Keep the files small — they are committed to git and shipped in the frontend
deploy. There is deliberately **no upload endpoint**: uploading from a browser
would need somewhere durable to put the bytes, and the free hosting this targets
has an ephemeral filesystem, so durable uploads belong behind object storage
(S3/R2) — a clean addition when it is wanted, and out of scope for the scheduler
itself.

---

## Debugging

Add `?debug=1` to any page — `http://localhost:5173/?debug=1` or
`/window/W1?debug=1` — to expose what the client is actually computing. It is the
fastest way to see whether a window is where it should be.

**Top bar** shows the clock estimate:

```
offset +12ms · rtt 3ms · cycle 18000s · 09:41:07.482
```

| Field | Meaning |
| --- | --- |
| `offset` | `serverNow() - Date.now()`, the correction applied to this machine's clock. Large values are fine; *unstable* values are the warning sign. |
| `rtt` | Round trip of the best `/api/time` sample. The offset is only as good as this is small. |
| `cycle` | `CYCLE_MS` in seconds, straight from `/api/state`. `18000s` = the real 5h cycle; `60s` = the demo override. |
| clock | The current server time this client believes in. Two devices showing the same value are genuinely in step. |

**Per window**, an overlay in the corner of the stage:

```
item   1 / 3
media  M2 (item)
elapsed 9726ms
left    274ms
cycle   25.6%
anchor  1789795210000 / 2
v4
```

| Field | Meaning |
| --- | --- |
| `item` | Resolved index within the playlist. |
| `media` | Media id, and the segment kind: `item` (normal playback), `sync` (a global sync is showing), or `blank` (empty playlist). |
| `elapsed` / `left` | Position within the current item. `left` is capped at the cycle boundary, so it is what actually drives the next switch. |
| `cycle` | How far through the cycle this window is. At 100% it wraps to 0% and the playlist restarts at item 0. |
| `anchor` | `anchorAt / anchorIndex`. `- / -` means clean playback from the cycle start; values appear after a playlist edit and disappear again at the next cycle boundary. |
| `v` | The window's version, bumped on every mutation. If this does not change after an edit, the SSE event did not arrive. |

To check the browser against the server, compare this overlay with
[`GET /api/windows/{id}/now`](#get-apiwindowsidnow) — `item` and `elapsed` should
match to within the time between the two observations:

```powershell
Invoke-RestMethod http://localhost:8080/api/windows/W1/now | ConvertTo-Json -Depth 5
```

Run it after touching either `resolve()` implementation and the two ports are
confirmed in agreement on live data, not just on paper.

<details>
<summary><b>Manual verification checklist</b></summary>

With `CYCLE_MS=60000` and `http://localhost:5173/?debug=1` open:

| # | Check | Expected |
| --- | --- | --- |
| 1 | Watch one window for a minute | The playlist loops back-to-back with no gaps and no blank frames |
| 2 | Watch across a minute boundary | Whatever was playing is cut off and item 0 starts immediately |
| 3 | Add media to a window mid-item | The current item keeps playing to its natural end; the new list continues afterwards |
| 4 | Open `/window/W1` in a second tab | Both tabs show the same item at the same offset |
| 5 | Start a sync with two tabs open | Both switch together; when it ends each resumes its own schedule |
| 6 | Reload during a sync | The sync media is showing, and a video is at the right offset |
| 7 | Stop and restart the backend | Playlists and any edits are still there |

</details>

---

## API

Base path `/api`. JSON in and out, camelCase. Errors are
`{ "error": { "code": "...", "message": "..." } }` with an appropriate status.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/health` | `{ "status": "ok" }` — used by host health checks |
| GET | `/api/time` | `{ "serverTimeMs" }` — the reference clock |
| GET | `/api/state` | Everything a client needs, in one payload |
| GET | `/api/media` | Media library |
| POST | `/api/media` | Create media |
| GET | `/api/windows` | Windows with playlists |
| POST | `/api/windows` | Create a window |
| POST | `/api/windows/{id}/items` | **Add media to a window's list** |
| DELETE | `/api/windows/{id}/items/{itemId}` | Remove an item |
| PUT | `/api/windows/{id}/items/order` | Reorder a playlist |
| GET | `/api/windows/{id}/now` | Server-side `resolve()` result, for debugging |
| POST | `/api/sync` | Start a sync |
| GET | `/api/sync/active` | Active or pending sync, or `null` |
| DELETE | `/api/sync/active` | Cancel the current sync |
| GET | `/api/events` | SSE stream |

### `GET /api/state`

```json
{
  "serverTimeMs": 1758260000000,
  "cycleMs": 18000000,
  "media": [
    { "id": "M1", "name": "City skyline", "type": "image", "url": "https://…", "durationMs": 10000 }
  ],
  "windows": [
    {
      "id": "W1", "name": "Window 1", "cycleEpoch": 1758240000000,
      "anchorAt": null, "anchorIndex": null, "version": 3, "position": 0,
      "items": [{ "id": 12, "mediaId": "M1", "position": 0, "durationMs": 10000 }]
    }
  ],
  "activeSync": { "id": 4, "mediaId": "M2", "startAt": 1758260001500, "endAt": 1758260031500 }
}
```

### `GET /api/windows/{id}/now`

The server's own `resolve()` answer for a window, at the instant of the request.
Read-only, no side effects. It exists so the browser's computation can be checked
against the backend's — they run the same algorithm in two languages, so any
disagreement is a bug in one of the ports.

```jsonc
// GET /api/windows/W1/now  -> 200
{
  "windowId": "W1",
  "serverTimeMs": 1789798624041,
  "cycleMs": 18000000,
  "resolved": {
    "blank": false,
    "index": 1,
    "itemId": 21,
    "mediaId": "M2",
    "elapsedInItemMs": 9726,
    "remainingMs": 274,
    "startedAtMs": 1789798614315,
    "cycleStartMs": 1789794000000,
    "cycleEndMs": 1789812000000
  },
  "activeSync": null
}
```

| Field | Meaning |
| --- | --- |
| `resolved.blank` | `true` only for an empty playlist. A *blank media item* is a normal item and reports `false`. |
| `resolved.index` | Playlist position, `-1` when `blank`. |
| `resolved.itemId` | `playlist_items.id` — the identity the re-anchoring matches on, not `mediaId`. |
| `resolved.remainingMs` | Capped at `cycleEndMs`, so an item straddling the boundary is cut off there. |
| `resolved.startedAtMs` | `serverTimeMs - elapsedInItemMs`. Unchanged across a playlist edit is exactly the no-jump guarantee. |
| `activeSync` | The sync overriding normal playback, or `null`. `resolved` always reports the *underlying* schedule, even mid-sync. |

Unknown window → `404`.

<details>
<summary><b>Mutating endpoints, with payloads</b></summary>

**`POST /api/media`** — `id` is optional and auto-assigned as `M<n>`.

```jsonc
// request
{ "name": "Promo clip", "type": "video", "url": "https://cdn.example/promo.mp4", "durationMs": 12000 }
// 201
{ "id": "M7", "name": "Promo clip", "type": "video", "url": "https://cdn.example/promo.mp4", "durationMs": 12000 }
```

**`POST /api/windows`**

```jsonc
// request
{ "name": "Lobby screen" }
// 201
{ "id": "W5", "name": "Lobby screen", "cycleEpoch": 1758258000000,
  "anchorAt": null, "anchorIndex": null, "version": 1, "position": 4, "items": [] }
```

**`POST /api/windows/{id}/items`** — `position` omitted appends; `durationMs`
omitted uses the media's own length. Returns the whole window, re-anchored.

```jsonc
// request
{ "mediaId": "M5", "position": 0 }
// 201
{ "id": "W1", "name": "Window 1", "cycleEpoch": 1758240000000,
  "anchorAt": 1758260005000, "anchorIndex": 2, "version": 4, "position": 0,
  "items": [
    { "id": 31, "mediaId": "M5", "position": 0, "durationMs": 12000 },
    { "id": 12, "mediaId": "M1", "position": 1, "durationMs": 10000 },
    { "id": 13, "mediaId": "M2", "position": 2, "durationMs": 10000 }
  ] }
```

Note `anchorAt` / `anchorIndex` in that response: `M2` was playing at index 1
before the insert and is still playing, now at index 2, with its original start
time — that is the no-jump guarantee in action.

**`DELETE /api/windows/{id}/items/{itemId}`** → `200` with the updated window.

**`PUT /api/windows/{id}/items/order`** — `itemIds` must be exactly the window's
current item ids, reordered.

```jsonc
// request
{ "itemIds": [13, 12, 31] }
// 200 -> the updated window
```

**`POST /api/sync`** — `durationMs` defaults to the media's own length and must
be between 1 s and 1 h.

```jsonc
// request
{ "mediaId": "M2", "durationMs": 30000 }
// 201
{ "id": 4, "mediaId": "M2", "startAt": 1758260001500, "endAt": 1758260031500 }
```

**`DELETE /api/sync/active`** → `200 { "cancelled": true }` (`false` if there was
nothing to cancel).

</details>

<details>
<summary><b>Validation rules</b></summary>

- Unknown `mediaId` or window → `404`.
- Bad type or non-positive duration → `400`.
- A media URL must be either an absolute `http(s)://` address **or** a site-root
  path such as `/media/clip.mp4` (see
  [using your own media](#using-your-own-media)). Bare relative paths
  (`media/clip.mp4`) and protocol-relative ones (`//host/clip.mp4`, which
  silently points at another origin) are rejected.
- `blank` media must have no URL; `image` / `video` must have one.
- Sync `durationMs` outside 1 s–1 h → `400`.
- Unknown JSON fields are rejected, so a typo in a payload is a `400` rather than
  being silently ignored.

</details>

---

## Data model

<details>
<summary><b>Schema</b> — four tables, plain SQL migrations</summary>

```sql
CREATE TABLE media (
  id           TEXT PRIMARY KEY,              -- human-readable, e.g. 'M1'
  name         TEXT NOT NULL,
  type         TEXT NOT NULL CHECK (type IN ('image','video','blank')),
  url          TEXT,                          -- NULL for blank
  duration_ms  BIGINT NOT NULL CHECK (duration_ms > 0),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK ((type = 'blank') = (url IS NULL))
);

CREATE TABLE windows (
  id            TEXT PRIMARY KEY,             -- e.g. 'W1'
  name          TEXT NOT NULL,
  cycle_epoch   BIGINT NOT NULL,              -- unix ms
  anchor_at     BIGINT,                       -- unix ms, nullable
  anchor_index  INT,
  version       BIGINT NOT NULL DEFAULT 1,
  position      INT NOT NULL DEFAULT 0        -- display order in the grid
);

CREATE TABLE playlist_items (
  id          BIGSERIAL PRIMARY KEY,
  window_id   TEXT NOT NULL REFERENCES windows(id) ON DELETE CASCADE,
  media_id    TEXT NOT NULL REFERENCES media(id),
  position    INT  NOT NULL,                  -- 0-based, contiguous
  duration_ms BIGINT CHECK (duration_ms > 0), -- optional per-item override
  UNIQUE (window_id, position)
);

CREATE TABLE syncs (
  id            BIGSERIAL PRIMARY KEY,
  media_id      TEXT NOT NULL REFERENCES media(id),
  start_at      BIGINT NOT NULL,              -- unix ms
  end_at        BIGINT NOT NULL,
  cancelled_at  BIGINT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

</details>

- The same media may appear several times in one playlist, so item identity is
  `playlist_items.id`, never `media_id`. Re-anchoring matches on that id.
- Effective duration is `COALESCE(playlist_items.duration_ms, media.duration_ms)`.
- Every timestamp the scheduler touches is an **int64 of unix milliseconds** in
  both Go and TypeScript — no `time.Time` on the wire, so there is no timezone or
  serialisation ambiguity to get wrong.
- Position rewrites happen inside the mutation transaction, in two steps (shift
  everything by `+10000`, then write final positions) so
  `UNIQUE (window_id, position)` is never transiently violated.
- Migrations are plain `.sql` files embedded with `go:embed`, applied in filename
  order on startup and recorded in `schema_migrations`. No migration tool needed.

---

## Configuration

`backend/.env.example`:

```
PORT=8080
DATABASE_URL=postgres://postgres:postgres@localhost:5432/sequencer?sslmode=disable
ALLOWED_ORIGINS=http://localhost:5173
CYCLE_MS=18000000
SYNC_LEAD_MS=1500
SEED_ON_START=true
```

`frontend/.env.example`:

```
VITE_API_BASE_URL=http://localhost:8080
```

No secrets are committed. The backend reads a local `.env` if present, but real
environment variables always win, which is what hosting platforms expect. `VITE_`
variables are baked in at **build** time, so changing one needs a redeploy.

---

## Deployment

Frontend on **Vercel**, Go backend on **Render**, Postgres on **Neon**. All the
config is committed: [`backend/Dockerfile`](backend/Dockerfile),
[`render.yaml`](render.yaml), [`frontend/vercel.json`](frontend/vercel.json),
[`frontend/public/_redirects`](frontend/public/_redirects) (Netlify).

> **Why the backend is not on Vercel.** `/api/events` is a Server-Sent Events
> stream held open by an in-memory hub (`events/hub.go`). Every Vercel function
> invocation is a separate instance with its own memory, so a `POST` that
> published an event would be running somewhere other than the process holding
> the client's stream, and no client would ever receive it. Realtime would
> silently fall back to 5 s polling, and sync's 1.5 s lead time would be missed.
> The backend needs one long-lived process: Render, Fly.io and Railway all work.

**On Docker:** [`backend/Dockerfile`](backend/Dockerfile) is for **Render only**.
Vercel does not build or run Dockerfiles — it is not a container host. The
frontend goes to Vercel as a static Vite build (`dist/`), which is why there is
no frontend Dockerfile and no use for one. The Go image is multi-stage
(`golang:1.25-alpine` → `distroless/static`, `CGO_ENABLED=0`, non-root);
migrations are embedded with `go:embed`, so it needs no extra files at runtime.

<details>
<summary><b>1. Database — Neon</b></summary>

Create a free project at [neon.tech](https://neon.tech) (region closest to your
Render region). On the project dashboard, **Connect** gives you a connection
string — there are two, and for this app the choice matters.

**Use the direct string, not the pooled one.** Neon offers a pooled endpoint
(hostname containing `-pooler`, routed through PgBouncer in transaction mode) and
a direct one (no `-pooler`). Take the **direct** one:

```
postgres://USER:PASSWORD@ep-xxx-123456.us-east-2.aws.neon.tech/neondb?sslmode=require
                          ^^^^^^^^^^^^^^^^^^ no "-pooler" here
```

Three reasons, specific to this service:

- **Migrations run at startup.** `store.Migrate()` executes DDL inside a
  transaction on every boot. Schema changes want a session, and PgBouncer in
  transaction mode does not give you a stable one.
- **pgx caches prepared statements.** pgx/v5's default exec mode prepares and
  reuses statements; behind a transaction-mode pooler that surfaces as
  `prepared statement "stmtN" already exists` — an error that never mentions
  pooling, so it is a miserable one to debug. (Pin
  `?default_query_exec_mode=exec` if you ever *must* go through the pooler.)
- **Pooling buys nothing here.** This is one long-lived Render process holding
  its own `pgxpool`. PgBouncer exists for serverless runtimes that open a
  connection per request; that is the opposite of this architecture.

`sslmode=require` is mandatory — Neon refuses plaintext connections.

> **Cold starts compound.** Neon suspends an idle compute after ~5 minutes
> (scale-to-zero) and Render's free tier sleeps too. After a quiet spell the
> first request can wait for *both* to wake. Storage is untouched — nothing is
> lost, it is just slow once. Load the dashboard a minute before demoing.

</details>

<details>
<summary><b>2. Backend — Render</b></summary>

New → Web Service → your repo, then:

| Setting | Value |
| --- | --- |
| Runtime | Docker |
| Root directory | `backend` |
| Dockerfile path | `./Dockerfile` |
| Health check path | `/api/health` |

Environment variables:

| Variable | Value |
| --- | --- |
| `DATABASE_URL` | the Neon string, with `sslmode=require` |
| `ALLOWED_ORIGINS` | the exact frontend origin, e.g. `https://media-sequencer.vercel.app` |
| `CYCLE_MS` | `18000000` (5 hours) |
| `SYNC_LEAD_MS` | `1500` |
| `SEED_ON_START` | `true` |
| `PORT` | set by Render automatically — do not override |

`ALLOWED_ORIGINS` is a chicken-and-egg: deploy the frontend first to learn its
URL, or set it afterwards and let Render redeploy.

</details>

<details>
<summary><b>3. Frontend — Vercel</b></summary>

New Project → your repo, then:

| Setting | Value |
| --- | --- |
| Framework preset | Vite (auto-detected) |
| **Root directory** | **`frontend`** — this one is easy to miss |
| Build command | `npm run build` (from `vercel.json`) |
| Output directory | `dist` (from `vercel.json`) |

One environment variable:

| Variable | Value |
| --- | --- |
| `VITE_API_BASE_URL` | the Render URL, e.g. `https://media-sequencer-api.onrender.com` — no trailing slash |

The committed `vercel.json` rewrites every path that is not `/assets/…`,
`/media/…` or the favicon to `index.html`, so `/window/W1` survives a hard
refresh instead of 404ing.

</details>

**After deploying**, check:

- `ALLOWED_ORIGINS` is the exact frontend origin (scheme and host, no trailing
  slash).
- SSE works through the host — open the dashboard and confirm the status dot says
  "live (SSE)" and stays there for more than a minute (that proves the 20 s
  heartbeat is getting through).
- The seed ran exactly once: the backend log says `seed applied` on the first
  boot and `seed skipped, database already has data` afterwards.

---

## Demo script

<details>
<summary><b>Nine steps that exercise every requirement in order</b></summary>

Have the dashboard open at `?debug=1`, and `/window/W1?debug=1` in a second tab
(or on a phone) beside it. Steps 2 and 3 need the compressed cycle
(`CYCLE_MS=60000`); everything else works against the deployed 5h build.

**1. Each window plays its own list, continuously and in order.**
Watch the four windows for half a minute. Each advances through its own playlist
back to back. The playlist under each window highlights the current item, and the
progress bar tracks it. Nothing stutters and nothing goes black between items —
the next item is preloaded before the switch.

**2. The cycle restarts at item 0.** *(needs `CYCLE_MS=60000`)*
Watch the `cycle` percentage in a window's overlay climb toward 100%. At the
boundary, whatever was mid-item is cut off and `item` jumps straight back to
`0 / n`. That is the 5h rule, compressed.

**3. Blank appears only when it is meant to.**
W2's playlist is `M2 -> B -> M4`. The black panel lasts exactly the 5 seconds
that `B` is scheduled for, then M4 starts. At no other point does any window go
blank — that is the requirement about the rest of the cycle never silently going
dark.

**4. Two screens agree.**
Compare the dashboard's W1 tile against the `/window/W1` tab. Same media, same
progress, same `elapsed` in the overlay. Neither tab told the other anything;
both computed it from `(window state, server time)`. Reload one — it comes back
mid-item, in the right place, not at the start.

**5. Add media at runtime, without a jump.**
Note what W1 is playing and its `elapsed`. In **Add media to a window**, pick W1,
pick any media, set **Position** to `0`, and submit. Within about a second: the
item list updates in every open tab, `v` increments, and `anchor` appears in the
overlay — but the item on screen keeps playing, with `elapsed` still climbing
from where it was. The new item is now index 0 and will play on the next pass.

**6. Sync every window to one item.**
In **Sync all windows**, pick `M2` and press the button. Every window — the four
tiles and the separate `/window/W1` tab — switches to M2 together, each showing
the `SYNC` badge. The switch is scheduled 1.5 s ahead so the event reaches every
client before it is due, which is why they move together rather than in a ripple.

**7. Join a sync late, at the right offset.**
While the sync is still running, reload the `/window/W1` tab. It comes back
already showing the sync media, at the correct offset — a video resumes part-way,
it does not restart. The active sync is part of `/api/state`, so a late joiner
has everything it needs.

**8. Sync ends, each window returns to its own schedule.**
When the sync finishes, every window resumes — not where it was interrupted, but
wherever its own timeline has reached in the meantime. The schedule kept running
underneath. Press **Cancel sync** mid-way to see the same thing happen early.

**9. It survives a restart.**
Stop the backend and start it again. The log says
`seed skipped, database already has data`, and the playlist edit from step 5 is
still there.

</details>

---

## Design notes

<details>
<summary><b>Assumptions</b></summary>

- **The cycle is per window**, aligned to a stored `cycle_epoch`. The seeded
  windows all share one epoch (the start of the current UTC day), so they flip
  together; nothing requires that.
- **The list restarts at item 0 at every cycle boundary**, truncating any partial
  loop. An item straddling the boundary is cut off, not allowed to finish.
- **A playlist longer than the cycle** has a tail that never plays.
- **Images and blanks have configured durations**; videos use their real length
  unless a per-item `durationMs` override is given. The create-media form can
  read a video's real duration from its metadata.
- **Sync replaces any existing sync**, and the timeline keeps running underneath
  it (wall clock) — there is no pause-and-resume.
- **Seed data is swappable in one file.** It lives entirely in
  [`backend/internal/store/seed.go`](backend/internal/store/seed.go) in two
  tables (`seedMedia`, `seedWindows`); swapping in real lists is a one-file edit
  and nothing else depends on those values. Seeding is idempotent — it only runs
  when both tables are empty — so restarts never duplicate or overwrite data.
- **Single backend instance.** The SSE hub is in-memory.
- **Videos are muted**, because browsers will not autoplay audio.
- **React 19**, the current Vite default. Nothing in the app depends on the
  major version, so it runs unchanged on React 18.

</details>

<details>
<summary><b>Tradeoffs</b></summary>

**Wall-clock sync vs pause-and-resume.** When a sync ends, each window shows
whatever its schedule says for that moment — it does *not* resume where it was
interrupted. Pause-and-resume would need per-window resume state, and, worse,
every window would then be permanently offset from its cycle by the accumulated
sync time, so windows would drift out of alignment with each other and with the
cycle boundary. The wall-clock rule needs no extra state, is deterministic, and
keeps all the cycle guarantees intact.

**SSE vs WebSockets.** Every realtime message here goes server→client, which is
exactly what SSE is for. `EventSource` reconnects on its own, it is plain HTTP so
it survives proxies that dislike upgrades, and the server side is a loop writing
to an `http.ResponseWriter`. WebSockets would add a dependency and a handshake to
buy bidirectionality we never use. The cost is the 6-connections-per-origin limit
on HTTP/1.1 and the need for an explicit heartbeat — both handled.

**Client-side computation vs server-pushed items.** Pushing "show item 3 now"
from a timer on the server is the obvious design and it is the wrong one: it
makes correctness depend on message delivery and latency, drifts over hours, and
every reconnect or late joiner needs special handling. Making the current item a
pure function of `(state, time)` moves the problem to clock agreement, which is a
smaller and much better-understood problem — solved here with a ~40-line
NTP-style offset estimate. The cost is that clients must be trusted to compute
correctly, which is why `GET /api/windows/{id}/now` exists: it returns the
server's own answer so the two can be compared at any moment.

**Postgres vs SQLite.** SQLite would remove a moving part, but the free hosting
tiers this targets have ephemeral filesystems, so a SQLite file would vanish on
every redeploy — and persistent playlists are an explicit requirement. Managed
Postgres is free at this size and survives restarts.

**Re-fetching all state on every event vs incremental patches.** Events carry
only a nudge; clients re-read `GET /api/state`. Patches would be smaller, but
every patch is a chance to apply an update out of order and leave a client
silently wrong. The whole payload is a few kilobytes, so the simple option wins
until it measurably does not.

**A live cross-check instead of a mocked one.** The scheduler is the part worth
verifying, and it exists in two languages. Rather than assert both against fixed
vectors, `GET /api/windows/{id}/now` publishes the server's own answer so the
browser's computation can be compared against it on real data at any moment —
including in production, against the real clock, mid-cycle. The manual checklist
above walks the same ground for the parts that are visual.

</details>

---

## Repository layout

```
backend/                 Go service
  cmd/server/            entrypoint
  internal/config/       env parsing + a tiny .env loader
  internal/model/        shared types
  internal/scheduler/    resolve() / cycleStart() / reanchor() — pure, no I/O
  internal/store/        pgx, embedded SQL migrations, idempotent seed
  internal/events/       in-memory SSE hub
  internal/api/          handlers, routing, middleware
frontend/                React + Vite + TypeScript
  src/lib/clock.ts       NTP-style server clock offset
  src/lib/scheduler.ts   line-for-line port of the Go scheduler
  src/lib/playback.ts    window + schedule + sync -> what is on screen
  src/hooks/             one 250 ms heartbeat, app state over SSE
  src/components/        WindowGrid, WindowPlayer, MediaView, controls
  public/media/          drop locally-hosted media here
docker-compose.yml       Postgres for local development
render.yaml              Render blueprint for the backend
```

No ORM, no router library, no state-management library — `net/http` with Go 1.22+
routing patterns on the backend, React with hooks on the frontend. The only
runtime dependencies are `pgx/v5` and `react-router-dom`.
