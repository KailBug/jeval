package protocol

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"

	"jeval/engine/internal/exchange"
	"jeval/engine/internal/model"
)

type comparisonStore interface {
	Comparison(context.Context, model.SnapshotRef, model.SnapshotRef) (model.Comparison, error)
	CheckExportPath(context.Context, string) error
}

func (s *scanService) exportComparison(req Request) Response {
	store, ok := s.store.(comparisonStore)
	if !ok {
		return failure(req.ID, "METHOD_NOT_FOUND", "Comparison requires persistent storage")
	}
	var p struct {
		Left   model.SnapshotRef `json:"left"`
		Right  model.SnapshotRef `json:"right"`
		Path   string            `json:"path"`
		Format string            `json:"format"`
	}
	d := json.NewDecoder(bytes.NewReader(req.Params))
	d.DisallowUnknownFields()
	valid := func(ref model.SnapshotRef) bool {
		return ref.RunID != "" && len(ref.RunID) <= 128 && ref.SnapshotID != "" && len(ref.SnapshotID) <= 128
	}
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || !valid(p.Left) || !valid(p.Right) || !filepath.IsAbs(p.Path) || len(p.Path) > 2048 || (p.Format != "json" && p.Format != "markdown") {
		return failure(req.ID, "INVALID_PARAMS", "Expected two saved snapshot references, output path and format")
	}
	ctx := context.Background()
	if err := store.CheckExportPath(ctx, p.Path); err != nil {
		return failure(req.ID, "EXPORT_FAILED", err.Error())
	}
	report, err := store.Comparison(ctx, p.Left, p.Right)
	if errors.Is(err, sql.ErrNoRows) {
		return failure(req.ID, "NOT_FOUND", "Comparison snapshot not found")
	}
	if err != nil {
		return failure(req.ID, "EXPORT_FAILED", err.Error())
	}
	data, err := exchange.EncodeComparison(report, p.Format)
	if err == nil {
		err = exchange.Write(p.Path, data)
	}
	if err != nil {
		return failure(req.ID, "EXPORT_FAILED", err.Error())
	}
	return Response{Type: "response", Version: Version, ID: req.ID, Result: map[string]any{"path": p.Path, "format": p.Format, "left": p.Left, "right": p.Right, "eventCounts": []int{report.Left.Record.Runs[0].EventCount, report.Right.Record.Runs[0].EventCount}}}
}
