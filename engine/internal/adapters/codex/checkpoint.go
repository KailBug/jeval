package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"jeval/engine/internal/model"
)

const CheckpointVersion = 1
const MaxCheckpointBytes = 1024 * 1024

// Checkpoint holds parser metadata only, never original message bodies. It is
// bound to the current normalized snapshot and committed in the same transaction.
type Checkpoint struct {
	Version        int               `json:"version"`
	SourceID       string            `json:"sourceId"`
	SnapshotID     string            `json:"snapshotId"`
	AdapterVersion string            `json:"adapterVersion"`
	SHA256         string            `json:"sha256"`
	Offset         int               `json:"offset"`
	Lines          int               `json:"lines"`
	Resumable      bool              `json:"resumable"`
	HasTitle       bool              `json:"hasTitle"`
	Calls          map[string]string `json:"calls"`
}

type UpdateReport struct {
	Mode          string `json:"mode"`
	Reason        string `json:"reason"`
	VerifiedBytes int    `json:"verifiedBytes"`
	ParsedLines   int    `json:"parsedLines"`
}

type parseState struct {
	Seed     *model.Record
	Lines    int
	Calls    map[string]string
	HasTitle bool
	Unsafe   bool
}

// ReadUpdate always reads and hashes the bounded file: metadata/mtime alone
// cannot prove append-only writes. Only JSON decoding/normalization is resumed.
// Unsafe histories fall back to the same two-pass parser as ReadContext.
func ReadUpdate(ctx context.Context, path string, old *Checkpoint, saved *model.Record, progress func(string)) (model.Run, []model.Event, *Checkpoint, UpdateReport, error) {
	report := UpdateReport{Mode: "full", Reason: "no-checkpoint"}
	if progress != nil {
		progress("reading")
	}
	path, data, err := readSource(ctx, path)
	if err != nil {
		return model.Run{}, nil, nil, report, err
	}
	report.VerifiedBytes = len(data)
	state := &parseState{}
	if old != nil {
		report.Reason = "checkpoint-invalid"
		if checkpointMatches(old, saved, path) {
			if len(data) < old.Offset {
				report.Reason = "source-truncated"
			} else if fmt.Sprintf("%x", sha256.Sum256(data[:old.Offset])) != old.SHA256 {
				report.Reason = "source-rewritten"
			} else if len(data) == old.Offset {
				if err := ctx.Err(); err != nil {
					return model.Run{}, nil, nil, report, err
				}
				report.Mode, report.Reason = "unchanged", "same-bytes"
				return saved.Runs[0], saved.Events, old, report, nil
			} else if old.Lines != bytes.Count(data[:old.Offset], []byte{'\n'}) || old.Resumable && data[old.Offset-1] != '\n' {
				report.Reason = "checkpoint-invalid"
			} else if !old.Resumable {
				report.Reason = "unsafe-prefix"
			} else {
				// A suffix projection can refer to canonical text/turns in the prefix.
				// Reparse both passes rather than trying to infer missing source content.
				projected, err := hasProjection(ctx, data[old.Offset:])
				if err != nil {
					return model.Run{}, nil, nil, report, err
				}
				if projected {
					report.Reason = "projection-reconciliation"
				} else {
					state = &parseState{Seed: saved, Lines: old.Lines, Calls: old.Calls, HasTitle: old.HasTitle}
					report.Mode, report.Reason = "incremental", "verified-prefix"
				}
			}
		}
	}
	if progress != nil {
		progress("parsing")
	}
	run, events, err := normalize(ctx, path, data, state)
	if err != nil {
		return model.Run{}, nil, nil, report, err
	}
	report.ParsedLines = bytes.Count(data, []byte{'\n'}) + 1 - state.Lines
	cp := &Checkpoint{Version: CheckpointVersion, SourceID: run.ID, SnapshotID: run.ImportInfo.SnapshotID, AdapterVersion: AdapterVersion,
		SHA256: run.ImportInfo.SHA256, Offset: len(data), Lines: bytes.Count(data, []byte{'\n'}),
		Resumable: len(data) > 0 && data[len(data)-1] == '\n' && !state.Unsafe, HasTitle: state.HasTitle, Calls: state.Calls}
	encoded, err := json.Marshal(cp)
	if err != nil {
		return model.Run{}, nil, nil, report, err
	}
	if len(encoded) > MaxCheckpointBytes || !cp.Resumable {
		cp.Resumable = false
		cp.Calls = nil
	}
	return run, events, cp, report, nil
}

func checkpointMatches(cp *Checkpoint, saved *model.Record, path string) bool {
	if saved == nil || len(saved.Runs) != 1 || cp.Version != CheckpointVersion || cp.AdapterVersion != AdapterVersion || cp.Offset < 1 || cp.Offset > MaxFileBytes || cp.Lines < 0 || cp.Lines >= 50000 {
		return false
	}
	run := saved.Runs[0]
	info := run.ImportInfo
	if info == nil || run.ReadOnly || cp.SourceID != run.ID || cp.SnapshotID != info.SnapshotID || cp.SHA256 != info.SHA256 || info.File != path || cp.AdapterVersion != info.AdapterVersion {
		return false
	}
	if cp.Resumable && (cp.Lines < 1 || len(saved.Events) == 0 || len(cp.Calls) > MaxEvents) {
		return false
	}
	calls := map[string]bool{}
	for _, event := range saved.Events {
		if event.Kind == "tool_call" {
			calls[event.ID] = true
		}
	}
	for _, id := range cp.Calls {
		if !calls[id] {
			return false
		}
	}
	return true
}

func hasProjection(ctx context.Context, data []byte) (bool, error) {
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		var row rolloutLine
		var p payload
		if json.Unmarshal(raw, &row) == nil && row.Type == "event_msg" && json.Unmarshal(row.Payload, &p) == nil && p.Type == "item_completed" {
			return true, nil
		}
	}
	return false, nil
}
