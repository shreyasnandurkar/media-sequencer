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
	URL        *string   `json:"url"`
	DurationMs int64     `json:"durationMs"`
}

type PlaylistItem struct {
	ID         int64  `json:"id"`
	MediaID    string `json:"mediaId"`
	Position   int    `json:"position"`
	DurationMs int64  `json:"durationMs"`
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

type State struct {
	ServerTimeMs int64    `json:"serverTimeMs"`
	CycleMs      int64    `json:"cycleMs"`
	Media        []Media  `json:"media"`
	Windows      []Window `json:"windows"`
	ActiveSync   *Sync    `json:"activeSync"`
}
