package model

type Run struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Project    string  `json:"project"`
	Source     string  `json:"source"`
	Demo       bool    `json:"demo"`
	Status     string  `json:"status"`
	StartedAt  *string `json:"startedAt"`
	DurationMs *int64  `json:"durationMs"`
	Tokens     *int64  `json:"tokens"`
	EventCount int     `json:"eventCount"`
}

type EvidenceRef struct {
	SourceID string `json:"sourceId"`
	Location string `json:"location"`
	Line     int    `json:"line"`
}

type Event struct {
	ID        string      `json:"id"`
	RunID     string      `json:"runId"`
	Sequence  int         `json:"sequence"`
	Kind      string      `json:"kind"`
	Role      string      `json:"role"`
	Title     string      `json:"title"`
	Content   string      `json:"content"`
	Timestamp *string     `json:"timestamp"`
	ParentID  *string     `json:"parentId"`
	Evidence  EvidenceRef `json:"evidence"`
}

type Record struct {
	SchemaVersion int     `json:"schemaVersion"`
	Runs          []Run   `json:"runs"`
	Events        []Event `json:"events"`
}
