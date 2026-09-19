// Package model holds the plain data structs shared by the store, the
// scheduler and the HTTP layer. JSON tags are camelCase to match the frontend.
//
// Every timestamp in this project is an int64 of unix milliseconds (UTC).
// We deliberately avoid time.Time on the wire so that Go and TypeScript agree
// exactly, with no timezone or formatting ambiguity.
package model

type MediaType string

const (
	MediaImage MediaType = "image"
	MediaVideo MediaType = "video"
	MediaBlank MediaType = "blank"
)

type Media struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       MediaType `json:"type"`
	URL        *string   `json:"url"` // nil for blank
	DurationMs int64     `json:"durationMs"`
}

// PlaylistItem is one entry in a window's list. The same media may appear more
// than once, so identity is ID (the row id), never MediaID.
type PlaylistItem struct {
	ID         int64  `json:"id"`
	MediaID    string `json:"mediaId"`
	Position   int    `json:"position"`
	DurationMs int64  `json:"durationMs"` // effective duration: override, else media's
}

type Window struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	CycleEpoch  int64          `json:"cycleEpoch"`
	AnchorAt    *int64         `json:"anchorAt"`
	AnchorIndex *int           `json:"anchorIndex"`
	Version     int64          `json:"version"`
	Position    int            `json:"position"`
	Items       []PlaylistItem `json:"items"`
}

type Sync struct {
	ID          int64  `json:"id"`
	MediaID     string `json:"mediaId"`
	StartAt     int64  `json:"startAt"`
	EndAt       int64  `json:"endAt"`
	CancelledAt *int64 `json:"cancelledAt,omitempty"`
}

// State is the single payload the frontend fetches; it contains everything
// needed to render every window.
type State struct {
	ServerTimeMs int64    `json:"serverTimeMs"`
	CycleMs      int64    `json:"cycleMs"`
	Media        []Media  `json:"media"`
	Windows      []Window `json:"windows"`
	ActiveSync   *Sync    `json:"activeSync"`
}
