package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"jeval/engine/internal/adapters/codex"
	"jeval/engine/internal/model"
	"jeval/engine/internal/storage"
)

// Keeping this small interface also lets protocol tests inject disk failures at
// the save boundary without weakening the production store's transaction rules.
type libraryStore interface {
	RestoreContents(context.Context, *model.Record) (bool, error)
	EventContent(context.Context, string, string, string, int) (storage.ContentPage, error)
	Runs(context.Context) ([]model.Run, error)
	Events(context.Context, string, string, string, int, int) ([]model.Event, int, error)
	Save(context.Context, model.Run, []model.Event) error
	SaveCheckpoint(context.Context, model.Run, []model.Event, *codex.Checkpoint) error
	Checkpoint(context.Context, string) (*codex.Checkpoint, error)
	Current(context.Context, string) (model.Record, error)
	SaveDirectory(context.Context, string) (storage.Directory, error)
	Directories(context.Context) ([]storage.Directory, error)
	RemoveDirectory(context.Context, string) error
}

func hello(persistent bool) map[string]any {
	capabilities := []string{"demo", "runs.list", "runs.get", "runs.events", "codex.import", "codex.update", "codex.update.start", "codex.update.status", "codex.update.cancel", "codex.scan.start", "codex.scan.status", "codex.scan.cancel", "codex.scan.candidates", "codex.scan.import"}
	if persistent {
		capabilities = append(capabilities, "persistent-library", "runs.eventContent", "codex.directories.list", "codex.directories.remove", "records.export", "records.import")
	}
	return map[string]any{"engineVersion": "0.1.0-dev.0", "protocolVersion": Version, "recordVersion": 1, "capabilities": capabilities}
}

func newPersistentService(ctx context.Context, demo model.Record, store libraryStore) (*scanService, error) {
	if store == nil {
		return nil, errors.New("persistent library requires a store")
	}
	runs, err := store.Runs(ctx)
	if err != nil {
		return nil, fmt.Errorf("restore library: %w", err)
	}
	files, events := 0, 0
	for _, run := range demo.Runs {
		events += run.EventCount
	}
	for _, run := range runs {
		files++
		events += run.EventCount
	}
	if files > model.MaxImportedSources || events > model.MaxCurrentEvents {
		return nil, fmt.Errorf("saved library exceeds %d files or %d events; database retained", model.MaxImportedSources, model.MaxCurrentEvents)
	}
	demo.Runs = append(append([]model.Run{}, demo.Runs...), runs...)
	return &scanService{record: demo, store: store}, nil
}

// publishRun is called under the service lock. Quotas are checked before any
// write; after a successful commit, publishing metadata cannot fail. Persisted
// events remain on disk instead of accumulating in the service's memory.
func (s *scanService) publishRun(ctx context.Context, run model.Run, events []model.Event, checkpoints ...*codex.Checkpoint) (bool, error) {
	index, err := replacementIndex(&s.record, run, len(events))
	if err != nil {
		return false, err
	}
	if s.store == nil {
		return replaceRun(&s.record, run, events)
	}
	var cp *codex.Checkpoint
	if len(checkpoints) > 0 {
		cp = checkpoints[0]
	}
	if err := s.store.SaveCheckpoint(ctx, run, events, cp); err != nil {
		return false, fmt.Errorf("保存记录失败: %w", err)
	}
	if index < 0 {
		s.record.Runs = append(s.record.Runs, run)
	} else {
		s.record.Runs[index] = run
	}
	return index >= 0, nil
}

func (s *scanService) importRun(req Request) Response {
	var path string
	var expectedID string
	if req.Method == "codex.update" {
		var params struct {
			RunID string `json:"runId"`
		}
		if json.Unmarshal(req.Params, &params) != nil || params.RunID == "" || len(params.RunID) > 128 {
			return failure(req.ID, "INVALID_PARAMS", "Expected a registered run ID (max 128 bytes)")
		}
		for _, run := range s.record.Runs {
			if run.ID == params.RunID && run.ReadOnly {
				return failure(req.ID, "IMPORT_FAILED", "交换记录只读，嵌入的来源路径未获本机授权；请显式选择来源文件后重新导入")
			}
			if run.ID == params.RunID && !run.Demo && run.Source == "Codex" && run.ImportInfo != nil {
				path = run.ImportInfo.File
				break
			}
		}
		if path == "" {
			return failure(req.ID, "NOT_FOUND", "Registered Codex run not found")
		}
		expectedID = params.RunID
	} else {
		var params struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(req.Params, &params) != nil || params.Path == "" {
			return failure(req.ID, "INVALID_PARAMS", "Expected a selected file path")
		}
		path = params.Path
	}
	var cp *codex.Checkpoint
	var saved *model.Record
	if expectedID != "" && s.store != nil {
		var err error
		cp, saved, err = s.updateBase(context.Background(), expectedID)
		if err != nil {
			return failure(req.ID, "IMPORT_FAILED", err.Error())
		}
	}
	run, events, next, report, err := codex.ReadUpdate(context.Background(), path, cp, saved, nil)
	if err != nil {
		return failure(req.ID, "IMPORT_FAILED", err.Error())
	}
	if expectedID != "" && run.ID != expectedID {
		return failure(req.ID, "IMPORT_FAILED", "来源路径身份发生变化，请重新选择文件")
	}
	if _, err := replacementIndex(&s.record, run, len(events)); err != nil {
		return failure(req.ID, "IMPORT_LIMIT", err.Error())
	}
	replaced, err := s.publishRun(context.Background(), run, events, next)
	if err != nil {
		return failure(req.ID, "IMPORT_FAILED", err.Error())
	}
	return Response{Type: "response", Version: Version, ID: req.ID, Result: map[string]any{"run": run, "replaced": replaced, "update": report}}
}

func (s *scanService) persistedEvents(req Request) Response {
	q := query{}
	if len(req.Params) != 0 && (string(req.Params) == "null" || json.Unmarshal(req.Params, &q) != nil) {
		return failure(req.ID, "INVALID_PARAMS", "Expected query object")
	}
	limit := 50
	if q.Limit != nil {
		limit = *q.Limit
	}
	if q.Offset < 0 || q.Offset > 1000000000 || limit < 1 || limit > 100 || len(q.Search) > 1000 {
		return failure(req.ID, "INVALID_PARAMS", "offset must be 0..1000000000, limit 1..100, search at most 1000 bytes")
	}
	switch q.Kind {
	case "", "all", "message", "tool_call", "tool_result", "verification", "lifecycle", "error":
	default:
		return failure(req.ID, "INVALID_PARAMS", "Unsupported event kind")
	}
	var run *model.Run
	for i := range s.record.Runs {
		if s.record.Runs[i].ID == q.RunID {
			run = &s.record.Runs[i]
			break
		}
	}
	if run == nil {
		return failure(req.ID, "NOT_FOUND", "Run not found")
	}
	if run.Demo {
		return dispatchRecord(req, &s.record)
	}
	items, total, err := s.store.Events(context.Background(), run.ID, q.Search, q.Kind, q.Offset, limit)
	if err != nil {
		return failure(req.ID, "STORAGE_FAILED", err.Error())
	}
	// The SQL page is bounded first; apply the same encoded-frame budget used
	// for memory pages, then advance by the actual number returned.
	page := paginate(items, 0, limit)
	page.Total, page.NextOffset = total, nil
	next := q.Offset + len(page.Items)
	if next < total {
		page.NextOffset = &next
	}
	return Response{Type: "response", Version: Version, ID: req.ID, Result: page}
}

func (s *scanService) directories(req Request) Response {
	if s.store == nil {
		return failure(req.ID, "METHOD_NOT_FOUND", "Unknown method")
	}
	if req.Method == "codex.directories.list" {
		items, err := s.store.Directories(context.Background())
		if err != nil {
			return failure(req.ID, "STORAGE_FAILED", err.Error())
		}
		return Response{Type: "response", Version: Version, ID: req.ID, Result: map[string]any{"items": items}}
	}
	var params struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(req.Params, &params) != nil || params.ID == "" || len(params.ID) > 128 {
		return failure(req.ID, "INVALID_PARAMS", "Expected a saved directory ID (max 128 bytes)")
	}
	if err := s.store.RemoveDirectory(context.Background(), params.ID); err != nil {
		return failure(req.ID, "STORAGE_FAILED", err.Error())
	}
	return Response{Type: "response", Version: Version, ID: req.ID, Result: map[string]bool{"ok": true}}
}
