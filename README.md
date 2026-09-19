<h1 align="center">Media Sequencer</h1>

<p align="center">
  <em>Real-time, multi-screen media scheduling with live playlist edits and clock-aligned sync across every display.</em>
</p>

<p align="center">
  <a href="https://media-sequencer-dun.vercel.app/"><strong>Live Demo →</strong></a>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white">
  <img alt="React" src="https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black">
  <img alt="TypeScript" src="https://img.shields.io/badge/TypeScript-6-3178C6?logo=typescript&logoColor=white">
  <img alt="Postgres" src="https://img.shields.io/badge/Postgres-16-4169E1?logo=postgresql&logoColor=white">
  <img alt="Realtime" src="https://img.shields.io/badge/realtime-SSE-FF6F00">
</p>

---

## Overview

Media Sequencer is a scheduling system for multiple display screens — the kind of setup used for digital signage in lobbies, stores, or event venues. Each screen plays its own playlist of images and videos on a continuous loop, and an operator can:

- **Edit any playlist while it is playing** — without interrupting what is currently on screen.
- **Push a single piece of media to every screen at once** — all screens switch at exactly the same moment, then return to their own schedules.

Every screen stays perfectly in step, whether it is a second browser tab, a different device, or a screen that was just refreshed or opened late.

**Live:** [media-sequencer-dun.vercel.app](https://media-sequencer-dun.vercel.app/) · Open a single screen at [`/window/W1`](https://media-sequencer-dun.vercel.app/window/W1)

---

## Features

- **Independent screens** — each window plays its own playlist back-to-back, looping continuously.
- **5-hour playback cycle** — every window restarts from the first item at each cycle boundary (configurable).
- **Live playlist editing** — add, remove, or reorder items at runtime; changes appear on every open client within about a second, and the item currently playing continues uninterrupted.
- **Global sync** — broadcast one media item to all windows at a scheduled instant; windows switch together and resume their own schedules afterwards.
- **Accurate late joins** — a screen opened mid-item or mid-sync starts at the correct position, including seeking videos to the right frame.
- **Persistent state** — playlists, edits, and sync history are stored in PostgreSQL and survive restarts and redeploys.
- **Built-in verification** — a debug overlay (`?debug=1`) shows what each client computed, and a server endpoint returns the backend's own calculation for comparison.

---

## Architecture

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

| Layer | Technology | Hosting |
| --- | --- | --- |
| Frontend | React 19, TypeScript, Vite | Vercel |
| Backend | Go 1.25 (`net/http`, `pgx/v5`) | Render (Docker) |
| Database | PostgreSQL 16 | Neon |
| Realtime | Server-Sent Events | — |

No ORM, router library, or state-management library. The only runtime dependencies are `pgx/v5` and `react-router-dom`.

---

## How It Works

### Deterministic playback

The server never tells a screen what to play. It stores playlists and a small set of timeline anchors; each client computes the current item itself from a pure function:

```
resolve(window, items, cycleMs, serverNow) -> { index, elapsedInItemMs, remainingMs, ... }
```

Because every client runs the same function against the same server time, all screens agree without exchanging messages, and a dropped network event can never desynchronise playback. The function is implemented in both Go ([`scheduler.go`](backend/internal/scheduler/scheduler.go)) and TypeScript ([`scheduler.ts`](frontend/src/lib/scheduler.ts)), and runs in O(playlist length) regardless of how long a window has been running.

### Clock synchronisation

Client clocks are not trusted. [`clock.ts`](frontend/src/lib/clock.ts) performs an NTP-style handshake against `GET /api/time` — five samples, keeping the one with the lowest round-trip time — and re-syncs every 60 seconds. All scheduling uses the estimated server time.

### Global sync

A sync is scheduled **1.5 seconds in the future** (`SYNC_LEAD_MS`). The realtime event reaches every client before the switch is due, so all screens change at the same wall-clock instant rather than whenever each message arrives. The underlying schedules keep running during a sync, so each window resumes exactly where its own timeline has reached. A new sync replaces any active one.

### Edits without interruption

Every playlist mutation runs in a single transaction with the window row locked:

1. Resolve the item currently on screen and its start time.
2. Apply the change.
3. If the playing item still exists (matched by `playlist_items.id`, since the same media can appear more than once), re-anchor the timeline so it keeps playing and the new list continues after it.
4. If it was removed, start the replacement immediately.
5. Bump the window's `version` and broadcast `window.updated`.

Anchors apply only within the cycle they were set in, so the next cycle boundary still restarts cleanly at the first item.

### Realtime updates

`GET /api/events` streams `window.updated`, `media.created`, `sync.started`, and `sync.cancelled`. On any event, clients re-fetch `GET /api/state`. A heartbeat every 20 seconds keeps the connection alive through hosting proxies, and clients fall back to polling every 5 seconds if the stream fails repeatedly.

---

## API Reference

Base path: `/api`. JSON request and response bodies, camelCase fields.
Errors follow the shape `{ "error": { "code": "...", "message": "..." } }`.

### System

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/api/health` | Health check — returns `{ "status": "ok" }` |
| `GET` | `/api/time` | Server reference clock — returns `{ "serverTimeMs" }` |
| `GET` | `/api/state` | Full application state (media, windows, playlists, active sync) in one payload |
| `GET` | `/api/events` | Server-Sent Events stream for realtime updates |

### Media

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/api/media` | List the media library |
| `POST` | `/api/media` | Create a media item (image, video, or blank) |

### Windows & Playlists

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/api/windows` | List all windows with their playlists |
| `POST` | `/api/windows` | Create a window |
| `POST` | `/api/windows/{id}/items` | Add media to a window's playlist (optional `position`) |
| `DELETE` | `/api/windows/{id}/items/{itemId}` | Remove an item from a playlist |
| `PUT` | `/api/windows/{id}/items/order` | Reorder a playlist |
| `GET` | `/api/windows/{id}/now` | Server-computed playback position for a window |

### Sync

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/api/sync` | Start a global sync across all windows |
| `GET` | `/api/sync/active` | Get the active or pending sync, or `null` |
| `DELETE` | `/api/sync/active` | Cancel the current sync |

<details>
<summary><b>Request and response examples</b></summary>

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

**`POST /api/windows/{id}/items`** — omitting `position` appends; omitting `durationMs` uses the media's own length. Returns the updated window.

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

**`PUT /api/windows/{id}/items/order`** — `itemIds` must contain exactly the window's current item ids.

```jsonc
{ "itemIds": [13, 12, 31] }
```

**`POST /api/sync`** — `durationMs` defaults to the media's length; must be between 1 s and 1 h.

```jsonc
// request
{ "mediaId": "M2", "durationMs": 30000 }
// 201
{ "id": 4, "mediaId": "M2", "startAt": 1758260001500, "endAt": 1758260031500 }
```

**`GET /api/windows/{id}/now`**

```jsonc
{
  "windowId": "W1",
  "serverTimeMs": 1789798624041,
  "cycleMs": 18000000,
  "resolved": {
    "blank": false, "index": 1, "itemId": 21, "mediaId": "M2",
    "elapsedInItemMs": 9726, "remainingMs": 274,
    "startedAtMs": 1789798614315,
    "cycleStartMs": 1789794000000, "cycleEndMs": 1789812000000
  },
  "activeSync": null
}
```

</details>

<details>
<summary><b>Validation rules</b></summary>

- Unknown `mediaId` or window → `404`.
- Invalid media type or non-positive duration → `400`.
- Media URLs must be absolute `http(s)://` addresses or site-root paths (e.g. `/media/clip.mp4`).
- `blank` media must have no URL; `image` and `video` must have one.
- Sync duration outside 1 s – 1 h → `400`.
- Unknown JSON fields are rejected with `400`.

</details>

---

## Database Schema

Four tables, managed through plain SQL migrations embedded in the Go binary and applied automatically on startup.

### `media`

The media library. The database stores metadata only; browsers fetch media directly from its URL.

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `TEXT` | Primary key, e.g. `M1` |
| `name` | `TEXT` | Display name |
| `type` | `TEXT` | `image`, `video`, or `blank` |
| `url` | `TEXT` | `NULL` for blank media |
| `duration_ms` | `BIGINT` | Default duration, must be > 0 |
| `created_at` | `TIMESTAMPTZ` | Creation time |

### `windows`

Each display screen and its timeline state.

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `TEXT` | Primary key, e.g. `W1` |
| `name` | `TEXT` | Display name |
| `cycle_epoch` | `BIGINT` | Cycle reference point (unix ms) |
| `anchor_at` | `BIGINT` | Timeline anchor set by a live edit (unix ms), nullable |
| `anchor_index` | `INT` | Playlist index the anchor refers to, nullable |
| `version` | `BIGINT` | Incremented on every change |
| `position` | `INT` | Display order on the dashboard |

### `playlist_items`

Ordered entries in each window's playlist. The same media may appear multiple times.

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `BIGSERIAL` | Primary key — the item's identity |
| `window_id` | `TEXT` | → `windows.id`, cascades on delete |
| `media_id` | `TEXT` | → `media.id` |
| `position` | `INT` | 0-based, unique per window |
| `duration_ms` | `BIGINT` | Optional per-item duration override |

### `syncs`

History of global sync events.

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `BIGSERIAL` | Primary key |
| `media_id` | `TEXT` | → `media.id` |
| `start_at` | `BIGINT` | Scheduled start (unix ms) |
| `end_at` | `BIGINT` | Scheduled end (unix ms) |
| `cancelled_at` | `BIGINT` | Set when cancelled or replaced, nullable |
| `created_at` | `TIMESTAMPTZ` | Creation time |

All scheduling timestamps are stored and transmitted as 64-bit unix milliseconds, avoiding any timezone or serialisation ambiguity between Go and TypeScript.

---

## Getting Started

**Prerequisites:** Go 1.25+, Node 20+, Docker.

```bash
# 1. Start Postgres
docker compose up -d

# 2. Start the backend
cd backend
cp .env.example .env
go run ./cmd/server          # http://localhost:8080

# 3. Start the frontend (new terminal)
cd frontend
cp .env.example .env.local
npm install
npm run dev                  # http://localhost:5173
```

Migrations and seed data are applied automatically on first run. To observe a cycle restart without waiting five hours, run the backend with a shortened cycle:

```bash
CYCLE_MS=60000 go run ./cmd/server
```

### Configuration

**Backend** (`backend/.env`)

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8080` | HTTP port |
| `DATABASE_URL` | local Postgres | Postgres connection string |
| `ALLOWED_ORIGINS` | `http://localhost:5173` | CORS origin(s) for the frontend |
| `CYCLE_MS` | `18000000` | Cycle length (5 hours) |
| `SYNC_LEAD_MS` | `1500` | Delay before a sync takes effect |
| `SEED_ON_START` | `true` | Seed sample data into an empty database |

**Frontend** (`frontend/.env.local`)

| Variable | Description |
| --- | --- |
| `VITE_API_BASE_URL` | Backend URL, e.g. `http://localhost:8080` |

---

## Deployment

| Component | Platform | Config |
| --- | --- | --- |
| Frontend | Vercel (static Vite build) | [`frontend/vercel.json`](frontend/vercel.json) |
| Backend | Render (Docker) | [`render.yaml`](render.yaml), [`backend/Dockerfile`](backend/Dockerfile) |
| Database | Neon (Postgres) | Direct connection string with `sslmode=require` |

The backend runs as a single long-lived process because the SSE hub holds client connections in memory — a serverless platform would route events and streams to separate instances. The Docker image is a multi-stage build (`golang:1.25-alpine` → `distroless/static`) running as a non-root user.

---

## Project Structure

```
backend/
  cmd/server/            Entrypoint
  internal/config/       Environment configuration
  internal/model/        Shared types
  internal/scheduler/    Pure scheduling logic (resolve, cycle, re-anchor)
  internal/store/        Postgres access, embedded migrations, seed data
  internal/events/       In-memory SSE hub
  internal/api/          HTTP handlers, routing, middleware
frontend/
  src/lib/clock.ts       Server clock offset estimation
  src/lib/scheduler.ts   TypeScript port of the Go scheduler
  src/lib/playback.ts    Combines schedule and sync into what is on screen
  src/hooks/             Render tick and realtime state
  src/components/        WindowGrid, WindowPlayer, MediaView, controls
  public/media/          Locally hosted media files
docker-compose.yml       Local Postgres
render.yaml              Render blueprint
```
