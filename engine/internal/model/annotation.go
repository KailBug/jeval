package model

// Annotation is a local human opinion about one immutable snapshot or event.
// Event is derived from saved evidence, never supplied by a caller.
type Annotation struct {
	RunID      string  `json:"runId"`
	SnapshotID string  `json:"snapshotId"`
	EventID    *string `json:"eventId"`
	Judgement  string  `json:"judgement"`
	Note       string  `json:"note"`
	Revision   int     `json:"revision"`
	UpdatedAt  string  `json:"updatedAt"`
	Deleted    bool    `json:"deleted"`
	Event      *Event  `json:"event"`
}
