// Package protocol implements bounded, versioned JSON-lines requests over stdio.
package protocol

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"jeval/engine/internal/adapters/codex"
	"jeval/engine/internal/model"
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
	Search string `json:"search"`
	Status string `json:"status"`
	Source string `json:"source"`
	RunID  string `json:"runId"`
	Offset int    `json:"offset"`
	Limit  *int   `json:"limit"`
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
		res.Result = map[string]any{"engineVersion": "0.1.0-dev.0", "protocolVersion": Version, "recordVersion": 1, "capabilities": []string{"demo", "runs.list", "runs.events", "codex.import"}}
		return res
	case "codex.import":
		var params struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(req.Params, &params) != nil || params.Path == "" {
			return failure(req.ID, "INVALID_PARAMS", "Expected a selected file path")
		}
		run, events, err := codex.Read(params.Path)
		if err != nil {
			return failure(req.ID, "IMPORT_FAILED", err.Error())
		}
		index, imported, eventCount := -1, 0, len(events)
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
		if imported >= 20 || eventCount > 50000 {
			return failure(req.ID, "IMPORT_LIMIT", "本次启动最多导入 20 个文件、50000 个事件，请重启后重新选择")
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
		res.Result = map[string]any{"run": run, "replaced": index >= 0}
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
		for _, event := range record.Events {
			if event.RunID == q.RunID {
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
		res := dispatchRecord(req, &record)
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
