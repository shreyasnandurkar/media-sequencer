CREATE TABLE IF NOT EXISTS media (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  type         TEXT NOT NULL CHECK (type IN ('image','video','blank')),
  url          TEXT,
  duration_ms  BIGINT NOT NULL CHECK (duration_ms > 0),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK ((type = 'blank') = (url IS NULL))
);

CREATE TABLE IF NOT EXISTS windows (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  cycle_epoch   BIGINT NOT NULL,
  anchor_at     BIGINT,
  anchor_index  INT,
  version       BIGINT NOT NULL DEFAULT 1,
  position      INT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS playlist_items (
  id          BIGSERIAL PRIMARY KEY,
  window_id   TEXT NOT NULL REFERENCES windows(id) ON DELETE CASCADE,
  media_id    TEXT NOT NULL REFERENCES media(id),
  position    INT  NOT NULL,
  duration_ms BIGINT CHECK (duration_ms > 0),
  UNIQUE (window_id, position)
);

CREATE INDEX IF NOT EXISTS playlist_items_window_idx ON playlist_items (window_id, position);

CREATE TABLE IF NOT EXISTS syncs (
  id            BIGSERIAL PRIMARY KEY,
  media_id      TEXT NOT NULL REFERENCES media(id),
  start_at      BIGINT NOT NULL,
  end_at        BIGINT NOT NULL,
  cancelled_at  BIGINT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS syncs_end_at_idx ON syncs (end_at DESC);
