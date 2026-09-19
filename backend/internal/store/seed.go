package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shreyasnandurkar/media-sequencer/backend/internal/model"
)

// ---------------------------------------------------------------------------
// SEED DATA
//
// The assignment brief refers to "example windows and media lists" but does not
// include them. Everything below is a placeholder that matches the shape of the
// brief. To swap in the real example lists, edit ONLY the two tables in this
// file (seedMedia and seedWindows) — nothing else in the codebase depends on
// these values.
// ---------------------------------------------------------------------------

func strptr(s string) *string { return &s }

var seedMedia = []model.Media{
	{ID: "M1", Name: "City skyline", Type: model.MediaImage, URL: strptr("https://picsum.photos/id/1015/1280/720"), DurationMs: 10_000},
	{ID: "M2", Name: "Big Buck Bunny (clip)", Type: model.MediaVideo, URL: strptr("https://test-videos.co.uk/vids/bigbuckbunny/mp4/h264/720/Big_Buck_Bunny_720_10s_1MB.mp4"), DurationMs: 10_000},
	{ID: "M3", Name: "Mountain road", Type: model.MediaImage, URL: strptr("https://picsum.photos/id/1025/1280/720"), DurationMs: 8_000},
	{ID: "M4", Name: "Jellyfish (clip)", Type: model.MediaVideo, URL: strptr("https://test-videos.co.uk/vids/jellyfish/mp4/h264/720/Jellyfish_720_10s_1MB.mp4"), DurationMs: 10_000},
	{ID: "M5", Name: "Harbour at dusk", Type: model.MediaImage, URL: strptr("https://picsum.photos/id/1043/1280/720"), DurationMs: 12_000},
	{ID: "B", Name: "Blank", Type: model.MediaBlank, URL: nil, DurationMs: 5_000},
}

// seedWindows lists each window's playlist as media ids, in order.
var seedWindows = []struct {
	ID       string
	Name     string
	Playlist []string
}{
	{ID: "W1", Name: "Window 1", Playlist: []string{"M1", "M2", "M3"}},
	{ID: "W2", Name: "Window 2", Playlist: []string{"M2", "B", "M4"}},
	{ID: "W3", Name: "Window 3", Playlist: []string{"M3", "M4", "M5", "M1"}},
	{ID: "W4", Name: "Window 4", Playlist: []string{"M5", "M2"}},
}

// Seed inserts the placeholder data, but only when the database is empty.
// Running it on every boot is therefore safe and data survives restarts.
func (s *Store) Seed(ctx context.Context) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		var mediaCount, windowCount int
		if err := tx.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM media), (SELECT COUNT(*) FROM windows)`).
			Scan(&mediaCount, &windowCount); err != nil {
			return err
		}
		if mediaCount > 0 || windowCount > 0 {
			s.log.Info("seed skipped, database already has data", "media", mediaCount, "windows", windowCount)
			return nil
		}

		for _, m := range seedMedia {
			if _, err := tx.Exec(ctx,
				`INSERT INTO media (id, name, type, url, duration_ms) VALUES ($1,$2,$3,$4,$5)`,
				m.ID, m.Name, string(m.Type), m.URL, m.DurationMs); err != nil {
				return err
			}
		}

		// All windows share one epoch — the start of the current UTC day — so
		// their 5h cycles are aligned with each other and with the wall clock.
		epoch := startOfUTCDay(time.Now())

		for i, w := range seedWindows {
			if _, err := tx.Exec(ctx,
				`INSERT INTO windows (id, name, cycle_epoch, version, position) VALUES ($1,$2,$3,1,$4)`,
				w.ID, w.Name, epoch, i); err != nil {
				return err
			}
			for pos, mediaID := range w.Playlist {
				if _, err := tx.Exec(ctx,
					`INSERT INTO playlist_items (window_id, media_id, position) VALUES ($1,$2,$3)`,
					w.ID, mediaID, pos); err != nil {
					return err
				}
			}
		}

		s.log.Info("seed applied", "media", len(seedMedia), "windows", len(seedWindows), "cycleEpoch", epoch)
		return nil
	})
}

func startOfUTCDay(t time.Time) int64 {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
}
