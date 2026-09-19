# Multi-Window Media Sequencer with Sync Playback

Several display windows each play their own playlist continuously on a 5-hour
cycle. Playlists can be edited while they are running, and any media item can be
pushed to **every** window at the same instant.

---

## 1. Live URLs

> **Not deployed yet.** Deployment needs account logins (Neon, Render, Vercel),
> so it is left to a human — all the config files are committed and the exact
> steps are in [section 9](#9-deployment). Fill these in once deployed:

| What | URL |
| --- | --- |
| Frontend | `https://<your-app>.vercel.app` |
| Backend health | `https://<your-api>.onrender.com/api/health` |
| Single window | `https://<your-app>.vercel.app/window/W1` |

### How to demo (3 lines)

1. Open the dashboard — four windows play their own playlists, each with a
   progress bar and its playlist below it, current item highlighted.
2. Pick a media item under **Sync all windows** and press the button — every
   window (including `/window/W1` open on your phone) switches within ~100 ms,
   then each returns to its own schedule.
3. Use **Add media to a window** — the new item appears in every open tab within
   about a second, and the item currently on screen does **not** jump.

---

## 2. Overview

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
all — and a dropped message can never desynchronise anything, it can only make a
client's copy of the *playlist* briefly stale.

That same function exists twice — [`backend/internal/scheduler/scheduler.go`](backend/internal/scheduler/scheduler.go)
and [`frontend/src/lib/scheduler.ts`](frontend/src/lib/scheduler.ts) — and both
are tested against one shared file, [`testdata/schedule_vectors.json`](testdata/schedule_vectors.json),
so they cannot drift apart.

### Repository layout

```
backend/    Go service: config, model, scheduler (pure), store, events, api
frontend/   React + Vite + TypeScript
testdata/   schedule_vectors.json - shared by the Go and TS test suites
```

---

## 3. Local setup (PowerShell)

Prerequisites: Go 1.23+, Node 20+, Docker Desktop.

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

Add `?debug=1` to any page for the clock-offset/RTT readout and a per-window
overlay showing the resolved index, elapsed/remaining ms and the anchor.

### Tests

```powershell
# Go
cd backend
go test ./...

# TypeScript (runs the same vectors)
cd frontend
npm test
```

There is also an **opt-in integration test** that checks the TypeScript
scheduler against the Go one on live data, by comparing it with
`GET /api/windows/{id}/now` for every window. With the backend running:

```powershell
cd frontend
$env:API_BASE = "http://localhost:8080"; npm test
```

> **Windows note.** On machines with Smart App Control / an Application Control
> policy enabled, `go test ./...` can fail with
> *"An Application Control policy has blocked this file"*. That is the OS
> refusing to execute the throw-away `.exe` that `go test` compiles into a temp
> folder — the tests themselves are fine. Run `.\backend\test.ps1` instead; it
> compiles each test binary to a stable path and runs it.

### Manual verification script

Run the backend with a 1-minute cycle so the 5-hour behaviour is observable:

```powershell
cd backend
$env:CYCLE_MS = "60000"; go run ./cmd/server
```

Then check, with `http://localhost:5173/?debug=1` open:

| # | Check | Expected |
| --- | --- | --- |
| 1 | Watch one window for a minute | The playlist loops back-to-back with no gaps and no blank frames |
| 2 | Watch across a minute boundary | Whatever was playing is cut off and item 0 starts immediately |
| 3 | Add media to a window mid-item | The current item keeps playing to its natural end; the new list continues afterwards |
| 4 | Open `/window/W1` in a second tab | Both tabs show the same item at the same offset |
| 5 | Start a sync with two tabs open | Both switch together; when it ends each resumes its own schedule |
| 6 | Reload during a sync | The sync media is showing, and a video is at the right offset |
| 7 | Stop and restart the backend | Playlists and any edits are still there |

---

## 4. How playback works

**Cycle.** Each window has a `cycle_epoch` (unix ms). `CYCLE_MS` is 5 hours
(`18_000_000`) and is configurable, mainly so the 1-minute demo above is
possible. The cycle containing an instant `t` starts at

```
cycleStart(t) = cycle_epoch + floor((t - cycle_epoch) / CYCLE_MS) * CYCLE_MS
```

Floor division (not truncation) so cycles tile evenly even for `t < epoch`.

**Within a cycle** the playlist runs back-to-back and loops. Resolving walks the
list from the starting index, then uses `elapsed % totalDuration` to skip all the
complete loops in one step — so the computation is O(list length), not O(elapsed),
whether the window has been running for a second or a month.

**At every boundary the list restarts at item 0**, even if the last loop was
mid-way. This is implemented by capping `remainingMs` at the cycle end: the item
straddling the boundary is simply cut off there.

**Blank** appears only when it is genuinely correct:

- a playlist item whose media `type` is `blank` (a normal item with a duration), or
- an empty playlist, which shows blank for the remainder of the cycle, or
- media that failed to load — the panel goes neutral and shows the name in debug
  mode, but the schedule keeps running underneath, so the next item arrives on time.

The rest of the cycle can never silently become blank: as long as the playlist
has at least one item with a positive duration, `resolve` always returns an item.

**Playlists longer than 5 hours** have a tail that never plays inside a cycle,
because the cycle restarts at item 0 before reaching it. This is a consequence of
the "restart every cycle" requirement, not a bug — vector *"playlist longer than
the cycle leaves its tail unreachable"* pins the behaviour.

### Using media you have locally

Media is referenced by URL; the database stores only metadata (id, name, type,
url, duration) and the browser fetches the bytes itself. The backend never sees
a pixel.

For files with no public URL, drop them in `frontend/public/media/`. Vite (dev)
and Vercel/Netlify (prod) both serve that folder at `/media/`, same origin as
the app and with HTTP range support -- which matters, because seeking to a
mid-clip offset is exactly what a late joiner or a mid-sync reload does.

Then create the media with a site-root path rather than a full URL:

```
/media/clip.mp4
```

Stored that way, one row works in every environment: the browser resolves the
path against whatever origin the page is on, so the same value means
`http://localhost:5173/media/clip.mp4` locally and
`https://<your-app>.vercel.app/media/clip.mp4` once deployed.

Keep the files small -- they are committed to git and shipped in the frontend
deploy. There is deliberately **no upload endpoint**: uploading from a browser
would need somewhere durable to put the bytes, and the free hosting this targets
has an ephemeral filesystem, so it would mean adding object storage (S3/R2) for
little gain in a scheduling demo.

**Video.** Videos are `muted` (browsers block autoplay with sound), never use
`loop`, and are joined at `currentTime = (serverNow - itemStartedAt) / 1000`
rather than at 0 — which is what makes a reload or a late joiner land in the
same frame as everyone else. A check every 2 s re-seeks if the element has
drifted more than 0.5 s from the schedule. If a clip is shorter than its
configured duration, the last frame is held until the schedule advances.

The `<img>`/`<video>` element is keyed on *segment identity*
(`windowId + itemId + startedAt`), so the 250 ms render tick never recreates it
— without that, every video would restart four times a second. The next item is
preloaded (`new Image()` / a hidden `<video preload="auto">`) so switches have no gap.

---

## 5. How sync works

`POST /api/sync { mediaId, durationMs? }` writes a row:
`{ id, media_id, start_at, end_at, cancelled_at }`.

**Lead time.** `start_at = serverNow + SYNC_LEAD_MS` (default 1500 ms). The sync
does not begin "now", it begins a moment in the future. That is the whole trick:
the SSE event reaches every client *before* the switch is due, so all of them
flip at the same wall-clock instant instead of "whenever my message arrived".

**Server clock offset.** Client clocks cannot be trusted, so
[`frontend/src/lib/clock.ts`](frontend/src/lib/clock.ts) runs a small NTP-style
handshake against `GET /api/time`: five samples, `offset = serverTime - (t0+t1)/2`,
keeping the sample with the smallest round trip (the fastest exchange has the
least room for error). It re-syncs every 60 s. All scheduling uses
`serverNow() = Date.now() + offset`; `?debug=1` shows the current offset and RTT.

**Client rule.** If `start_at <= serverNow < end_at` and the sync is not
cancelled, show the sync media (for video, at offset `(serverNow - start_at)/1000`);
otherwise fall back to the window's normal `resolve`.

**Replacement.** A new sync cancels any active or pending one (`cancelled_at` is
set on the old row) — there is only ever one sync in flight.
`DELETE /api/sync/active` ends it early.

**After a sync ends**, the underlying timeline has kept running the whole time
(wall-clock semantics), so each window simply shows whatever its own schedule
says for that moment. Playlists are untouched — a sync never mutates them — so
this needs no extra state and is fully deterministic.

**Late joiners and reloads.** The active sync is part of `GET /api/state`, so a
client that arrives mid-sync joins at the correct offset rather than starting the
clip over.

---

## 6. Dynamic updates (and why nothing jumps)

Media can be added to any window's playlist at runtime, and every open client
reflects it within about a second.

The subtle part is not the update, it is *not disturbing what is on screen*. If
the server simply recomputed from `cycleStart` after an edit, inserting an item
would shift every index and each screen would jump to different media mid-item.

So every playlist mutation goes through `store.mutatePlaylist`, which does all of
this in **one transaction**, with the window row locked:

1. Read the current list, and `resolve()` it — this gives the item on screen and
   its start time `s = now - elapsedInItem`.
2. Apply the mutation.
3. Re-read the list. If the item that was playing still exists (matched on
   `playlist_items.id`, **not** `media_id` — the same media may appear several
   times in one list), set `anchor_at = s`, `anchor_index = j`. It keeps playing
   uninterrupted, and the *new* list continues from `j+1`.
4. If it was removed, set `anchor_at = now`, `anchor_index = min(oldIndex, len-1)`
   so the replacement starts immediately.
5. Bump `version` and broadcast `window.updated`.

Anchors are scoped to the cycle they were set in — `resolve` ignores an anchor
whose `anchor_at` is before the current `cycleStart` (or in the future), so the
next 5-hour boundary still restarts cleanly at item 0.

This is covered by tests for append, insert-before-current, delete-another and
delete-current, each asserting that the current item's id and start time are
unchanged.

**Transport.** `GET /api/events` is a Server-Sent Events stream emitting
`window.updated`, `media.created`, `sync.started` and `sync.cancelled`. Clients
treat every event the same way: re-fetch `GET /api/state`. The payload is small,
and "always re-read the truth" is far easier to get right than applying
incremental patches. A `: ping` comment every 20 s stops hosting proxies closing
the idle stream, and after 3 consecutive failures the client gives up on SSE and
polls `/api/state` every 5 s instead (the status dot in the top bar shows which
mode it is in).

---

## 7. API documentation

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

### Mutating endpoints

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

**`POST /api/windows/{id}/items`** — `position` omitted appends;
`durationMs` omitted uses the media's own length. Returns the whole window,
re-anchored.

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

Note `anchorAt`/`anchorIndex` in that response: `M2` was playing at index 1
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

### Validation

- Unknown `mediaId` or window → `404`.
- Bad type or non-positive duration → `400`.
- A media URL must be either an absolute `http(s)://` address **or** a site-root
  path such as `/media/clip.mp4` (see [local media](#using-media-you-have-locally)).
  Bare relative paths (`media/clip.mp4`) and protocol-relative ones
  (`//host/clip.mp4`, which silently points at another origin) are rejected.
- `blank` media must have no URL; `image`/`video` must have one.
- Sync `durationMs` outside 1 s–1 h → `400`.
- Unknown JSON fields are rejected, so a typo in a payload is a `400` rather than
  being silently ignored.

---

## 8. Data model

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

Notes:

- The same media may appear several times in one playlist, so item identity is
  `playlist_items.id`, never `media_id`. Re-anchoring matches on that id.
- Effective duration is `COALESCE(playlist_items.duration_ms, media.duration_ms)`.
- Every timestamp the scheduler touches is an **int64 of unix milliseconds** in
  both Go and TypeScript — no `time.Time` on the wire, so there is no timezone or
  serialisation ambiguity to get wrong.
- Position rewrites happen inside the mutation transaction, in two steps (shift
  everything by `+10000`, then write final positions) so `UNIQUE (window_id, position)`
  is never transiently violated.
- Migrations are plain `.sql` files embedded with `go:embed`, applied in filename
  order on startup and recorded in `schema_migrations`. No migration tool needed.

---

## 9. Deployment

Config files are committed: [`backend/Dockerfile`](backend/Dockerfile),
[`render.yaml`](render.yaml), [`frontend/vercel.json`](frontend/vercel.json),
[`frontend/public/_redirects`](frontend/public/_redirects) (Netlify).

The Docker image is multi-stage (`golang:1.25-alpine` → `distroless/static`,
`CGO_ENABLED=0`) and has been built and run locally against the compose
Postgres. Migrations are embedded, so the image needs no extra files.

**1. Database — Neon (or Supabase).** Create a free Postgres project and copy the
connection string. It must end with `?sslmode=require`.

**2. Backend — Render (Docker web service).** Root directory `backend`, health
check path `/api/health`. Environment variables:

| Variable | Value |
| --- | --- |
| `DATABASE_URL` | the Neon string, with `sslmode=require` |
| `ALLOWED_ORIGINS` | the exact frontend origin, e.g. `https://media-sequencer.vercel.app` |
| `CYCLE_MS` | `18000000` |
| `SYNC_LEAD_MS` | `1500` |
| `SEED_ON_START` | `true` |
| `PORT` | set by Render automatically |

**3. Frontend — Vercel (or Netlify).** Root directory `frontend`, build
`npm run build`, output `dist`. Set `VITE_API_BASE_URL` to the backend URL. The
committed `vercel.json` / `_redirects` provide the SPA fallback so `/window/W1`
survives a hard refresh.

**After deploying**, check:

- `ALLOWED_ORIGINS` is the exact frontend origin (scheme and host, no trailing slash).
- SSE works through the host — open the dashboard and confirm the status dot says
  "live (SSE)" and stays there for more than a minute (that proves the 20 s
  heartbeat is getting through).
- The seed ran exactly once: the backend log says `seed applied` on the first
  boot and `seed skipped, database already has data` afterwards.

> Render's free tier sleeps after inactivity, so the first request after a quiet
> period takes several seconds to wake the service.

### Environment variables

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
environment variables always win, which is what hosting platforms expect.

---

## 10. Assumptions

- **The 5-hour cycle is per window**, aligned to a stored `cycle_epoch`. The
  seeded windows all share one epoch (the start of the current UTC day), so they
  flip together; nothing requires that.
- **The list restarts at item 0 at every cycle boundary**, truncating any partial
  loop. An item straddling the boundary is cut off, not allowed to finish.
- **A playlist longer than the cycle** has a tail that never plays. Documented
  above and pinned by a test vector.
- **Images and blanks have configured durations**; videos use their real length
  unless a per-item `durationMs` override is given. The "create media" form can
  read a video's real duration from its metadata.
- **Sync replaces any existing sync**, and the timeline keeps running underneath
  it (wall clock) — there is no pause-and-resume.
- **Seed data is a placeholder.** The brief refers to "example windows and media
  lists" but does not include them. The placeholder lives entirely in
  [`backend/internal/store/seed.go`](backend/internal/store/seed.go) in two
  tables (`seedMedia`, `seedWindows`); swapping in the real lists is a one-file
  edit and nothing else depends on those values. Seeding is idempotent — it only
  runs when both tables are empty — so restarts never duplicate or overwrite data.
- **Single backend instance.** The SSE hub is in-memory.
- **Videos are muted**, because browsers will not autoplay audio.
- The scaffold produced **React 19** (the current Vite default) rather than the
  React 18 named in the brief. Nothing in the app depends on the difference.

---

## 11. Tradeoffs

**Wall-clock sync vs pause-and-resume.** When a sync ends, each window shows
whatever its schedule says for that moment — it does *not* resume where it was
interrupted. Pause-and-resume would need per-window resume state, and, worse,
every window would then be permanently offset from its 5-hour cycle by the
accumulated sync time, so windows would drift out of alignment with each other
and with the cycle boundary. The wall-clock rule needs no extra state, is
deterministic, and keeps all the cycle guarantees intact.

**SSE vs WebSockets.** Every realtime message here goes server→client, which is
exactly what SSE is for. `EventSource` reconnects on its own, it is plain HTTP so
it survives proxies that dislike upgrades, and the server side is a loop writing
to an `http.ResponseWriter`. WebSockets would add a dependency and a handshake to
buy bidirectionality we never use. The cost is the 6-connections-per-origin limit
on HTTP/1.1 and the need for an explicit heartbeat — both handled.

**Client-side computation vs server-pushed items.** Pushing "show item 3 now"
from a timer on the server is the obvious design and it is the wrong one: it makes
correctness depend on message delivery and latency, drifts over hours, and every
reconnect or late joiner needs special handling. Making the current item a pure
function of `(state, time)` moves the problem to clock agreement, which is a
smaller and much better-understood problem — solved here with a ~40-line NTP-style
offset estimate. The cost is that clients must be trusted to compute correctly,
which is why the same function is tested on both sides against one shared file,
and why `GET /api/windows/{id}/now` exists to compare them.

**Postgres vs SQLite.** SQLite would remove a moving part, but the free hosting
tiers this targets have ephemeral filesystems, so a SQLite file would vanish on
every redeploy — and "playlists are stored in persistent storage" is an explicit
requirement. Managed Postgres is free at this size and survives restarts.

**Re-fetching all state on every event vs incremental patches.** Events carry
only a nudge; clients re-read `GET /api/state`. Patches would be smaller, but
every patch is a chance to apply an update out of order and leave a client
silently wrong. The whole payload is a few kilobytes, so the simple option wins
until it measurably does not.

**Verification.** One thing not verified here: the app was never opened in a real
browser during development (no browser automation was available in the
environment). Everything is verified through the Go and TypeScript test suites,
direct API calls against the running server, an SSE stream capture, a live
cycle-boundary probe, and the cross-implementation integration test — but the
rendering itself, and the video-drift behaviour in particular, still needs a
human to look at it.
