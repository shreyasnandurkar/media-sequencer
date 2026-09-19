// Package store is the only place that talks to PostgreSQL. Handlers call it;
// it calls the (pure) scheduler when a playlist edit needs re-anchoring.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shreyasnandurkar/media-sequencer/backend/internal/model"
	"github.com/shreyasnandurkar/media-sequencer/backend/internal/scheduler"
)

// Sentinel errors the API layer maps onto HTTP status codes.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func New(ctx context.Context, databaseURL string, log *slog.Logger) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool, log: log}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Migrate applies every embedded .sql file, in filename order, exactly once.
// Applied names are recorded in schema_migrations, so restarts are cheap and
// a fresh database and an existing one both end up in the same shape.
func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		// Each migration runs in its own transaction: either the whole file
		// applies and is recorded, or nothing changes.
		err = s.withTx(ctx, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		s.log.Info("migration applied", "name", name)
	}
	return nil
}

// withTx runs fn inside a transaction, rolling back on error or panic.
// (defer + named error is the standard Go idiom for this.)
func (s *Store) withTx(ctx context.Context, fn func(tx pgx.Tx) error) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// --- Reads ---------------------------------------------------------------

func (s *Store) ListMedia(ctx context.Context) ([]model.Media, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, type, url, duration_ms FROM media ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Media{}
	for rows.Next() {
		var m model.Media
		if err := rows.Scan(&m.ID, &m.Name, &m.Type, &m.URL, &m.DurationMs); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetMedia(ctx context.Context, id string) (model.Media, error) {
	var m model.Media
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, type, url, duration_ms FROM media WHERE id = $1`, id,
	).Scan(&m.ID, &m.Name, &m.Type, &m.URL, &m.DurationMs)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// ListWindows returns every window with its playlist already attached.
// One query per table (not one per window) keeps this O(1) round trips.
func (s *Store) ListWindows(ctx context.Context) ([]model.Window, error) {
	return listWindows(ctx, s.pool, "")
}

func (s *Store) GetWindow(ctx context.Context, id string) (model.Window, error) {
	ws, err := listWindows(ctx, s.pool, id)
	if err != nil {
		return model.Window{}, err
	}
	if len(ws) == 0 {
		return model.Window{}, ErrNotFound
	}
	return ws[0], nil
}

// listWindows reads windows plus their items. The inline interface means it
// accepts both *pgxpool.Pool and pgx.Tx, so it works in or out of a transaction.
func listWindows(ctx context.Context, q interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}, onlyID string) ([]model.Window, error) {
	rows, err := q.Query(ctx,
		`SELECT id, name, cycle_epoch, anchor_at, anchor_index, version, position
		 FROM windows
		 WHERE ($1 = '' OR id = $1)
		 ORDER BY position, id`, onlyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	windows := []model.Window{}
	index := map[string]int{}
	for rows.Next() {
		var w model.Window
		if err := rows.Scan(&w.ID, &w.Name, &w.CycleEpoch, &w.AnchorAt, &w.AnchorIndex, &w.Version, &w.Position); err != nil {
			return nil, err
		}
		w.Items = []model.PlaylistItem{}
		index[w.ID] = len(windows)
		windows = append(windows, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(windows) == 0 {
		return windows, nil
	}

	itemRows, err := q.Query(ctx,
		`SELECT pi.id, pi.window_id, pi.media_id, pi.position,
		        COALESCE(pi.duration_ms, m.duration_ms) AS duration_ms
		 FROM playlist_items pi
		 JOIN media m ON m.id = pi.media_id
		 WHERE ($1 = '' OR pi.window_id = $1)
		 ORDER BY pi.window_id, pi.position`, onlyID)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()

	for itemRows.Next() {
		var windowID string
		var it model.PlaylistItem
		if err := itemRows.Scan(&it.ID, &windowID, &it.MediaID, &it.Position, &it.DurationMs); err != nil {
			return nil, err
		}
		if i, ok := index[windowID]; ok {
			windows[i].Items = append(windows[i].Items, it)
		}
	}
	return windows, itemRows.Err()
}

// State assembles the single payload the frontend polls/refreshes.
func (s *Store) State(ctx context.Context, cycleMs, now int64) (model.State, error) {
	media, err := s.ListMedia(ctx)
	if err != nil {
		return model.State{}, err
	}
	windows, err := s.ListWindows(ctx)
	if err != nil {
		return model.State{}, err
	}
	sync, err := s.ActiveSync(ctx, now)
	if err != nil {
		return model.State{}, err
	}
	return model.State{
		ServerTimeMs: now,
		CycleMs:      cycleMs,
		Media:        media,
		Windows:      windows,
		ActiveSync:   sync,
	}, nil
}

// --- Media writes --------------------------------------------------------

func (s *Store) CreateMedia(ctx context.Context, m model.Media) (model.Media, error) {
	if m.ID == "" {
		id, err := s.nextMediaID(ctx)
		if err != nil {
			return m, err
		}
		m.ID = id
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO media (id, name, type, url, duration_ms) VALUES ($1,$2,$3,$4,$5)`,
		m.ID, m.Name, string(m.Type), m.URL, m.DurationMs)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return m, fmt.Errorf("%w: media id %q already exists", ErrConflict, m.ID)
		}
		return m, err
	}
	return m, nil
}

// nextMediaID picks the next free M<n>. Good enough for a single-instance
// service; a collision just surfaces as a 409 and the caller retries.
func (s *Store) nextMediaID(ctx context.Context) (string, error) {
	var maxN int
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(CAST(SUBSTRING(id FROM 2) AS INT)), 0)
		 FROM media WHERE id ~ '^M[0-9]+$'`).Scan(&maxN)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("M%d", maxN+1), nil
}

// --- Window writes -------------------------------------------------------

func (s *Store) CreateWindow(ctx context.Context, name string, cycleEpoch int64) (model.Window, error) {
	var w model.Window
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var maxN, maxPos int
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(MAX(CAST(SUBSTRING(id FROM 2) AS INT)), 0), COALESCE(MAX(position), -1)
			 FROM windows WHERE id ~ '^W[0-9]+$'`).Scan(&maxN, &maxPos); err != nil {
			return err
		}
		w = model.Window{
			ID:         fmt.Sprintf("W%d", maxN+1),
			Name:       name,
			CycleEpoch: cycleEpoch,
			Version:    1,
			Position:   maxPos + 1,
			Items:      []model.PlaylistItem{},
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO windows (id, name, cycle_epoch, version, position) VALUES ($1,$2,$3,1,$4)`,
			w.ID, w.Name, w.CycleEpoch, w.Position)
		return err
	})
	return w, err
}

// mutatePlaylist is the heart of TASK.md 2.3. Every playlist edit goes through
// it so the re-anchor logic can never be forgotten:
//
//  1. lock the window row and read the CURRENT list
//  2. Resolve() with the old list -> what is on screen right now
//  3. run the caller's mutation
//  4. re-read the list, recompute the anchor, bump version
//
// All of it in one transaction, so a concurrent edit cannot interleave.
func (s *Store) mutatePlaylist(
	ctx context.Context,
	windowID string,
	cycleMs, now int64,
	mutate func(ctx context.Context, tx pgx.Tx, w model.Window, items []model.PlaylistItem) error,
) (model.Window, error) {
	var out model.Window

	err := s.withTx(ctx, func(tx pgx.Tx) error {
		// SELECT ... FOR UPDATE takes a row lock for the rest of the transaction.
		var w model.Window
		err := tx.QueryRow(ctx,
			`SELECT id, name, cycle_epoch, anchor_at, anchor_index, version, position
			 FROM windows WHERE id = $1 FOR UPDATE`, windowID,
		).Scan(&w.ID, &w.Name, &w.CycleEpoch, &w.AnchorAt, &w.AnchorIndex, &w.Version, &w.Position)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		oldItems, err := itemsOf(ctx, tx, windowID)
		if err != nil {
			return err
		}
		before := scheduler.Resolve(w, oldItems, cycleMs, now)

		if err := mutate(ctx, tx, w, oldItems); err != nil {
			return err
		}

		newItems, err := itemsOf(ctx, tx, windowID)
		if err != nil {
			return err
		}
		anchorAt, anchorIndex := scheduler.Reanchor(before, newItems, now)

		if err := tx.QueryRow(ctx,
			`UPDATE windows SET anchor_at = $2, anchor_index = $3, version = version + 1
			 WHERE id = $1 RETURNING version`,
			windowID, anchorAt, anchorIndex).Scan(&w.Version); err != nil {
			return err
		}

		w.AnchorAt, w.AnchorIndex, w.Items = anchorAt, anchorIndex, newItems
		out = w
		return nil
	})

	return out, err
}

func itemsOf(ctx context.Context, tx pgx.Tx, windowID string) ([]model.PlaylistItem, error) {
	rows, err := tx.Query(ctx,
		`SELECT pi.id, pi.media_id, pi.position, COALESCE(pi.duration_ms, m.duration_ms)
		 FROM playlist_items pi
		 JOIN media m ON m.id = pi.media_id
		 WHERE pi.window_id = $1
		 ORDER BY pi.position`, windowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.PlaylistItem{}
	for rows.Next() {
		var it model.PlaylistItem
		if err := rows.Scan(&it.ID, &it.MediaID, &it.Position, &it.DurationMs); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// AddItem inserts media into a window's playlist. position==nil appends.
func (s *Store) AddItem(ctx context.Context, windowID, mediaID string, position *int, durationMs *int64, cycleMs, now int64) (model.Window, error) {
	return s.mutatePlaylist(ctx, windowID, cycleMs, now,
		func(ctx context.Context, tx pgx.Tx, w model.Window, items []model.PlaylistItem) error {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media WHERE id = $1)`, mediaID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: media %q", ErrNotFound, mediaID)
			}

			at := len(items)
			if position != nil {
				at = *position
				if at < 0 {
					at = 0
				}
				if at > len(items) {
					at = len(items)
				}
			}

			// Rebuild the ordering with the new item spliced in. The two-step
			// write (park everything above 10000, then write final positions)
			// avoids transiently violating UNIQUE (window_id, position).
			ids := make([]int64, 0, len(items)+1)
			for i, it := range items {
				if i == at {
					ids = append(ids, 0) // placeholder for the new row
				}
				ids = append(ids, it.ID)
			}
			if at == len(items) {
				ids = append(ids, 0)
			}

			if _, err := tx.Exec(ctx,
				`UPDATE playlist_items SET position = position + 10000 WHERE window_id = $1`, windowID); err != nil {
				return err
			}
			for pos, id := range ids {
				if id == 0 {
					if _, err := tx.Exec(ctx,
						`INSERT INTO playlist_items (window_id, media_id, position, duration_ms)
						 VALUES ($1,$2,$3,$4)`, windowID, mediaID, pos, durationMs); err != nil {
						return err
					}
					continue
				}
				if _, err := tx.Exec(ctx,
					`UPDATE playlist_items SET position = $2 WHERE id = $1`, id, pos); err != nil {
					return err
				}
			}
			return nil
		})
}

func (s *Store) DeleteItem(ctx context.Context, windowID string, itemID int64, cycleMs, now int64) (model.Window, error) {
	return s.mutatePlaylist(ctx, windowID, cycleMs, now,
		func(ctx context.Context, tx pgx.Tx, w model.Window, items []model.PlaylistItem) error {
			tag, err := tx.Exec(ctx,
				`DELETE FROM playlist_items WHERE id = $1 AND window_id = $2`, itemID, windowID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return fmt.Errorf("%w: item %d", ErrNotFound, itemID)
			}
			return normalizePositions(ctx, tx, windowID)
		})
}

// SetOrder rewrites a window's playlist order. itemIDs must be exactly the
// window's current item ids, in the desired order.
func (s *Store) SetOrder(ctx context.Context, windowID string, itemIDs []int64, cycleMs, now int64) (model.Window, error) {
	return s.mutatePlaylist(ctx, windowID, cycleMs, now,
		func(ctx context.Context, tx pgx.Tx, w model.Window, items []model.PlaylistItem) error {
			if len(itemIDs) != len(items) {
				return fmt.Errorf("%w: expected %d item ids, got %d", ErrConflict, len(items), len(itemIDs))
			}
			known := map[int64]bool{}
			for _, it := range items {
				known[it.ID] = true
			}
			seen := map[int64]bool{}
			for _, id := range itemIDs {
				if !known[id] || seen[id] {
					return fmt.Errorf("%w: item %d is not in this window (or is repeated)", ErrConflict, id)
				}
				seen[id] = true
			}

			if _, err := tx.Exec(ctx,
				`UPDATE playlist_items SET position = position + 10000 WHERE window_id = $1`, windowID); err != nil {
				return err
			}
			for pos, id := range itemIDs {
				if _, err := tx.Exec(ctx,
					`UPDATE playlist_items SET position = $2 WHERE id = $1`, id, pos); err != nil {
					return err
				}
			}
			return nil
		})
}

// normalizePositions rewrites positions to 0..n-1 keeping the current order.
func normalizePositions(ctx context.Context, tx pgx.Tx, windowID string) error {
	rows, err := tx.Query(ctx,
		`SELECT id FROM playlist_items WHERE window_id = $1 ORDER BY position`, windowID)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE playlist_items SET position = position + 10000 WHERE window_id = $1`, windowID); err != nil {
		return err
	}
	for pos, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE playlist_items SET position = $2 WHERE id = $1`, id, pos); err != nil {
			return err
		}
	}
	return nil
}

// --- Sync ----------------------------------------------------------------

// ActiveSync returns the sync that is running or about to run, or nil.
// "About to run" matters: a sync starts SYNC_LEAD_MS in the future so every
// client can be told before it begins.
func (s *Store) ActiveSync(ctx context.Context, now int64) (*model.Sync, error) {
	var sy model.Sync
	err := s.pool.QueryRow(ctx,
		`SELECT id, media_id, start_at, end_at, cancelled_at
		 FROM syncs
		 WHERE cancelled_at IS NULL AND end_at > $1
		 ORDER BY id DESC LIMIT 1`, now,
	).Scan(&sy.ID, &sy.MediaID, &sy.StartAt, &sy.EndAt, &sy.CancelledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sy, nil
}

// StartSync replaces any pending/active sync with a new one.
func (s *Store) StartSync(ctx context.Context, mediaID string, startAt, endAt, now int64) (model.Sync, error) {
	var sy model.Sync
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media WHERE id = $1)`, mediaID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: media %q", ErrNotFound, mediaID)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE syncs SET cancelled_at = $1 WHERE cancelled_at IS NULL AND end_at > $1`, now); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`INSERT INTO syncs (media_id, start_at, end_at) VALUES ($1,$2,$3)
			 RETURNING id, media_id, start_at, end_at, cancelled_at`,
			mediaID, startAt, endAt,
		).Scan(&sy.ID, &sy.MediaID, &sy.StartAt, &sy.EndAt, &sy.CancelledAt)
	})
	return sy, err
}

// CancelActiveSync ends the current sync early. Returns false if there was none.
func (s *Store) CancelActiveSync(ctx context.Context, now int64) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE syncs SET cancelled_at = $1 WHERE cancelled_at IS NULL AND end_at > $1`, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
