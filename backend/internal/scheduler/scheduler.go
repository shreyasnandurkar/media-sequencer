package scheduler

import "github.com/shreyasnandurkar/media-sequencer/backend/internal/model"

type Result struct {
	Blank bool `json:"blank"`

	Index           int    `json:"index"`
	ItemID          int64  `json:"itemId"`
	MediaID         string `json:"mediaId"`
	ElapsedInItemMs int64  `json:"elapsedInItemMs"`

	RemainingMs int64 `json:"remainingMs"`

	StartedAtMs  int64 `json:"startedAtMs"`
	CycleStartMs int64 `json:"cycleStartMs"`
	CycleEndMs   int64 `json:"cycleEndMs"`
}

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

func TotalDuration(items []model.PlaylistItem) int64 {
	var total int64
	for _, it := range items {
		total += it.DurationMs
	}
	return total
}

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

	t0, idx := cs, 0
	if w.AnchorAt != nil && w.AnchorIndex != nil && *w.AnchorAt >= cs && *w.AnchorAt <= now {
		t0 = *w.AnchorAt
		idx = clamp(*w.AnchorIndex, 0, len(items)-1)
	}

	elapsed := now - t0

	for i := idx; i < len(items); i++ {
		if elapsed < items[i].DurationMs {
			return at(items, i, elapsed, now, cs, cycleEnd)
		}
		elapsed -= items[i].DurationMs
	}

	elapsed %= total
	for i := 0; i < len(items); i++ {
		if elapsed < items[i].DurationMs {
			return at(items, i, elapsed, now, cs, cycleEnd)
		}
		elapsed -= items[i].DurationMs
	}

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

func Reanchor(before Result, newItems []model.PlaylistItem, now int64) (*int64, *int) {
	if len(newItems) == 0 {
		return nil, nil
	}
	if !before.Blank {

		for j, it := range newItems {
			if it.ID == before.ItemID {
				start := before.StartedAtMs
				idx := j
				return &start, &idx
			}
		}
	}

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
