package protocol

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"jeval/engine/internal/storage"
)

func (s *scanService) eventContent(req Request) Response {
	if s.store == nil {
		return failure(req.ID, "METHOD_NOT_FOUND", "Full content requires a persistent library")
	}
	var p struct {
		RunID      string          `json:"runId"`
		SnapshotID string          `json:"snapshotId"`
		EventID    string          `json:"eventId"`
		Offset     json.RawMessage `json:"offset"`
	}
	if json.Unmarshal(req.Params, &p) != nil || p.RunID == "" || len(p.RunID) > 128 || p.SnapshotID == "" || len(p.SnapshotID) > 128 || p.EventID == "" || len(p.EventID) > 256 {
		return failure(req.ID, "INVALID_PARAMS", "Expected runId, snapshotId, eventId and a bounded UTF-8 byte offset")
	}
	var offset int
	if len(p.Offset) != 0 && (string(p.Offset) == "null" || json.Unmarshal(p.Offset, &offset) != nil) || offset < 0 || offset > storage.MaxBodyBytes {
		return failure(req.ID, "INVALID_PARAMS", "offset must be an integer from 0 to 16777216")
	}
	page, err := s.store.EventContent(context.Background(), p.RunID, p.SnapshotID, p.EventID, offset)
	if errors.Is(err, sql.ErrNoRows) {
		return failure(req.ID, "NOT_FOUND", "Event not found in the requested run and snapshot")
	}
	if errors.Is(err, storage.ErrContentOffset) {
		return failure(req.ID, "INVALID_PARAMS", err.Error())
	}
	if err != nil {
		return failure(req.ID, "STORAGE_FAILED", err.Error())
	}
	return Response{Type: "response", Version: Version, ID: req.ID, Result: page}
}
