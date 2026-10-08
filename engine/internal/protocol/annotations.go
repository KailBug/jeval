package protocol

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"

	"jeval/engine/internal/model"
	"jeval/engine/internal/storage"
)

type historyStore interface {
	SnapshotRun(context.Context, string, string) (model.Run, error)
	Snapshots(context.Context, string, int, int) ([]model.Run, int, error)
	EventsAt(context.Context, string, string, string, string, int, int) ([]model.Event, int, error)
}
type annotationStore interface {
	Annotation(context.Context, string, string, *string) (*model.Annotation, error)
	Annotations(context.Context, string, string, int, int) ([]model.Annotation, int, error)
	SaveAnnotation(context.Context, string, string, *string, string, string, int, bool) (*model.Annotation, error)
}

type reviewParams struct {
	RunID            string          `json:"runId"`
	SnapshotID       string          `json:"snapshotId"`
	EventID          json.RawMessage `json:"eventId"`
	Judgement        string          `json:"judgement"`
	Note             string          `json:"note"`
	ExpectedRevision *int            `json:"expectedRevision"`
	Offset           int             `json:"offset"`
	Limit            *int            `json:"limit"`
}

func reviewInput(data []byte, p *reviewParams) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(p) != nil || p.RunID == "" || len(p.RunID) > 128 || len(p.SnapshotID) > 128 || p.Offset < 0 || p.Offset > 1000000000 || p.Limit != nil && (*p.Limit < 1 || *p.Limit > 50) {
		return false
	}
	return decoder.Decode(new(any)) == io.EOF
}
func reviewResponse(req Request, result any, err error) Response {
	if err == nil {
		return Response{Type: "response", Version: Version, ID: req.ID, Result: result}
	}
	code := "STORAGE_FAILED"
	switch {
	case errors.Is(err, sql.ErrNoRows):
		code = "NOT_FOUND"
	case errors.Is(err, storage.ErrAnnotationConflict):
		code = "ANNOTATION_CONFLICT"
	case errors.Is(err, storage.ErrAnnotationInput):
		code = "INVALID_PARAMS"
	case errors.Is(err, storage.ErrAnnotationLimit):
		code = "ANNOTATION_LIMIT"
	}
	return failure(req.ID, code, err.Error())
}

func (s *scanService) review(req Request) Response {
	var p reviewParams
	if !reviewInput(req.Params, &p) {
		return failure(req.ID, "INVALID_PARAMS", "Expected a bounded snapshot/annotation request")
	}
	limit := 20
	if p.Limit != nil {
		limit = *p.Limit
	}
	ctx := context.Background()
	if req.Method == "runs.snapshots" || req.Method == "runs.snapshot" {
		store, ok := s.store.(historyStore)
		if !ok {
			return failure(req.ID, "METHOD_NOT_FOUND", "Snapshot history requires persistent storage")
		}
		if req.Method == "runs.snapshot" {
			if p.SnapshotID == "" {
				return failure(req.ID, "INVALID_PARAMS", "snapshotId is required")
			}
			run, err := store.SnapshotRun(ctx, p.RunID, p.SnapshotID)
			return reviewResponse(req, run, err)
		}
		items, total, err := store.Snapshots(ctx, p.RunID, p.Offset, limit)
		page := paginate(items, 0, limit)
		page.Total = total
		page.NextOffset = nil
		next := p.Offset + len(page.Items)
		if next < total {
			page.NextOffset = &next
		}
		return reviewResponse(req, page, err)
	}
	store, ok := s.store.(annotationStore)
	if !ok {
		return failure(req.ID, "METHOD_NOT_FOUND", "Annotations require persistent storage")
	}
	if p.SnapshotID == "" {
		return failure(req.ID, "INVALID_PARAMS", "snapshotId is required")
	}
	if req.Method == "annotations.list" {
		items, total, err := store.Annotations(ctx, p.RunID, p.SnapshotID, p.Offset, limit)
		page := paginate(items, 0, limit)
		page.Total = total
		page.NextOffset = nil
		next := p.Offset + len(page.Items)
		if next < total {
			page.NextOffset = &next
		}
		return reviewResponse(req, page, err)
	}
	var eventID *string
	if len(p.EventID) == 0 || string(p.EventID) != "null" && (json.Unmarshal(p.EventID, &eventID) != nil || eventID == nil || *eventID == "" || len(*eventID) > 256) {
		return failure(req.ID, "INVALID_PARAMS", "eventId must be null for a snapshot or a saved event ID")
	}
	if req.Method == "annotations.get" {
		a, err := store.Annotation(ctx, p.RunID, p.SnapshotID, eventID)
		return reviewResponse(req, map[string]any{"annotation": a}, err)
	}
	if p.ExpectedRevision == nil {
		return failure(req.ID, "INVALID_PARAMS", "expectedRevision is required")
	}
	deleted := req.Method == "annotations.delete"
	if deleted {
		p.Judgement = "uncertain"
		p.Note = ""
	}
	a, err := store.SaveAnnotation(ctx, p.RunID, p.SnapshotID, eventID, p.Judgement, p.Note, *p.ExpectedRevision, deleted)
	return reviewResponse(req, a, err)
}
