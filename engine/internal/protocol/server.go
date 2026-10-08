// Package protocol implements bounded, versioned JSON-lines requests over stdio.
package protocol

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"jeval/engine/internal/model"
	"jeval/engine/internal/storage"
	"strings"
)

const Version = 1
const MaxFrameBytes = 1024 * 1024

type Request struct {
	Type    string          `json:"type"`
	Version int             `json:"version"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Response struct {
	Type    string    `json:"type"`
	Version int       `json:"version"`
	ID      string    `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}
type query struct {
	Search     string `json:"search"`
	Status     string `json:"status"`
	Source     string `json:"source"`
	RunID      string `json:"runId"`
	Kind       string `json:"kind"`
	Offset     int    `json:"offset"`
	Limit      *int   `json:"limit"`
	SnapshotID string `json:"snapshotId"`
}
type Page[T any] struct {
	Items      []T  `json:"items"`
	Total      int  `json:"total"`
	NextOffset *int `json:"nextOffset"`
}

func paginate[T any](items []T, offset, limit int) Page[T] {
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	// Escaped tool output and long paths can be much larger on the wire. Return
	// fewer items when needed; nextOffset, not requested limit, drives pagination.
	bytes := 0
	for i := offset; i < end; i++ {
		encoded, _ := json.Marshal(items[i])
		bytes += len(encoded) + 1
		if bytes > MaxFrameBytes/2 && i > offset {
			end = i
			break
		}
	}
	var next *int
	if end < total {
		next = &end
	}
	return Page[T]{Items: items[offset:end], Total: total, NextOffset: next}
}

func failure(id, code, message string) Response {
	return Response{Type: "response", Version: Version, ID: id, Error: &RPCError{Code: code, Message: message}}
}

func dispatch(req Request, record model.Record) Response {
	return dispatchRecord(req, &record)
}

func dispatchRecord(req Request, record *model.Record) Response {
	if req.ID == "" || len(req.ID) > 128 || req.Type != "request" {
		return failure(req.ID, "INVALID_REQUEST", "Expected a request with a non-empty ID (max 128 characters)")
	}
	if req.Version != Version {
		return failure(req.ID, "PROTOCOL_MISMATCH", "Expected protocol version 1")
	}
	res := Response{Type: "response", Version: Version, ID: req.ID}
	switch req.Method {
	case "hello":
		res.Result = hello(false)
		return res
	case "runs.get":
		var params struct {
			RunID string `json:"runId"`
		}
		if json.Unmarshal(req.Params, &params) != nil || params.RunID == "" || len(params.RunID) > 128 {
			return failure(req.ID, "INVALID_PARAMS", "Expected a run ID (max 128 bytes)")
		}
		for _, run := range record.Runs {
			if run.ID == params.RunID {
				res.Result = run
				return res
			}
		}
		return failure(req.ID, "NOT_FOUND", "Run not found")
	case "codex.import", "codex.update":
		service := &scanService{record: *record}
		res := service.importRun(req)
		*record = service.record
		return res
	case "shutdown":
		res.Result = map[string]bool{"ok": true}
		return res
	case "runs.list", "runs.events":
	default:
		return failure(req.ID, "METHOD_NOT_FOUND", "Unknown method")
	}
	q := query{}
	if len(req.Params) != 0 {
		if string(req.Params) == "null" || json.Unmarshal(req.Params, &q) != nil {
			return failure(req.ID, "INVALID_PARAMS", "Expected query object")
		}
	}
	limit := 50
	if q.Limit != nil {
		limit = *q.Limit
	}
	if q.Offset < 0 || q.Offset > 1000000000 || limit < 1 || limit > 100 || len(q.Search) > 1000 {
		return failure(req.ID, "INVALID_PARAMS", "offset must be 0..1000000000, limit 1..100, search at most 1000 bytes")
	}
	if req.Method == "runs.list" {
		if q.Source != "" && q.Source != "all" && q.Source != "demo" && q.Source != "codex" {
			return failure(req.ID, "INVALID_PARAMS", "Unsupported source")
		}
		if q.Status != "" && q.Status != "all" && q.Status != "completed" && q.Status != "failed" && q.Status != "unknown" {
			return failure(req.ID, "INVALID_PARAMS", "Unsupported status")
		}
		items := make([]model.Run, 0)
		search := strings.ToLower(strings.TrimSpace(q.Search))
		for _, run := range record.Runs {
			if q.Source == "demo" && !run.Demo || q.Source == "codex" && run.Source != "Codex" {
				continue
			}
			if q.Status != "" && q.Status != "all" && q.Status != run.Status {
				continue
			}
			if !strings.Contains(strings.ToLower(run.Title+" "+run.Project+" "+run.Source), search) {
				continue
			}
			items = append(items, run)
		}
		res.Result = paginate(items, q.Offset, limit)
	} else {
		switch q.Kind {
		case "", "all", "message", "tool_call", "tool_result", "verification", "lifecycle", "error":
		default:
			return failure(req.ID, "INVALID_PARAMS", "Unsupported event kind")
		}
		found := false
		for _, run := range record.Runs {
			if run.ID == q.RunID {
				found = true
				break
			}
		}
		if !found {
			return failure(req.ID, "NOT_FOUND", "Run not found")
		}
		items := make([]model.Event, 0)
		search := strings.ToLower(strings.TrimSpace(q.Search))
		for _, event := range record.Events {
			if event.RunID == q.RunID && (q.Kind == "" || q.Kind == "all" || q.Kind == event.Kind) && strings.Contains(strings.ToLower(event.Title+" "+event.Content+" "+event.Role), search) {
				items = append(items, event)
			}
		}
		res.Result = paginate(items, q.Offset, limit)
	}
	return res
}

// Serve keeps stdout exclusively for protocol frames. An oversized frame ends the
// connection; malformed JSON returns an error and leaves subsequent requests usable.
func Serve(input io.Reader, output io.Writer, record model.Record) error {
	service := &scanService{record: record}
	return serve(input, output, service)
}

// ServeWithStore restores current run metadata only. Event pages come from the
// database; opening a library never reads or scans the original source files.
// The caller owns the store and must close it after ServeWithStore returns.
func ServeWithStore(input io.Reader, output io.Writer, demo model.Record, store *storage.Store) error {
	if store == nil {
		return fmt.Errorf("persistent library requires a store")
	}
	service, err := newPersistentService(context.Background(), demo, store)
	if err != nil {
		return err
	}
	return serve(input, output, service)
}

func serve(input io.Reader, output io.Writer, service *scanService) error {
	defer service.close()
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), MaxFrameBytes)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			if err := encoder.Encode(failure("", "PARSE_ERROR", "Invalid JSON request")); err != nil {
				return err
			}
			continue
		}
		res := service.dispatch(req)
		if err := encoder.Encode(res); err != nil {
			return err
		}
		if req.Method == "shutdown" && res.Error == nil {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read protocol frame: %w", err)
	}
	return nil
}

func replaceRun(record *model.Record, run model.Run, events []model.Event) (bool, error) {
	index, err := replacementIndex(record, run, len(events))
	if err != nil {
		return false, err
	}
	eventCount := len(events)
	for _, old := range record.Runs {
		if old.ID != run.ID {
			eventCount += old.EventCount
		}
	}
	retained := make([]model.Event, 0, eventCount)
	for _, event := range record.Events {
		if event.RunID != run.ID {
			retained = append(retained, event)
		}
	}
	record.Events = append(retained, events...)
	if index >= 0 {
		record.Runs[index] = run
	} else {
		record.Runs = append(record.Runs, run)
	}
	return index >= 0, nil
}

func replacementIndex(record *model.Record, run model.Run, events int) (int, error) {
	index, imported, eventCount := -1, 0, events
	for i, old := range record.Runs {
		if old.ID == run.ID {
			index = i
		} else {
			eventCount += old.EventCount
			if !old.Demo {
				imported++
			}
		}
	}
	if imported >= model.MaxImportedSources || eventCount > model.MaxCurrentEvents {
		return -1, fmt.Errorf("任务库最多导入 %d 个文件、%d 个事件，请减少导入范围", model.MaxImportedSources, model.MaxCurrentEvents)
	}
	return index, nil
}
