// Package scheduler answers one question: "given a window and a moment in
// time, what should be on screen?"
//
// It is PURE: no database, no clock, no I/O. `now` is always passed in. That
// makes it trivially testable and lets an identical TypeScript port
// (frontend/src/lib/scheduler.ts) compute the same answer in the browser.
// Because every client computes rather than receives the current item, extra
// tabs, reloads and late joiners line up automatically.
package scheduler

import "github.com/shreyasnandurkar/media-sequencer/backend/internal/model"

// Result describes what a window shows at a given instant.
type Result struct {
	// Blank is true when there is nothing to play at all (empty playlist).
	// A *blank media item* is NOT this; that is a normal item with Blank=false.
	Blank bool `json:"blank"`
	// Index is the 0-based playlist position, or -1 when Blank.
	Index           int    `json:"index"`
	ItemID          int64  `json:"itemId"`
	MediaID         string `json:"mediaId"`
	ElapsedInItemMs int64  `json:"elapsedInItemMs"`
	// RemainingMs is capped at the end of the cycle: an item that straddles the
	// 5h boundary is cut off there and the playlist restarts at item 0.
	RemainingMs int64 `json:"remainingMs"`
	// StartedAtMs is when the current item began (now - ElapsedInItemMs).
	// (ItemID, StartedAtMs) is the render identity used by the UI: it only
	// remounts the <video>/<img> when that pair changes.
	StartedAtMs  int64 `json:"startedAtMs"`
	CycleStartMs int64 `json:"cycleStartMs"`
	CycleEndMs   int64 `json:"cycleEndMs"`
}

// CycleStart returns the start of the cycle containing `now`.
//
// Note the explicit floor division: Go's `/` truncates toward zero, which would
// be wrong for `now < epoch` (a window seeded with a future epoch, or clock
// skew). floorDiv keeps cycles evenly tiled in both directions.
func CycleStart(cycleEpoch, cycleMs, now int64) int64 {
	return cycleEpoch + floorDiv(now-cycleEpoch, cycleMs)*cycleMs
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// TotalDuration sums the effective durations of a playlist.
func TotalDuration(items []model.PlaylistItem) int64 {
	var total int64
	for _, it := range items {
		total += it.DurationMs
	}
	return total
}

// Resolve is the core function. See TASK.md §2.2.
func Resolve(w model.Window, items []model.PlaylistItem, cycleMs, now int64) Result {
	cs := CycleStart(w.CycleEpoch, cycleMs, now)
	cycleEnd := cs + cycleMs

	total := TotalDuration(items)
	if len(items) == 0 || total <= 0 {
		return Result{
			Blank:           true,
			Index:           -1,
			ElapsedInItemMs: now - cs,
			RemainingMs:     cycleEnd - now,
			StartedAtMs:     cs,
			CycleStartMs:    cs,
			CycleEndMs:      cycleEnd,
		}
	}

	// Where does playback in *this* cycle start from?
	// An anchor (set by a playlist edit, see Reanchor) only counts if it was
	// placed inside the current cycle and is not in the future. Otherwise the
	// cycle restarts cleanly at item 0 — that is the "5h restart" rule.
	t0, idx := cs, 0
	if w.AnchorAt != nil && w.AnchorIndex != nil && *w.AnchorAt >= cs && *w.AnchorAt <= now {
		t0 = *w.AnchorAt
		idx = clamp(*w.AnchorIndex, 0, len(items)-1)
	}

	elapsed := now - t0

	// Pass 1: the partial run from idx to the end of the list.
	for i := idx; i < len(items); i++ {
		if elapsed < items[i].DurationMs {
			return at(items, i, elapsed, now, cs, cycleEnd)
		}
		elapsed -= items[i].DurationMs
	}
	// Pass 2: whole loops from item 0. The modulo skips however many complete
	// loops have passed in O(1), so this stays O(n) no matter how long the
	// window has been running.
	elapsed %= total
	for i := 0; i < len(items); i++ {
		if elapsed < items[i].DurationMs {
			return at(items, i, elapsed, now, cs, cycleEnd)
		}
		elapsed -= items[i].DurationMs
	}

	// Unreachable: elapsed < total guarantees a hit above. Fail safe.
	return at(items, 0, 0, now, cs, cycleEnd)
}

func at(items []model.PlaylistItem, i int, elapsed, now, cs, cycleEnd int64) Result {
	remaining := items[i].DurationMs - elapsed
	if capped := cycleEnd - now; capped < remaining {
		remaining = capped
	}
	return Result{
		Index:           i,
		ItemID:          items[i].ID,
		MediaID:         items[i].MediaID,
		ElapsedInItemMs: elapsed,
		RemainingMs:     remaining,
		StartedAtMs:     now - elapsed,
		CycleStartMs:    cs,
		CycleEndMs:      cycleEnd,
	}
}

// Reanchor computes the new (anchorAt, anchorIndex) after a playlist edit so
// the item currently on screen is not interrupted. See TASK.md §2.3.
//
// `before` must be the Resolve() result computed with the OLD list at the same
// `now` that the mutation happens at.
func Reanchor(before Result, newItems []model.PlaylistItem, now int64) (*int64, *int) {
	if len(newItems) == 0 {
		return nil, nil
	}
	if !before.Blank {
		// Did the item that was playing survive the edit? Match on the row id,
		// not the media id: the same media can appear several times in a list.
		for j, it := range newItems {
			if it.ID == before.ItemID {
				start := before.StartedAtMs
				idx := j
				return &start, &idx
			}
		}
	}
	// The playing item is gone (or there was nothing playing): continue from
	// the nearest surviving position, starting now.
	idx := clamp(before.Index, 0, len(newItems)-1)
	t := now
	return &t, &idx
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
