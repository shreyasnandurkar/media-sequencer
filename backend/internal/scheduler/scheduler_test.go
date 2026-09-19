package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shreyasnandurkar/media-sequencer/backend/internal/model"
)

const vectorPath = "../../../testdata/schedule_vectors.json"

type vectorFile struct {
	Cases []vectorCase `json:"cases"`
}

type vectorCase struct {
	Name    string  `json:"name"`
	CycleMs int64   `json:"cycleMs"`
	Window  vecWin  `json:"window"`
	Items   []vecIt `json:"items"`
	Now     int64   `json:"now"`
	Exp     vecExp  `json:"expected"`
}

type vecWin struct {
	CycleEpoch  int64  `json:"cycleEpoch"`
	AnchorAt    *int64 `json:"anchorAt"`
	AnchorIndex *int   `json:"anchorIndex"`
}

type vecIt struct {
	ID         int64 `json:"id"`
	DurationMs int64 `json:"durationMs"`
}

type vecExp struct {
	Blank           bool  `json:"blank"`
	Index           int   `json:"index"`
	ElapsedInItemMs int64 `json:"elapsedInItemMs"`
	RemainingMs     int64 `json:"remainingMs"`
}

func TestResolveAgainstSharedVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.FromSlash(vectorPath))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var vf vectorFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if len(vf.Cases) == 0 {
		t.Fatal("no vector cases found")
	}

	for _, c := range vf.Cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			w := model.Window{
				CycleEpoch:  c.Window.CycleEpoch,
				AnchorAt:    c.Window.AnchorAt,
				AnchorIndex: c.Window.AnchorIndex,
			}
			list := make([]model.PlaylistItem, len(c.Items))
			for i, it := range c.Items {
				list[i] = model.PlaylistItem{ID: it.ID, Position: i, DurationMs: it.DurationMs}
			}

			got := Resolve(w, list, c.CycleMs, c.Now)

			if got.Blank != c.Exp.Blank {
				t.Errorf("blank: got %v want %v", got.Blank, c.Exp.Blank)
			}
			if got.Index != c.Exp.Index {
				t.Errorf("index: got %d want %d", got.Index, c.Exp.Index)
			}
			if got.ElapsedInItemMs != c.Exp.ElapsedInItemMs {
				t.Errorf("elapsedInItemMs: got %d want %d", got.ElapsedInItemMs, c.Exp.ElapsedInItemMs)
			}
			if got.RemainingMs != c.Exp.RemainingMs {
				t.Errorf("remainingMs: got %d want %d", got.RemainingMs, c.Exp.RemainingMs)
			}

			if got.StartedAtMs != c.Now-got.ElapsedInItemMs {
				t.Errorf("startedAtMs inconsistent: %d", got.StartedAtMs)
			}
			if got.RemainingMs <= 0 {
				t.Errorf("remainingMs must be > 0, got %d", got.RemainingMs)
			}
			if got.CycleEndMs-got.CycleStartMs != c.CycleMs {
				t.Errorf("cycle span wrong: %d", got.CycleEndMs-got.CycleStartMs)
			}
		})
	}
}

func TestCycleStartIsStableAcrossACycle(t *testing.T) {
	cs := CycleStart(epoch, cycle, epoch+cycle+1)
	if cs != epoch+cycle {
		t.Fatalf("got %d", cs)
	}
	if CycleStart(epoch, cycle, epoch+2*cycle-1) != epoch+cycle {
		t.Fatal("should still be in cycle 1")
	}
	if CycleStart(epoch, cycle, epoch+2*cycle) != epoch+2*cycle {
		t.Fatal("should have advanced to cycle 2")
	}

	if got := CycleStart(epoch, cycle, epoch-1); got != epoch-cycle {
		t.Fatalf("pre-epoch: got %d want %d", got, epoch-cycle)
	}
}

const (
	epoch = int64(1_000_000_000_000)
	cycle = int64(18_000_000)
)

func mkItems(specs ...[2]int64) []model.PlaylistItem {
	out := make([]model.PlaylistItem, len(specs))
	for i, s := range specs {
		out[i] = model.PlaylistItem{ID: s[0], Position: i, DurationMs: s[1]}
	}
	return out
}

func editAndResolve(t *testing.T, w model.Window, oldItems, newItems []model.PlaylistItem, now int64) (Result, Result) {
	t.Helper()
	before := Resolve(w, oldItems, cycle, now)
	at, idx := Reanchor(before, newItems, now)
	w.AnchorAt, w.AnchorIndex = at, idx
	after := Resolve(w, newItems, cycle, now)
	return before, after
}

func assertNoJump(t *testing.T, before, after Result) {
	t.Helper()
	if after.ItemID != before.ItemID {
		t.Errorf("item changed: %d -> %d", before.ItemID, after.ItemID)
	}
	if after.StartedAtMs != before.StartedAtMs {
		t.Errorf("start time changed: %d -> %d", before.StartedAtMs, after.StartedAtMs)
	}
	if after.ElapsedInItemMs != before.ElapsedInItemMs {
		t.Errorf("elapsed changed: %d -> %d", before.ElapsedInItemMs, after.ElapsedInItemMs)
	}
}

func TestReanchorAppendKeepsCurrentItem(t *testing.T) {
	w := model.Window{CycleEpoch: epoch}
	old := mkItems([2]int64{1, 10000}, [2]int64{2, 30000})
	now := epoch + 25000
	newItems := mkItems([2]int64{1, 10000}, [2]int64{2, 30000}, [2]int64{3, 8000})

	before, after := editAndResolve(t, w, old, newItems, now)
	if before.ItemID != 2 {
		t.Fatalf("precondition: expected item 2, got %d", before.ItemID)
	}
	assertNoJump(t, before, after)
}

func TestReanchorInsertBeforeCurrentKeepsCurrentItem(t *testing.T) {
	w := model.Window{CycleEpoch: epoch}
	old := mkItems([2]int64{1, 10000}, [2]int64{2, 30000}, [2]int64{3, 8000})
	now := epoch + 25000

	newItems := mkItems([2]int64{9, 5000}, [2]int64{1, 10000}, [2]int64{2, 30000}, [2]int64{3, 8000})

	before, after := editAndResolve(t, w, old, newItems, now)
	assertNoJump(t, before, after)
	if after.Index != 2 {
		t.Errorf("expected the current item at its new index 2, got %d", after.Index)
	}
}

func TestReanchorDeletingAnotherItemKeepsCurrentItem(t *testing.T) {
	w := model.Window{CycleEpoch: epoch}
	old := mkItems([2]int64{1, 10000}, [2]int64{2, 30000}, [2]int64{3, 8000})
	now := epoch + 25000
	newItems := mkItems([2]int64{2, 30000}, [2]int64{3, 8000})

	before, after := editAndResolve(t, w, old, newItems, now)
	assertNoJump(t, before, after)
}

func TestReanchorDeletingCurrentItemStartsNextImmediately(t *testing.T) {
	w := model.Window{CycleEpoch: epoch}
	old := mkItems([2]int64{1, 10000}, [2]int64{2, 30000}, [2]int64{3, 8000})
	now := epoch + 25000
	newItems := mkItems([2]int64{1, 10000}, [2]int64{3, 8000})

	before, after := editAndResolve(t, w, old, newItems, now)
	if before.Index != 1 {
		t.Fatalf("precondition failed, index %d", before.Index)
	}
	if after.ItemID != 3 {
		t.Errorf("expected to fall through to item 3, got %d", after.ItemID)
	}
	if after.ElapsedInItemMs != 0 || after.StartedAtMs != now {
		t.Errorf("replacement item should start now: elapsed=%d startedAt=%d", after.ElapsedInItemMs, after.StartedAtMs)
	}
}

func TestReanchorClearedWhenListBecomesEmpty(t *testing.T) {
	w := model.Window{CycleEpoch: epoch}
	old := mkItems([2]int64{1, 10000})
	now := epoch + 3000

	before := Resolve(w, old, cycle, now)
	at, idx := Reanchor(before, nil, now)
	if at != nil || idx != nil {
		t.Fatalf("expected a nil anchor for an empty list, got %v %v", at, idx)
	}
	w.AnchorAt, w.AnchorIndex = at, idx
	if got := Resolve(w, nil, cycle, now); !got.Blank {
		t.Fatal("empty playlist must resolve to blank")
	}
}

func TestAnchorDoesNotSurviveIntoTheNextCycle(t *testing.T) {
	anchorAt := epoch + 1_000_000
	anchorIdx := 2
	w := model.Window{CycleEpoch: epoch, AnchorAt: &anchorAt, AnchorIndex: &anchorIdx}
	list := mkItems([2]int64{1, 10000}, [2]int64{2, 30000}, [2]int64{3, 8000})

	got := Resolve(w, list, cycle, epoch+cycle)
	if got.Index != 0 || got.ElapsedInItemMs != 0 {
		t.Fatalf("new cycle must restart at item 0, got index %d elapsed %d", got.Index, got.ElapsedInItemMs)
	}
}
