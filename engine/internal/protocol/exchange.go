package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"jeval/engine/internal/exchange"
	"jeval/engine/internal/model"
	"jeval/engine/internal/storage"
)

type exchangeStore interface {
	Current(context.Context, string) (model.Record, error)
	SaveExchange(context.Context, model.Run, []model.Event) error
	CheckExportPath(context.Context, string) error
}

func (s *scanService) exportRecord(req Request) Response {
	store, ok := s.store.(exchangeStore)
	if !ok {
		return failure(req.ID, "METHOD_NOT_FOUND", "Record exchange requires a persistent library")
	}
	var params struct {
		RunID  string `json:"runId"`
		Path   string `json:"path"`
		Format string `json:"format"`
	}
	if json.Unmarshal(req.Params, &params) != nil || params.RunID == "" || len(params.RunID) > 128 || !filepath.IsAbs(params.Path) || len(params.Path) > 2048 || (params.Format != "json" && params.Format != "markdown") {
		return failure(req.ID, "INVALID_PARAMS", "Expected a run ID, absolute output path (max 2048 bytes), and json or markdown format")
	}
	found := false
	for _, run := range s.record.Runs {
		if run.ID == params.RunID && !run.Demo {
			found = true
			break
		}
	}
	if !found {
		return failure(req.ID, "NOT_FOUND", "Saved run not found")
	}
	if err := store.CheckExportPath(context.Background(), params.Path); err != nil {
		return failure(req.ID, "EXPORT_FAILED", err.Error())
	}
	record, err := store.Current(context.Background(), params.RunID)
	if err != nil {
		return failure(req.ID, "EXPORT_FAILED", err.Error())
	}
	data, err := exchange.Encode(record, params.Format)
	if err != nil {
		return failure(req.ID, "EXPORT_FAILED", err.Error())
	}
	if err := exchange.Write(params.Path, data); err != nil {
		return failure(req.ID, "EXPORT_FAILED", err.Error())
	}
	run := record.Runs[0]
	return Response{Type: "response", Version: Version, ID: req.ID, Result: map[string]any{"path": params.Path, "format": params.Format, "snapshotId": run.ImportInfo.SnapshotID, "eventCount": run.EventCount}}
}

func (s *scanService) importRecord(req Request) Response {
	store, ok := s.store.(exchangeStore)
	if !ok {
		return failure(req.ID, "METHOD_NOT_FOUND", "Record exchange requires a persistent library")
	}
	var params struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(req.Params, &params) != nil || !filepath.IsAbs(params.Path) || len(params.Path) > 2048 {
		return failure(req.ID, "INVALID_PARAMS", "Expected a selected record file's absolute path (max 2048 bytes)")
	}
	record, err := exchange.Read(params.Path)
	if err != nil {
		return failure(req.ID, "IMPORT_FAILED", err.Error())
	}
	run := record.Runs[0]
	index, err := replacementIndex(&s.record, run, len(record.Events))
	if err != nil {
		return failure(req.ID, "IMPORT_LIMIT", err.Error())
	}
	if err := store.SaveExchange(context.Background(), run, record.Events); err != nil {
		if errors.Is(err, storage.ErrExchangeConflict) {
			return failure(req.ID, "RECORD_CONFLICT", err.Error())
		}
		return failure(req.ID, "IMPORT_FAILED", err.Error())
	}
	// SaveExchange preserves an existing source's permission for idempotent
	// imports; a new source has no local source-reading authorization.
	run.ReadOnly = true
	if index < 0 {
		s.record.Runs = append(s.record.Runs, run)
	} else {
		run.ReadOnly = s.record.Runs[index].ReadOnly
		s.record.Runs[index] = run
	}
	return Response{Type: "response", Version: Version, ID: req.ID, Result: map[string]any{"run": run, "replaced": index >= 0}}
}
