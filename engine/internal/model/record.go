package model

type Run struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Project string `json:"project"`
	Source  string `json:"source"`
	Demo    bool   `json:"demo"`
	// ReadOnly is derived library metadata, not part of an immutable snapshot.
	ReadOnly   bool        `json:"readOnly,omitempty"`
	Status     string      `json:"status"`
	StartedAt  *string     `json:"startedAt"`
	DurationMs *int64      `json:"durationMs"`
	Tokens     *int64      `json:"tokens"`
	EventCount int         `json:"eventCount"`
	ImportInfo *ImportInfo `json:"importInfo,omitempty"`
}

type ImportWarning struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

type ImportInfo struct {
	AdapterVersion string          `json:"adapterVersion"`
	SnapshotID     string          `json:"snapshotId"`
	File           string          `json:"file"`
	SHA256         string          `json:"sha256"`
	SessionID      string          `json:"sessionId"`
	CLIVersion     string          `json:"cliVersion"`
	HistoryMode    string          `json:"historyMode"`
	ParentThreadID string          `json:"parentThreadId,omitempty"`
	ForkedFromID   string          `json:"forkedFromId,omitempty"`
	WarningCount   int             `json:"warningCount"`
	Warnings       []ImportWarning `json:"warnings"`
}

type EvidenceRef struct {
	SnapshotID string `json:"snapshotId,omitempty"`
	SourceID   string `json:"sourceId"`
	Location   string `json:"location"`
	Line       int    `json:"line"`
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
