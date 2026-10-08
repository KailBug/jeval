package storage

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"jeval/engine/internal/exchange"
	"jeval/engine/internal/model"
)

func TestComparisonMetricsAndBoundEvidence(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, filepath.Join(t.TempDir(), "comparison.sqlite"))
	left, le, _ := sample(t)
	right, re, _ := sample(t)
	lDuration, rDuration, lTokens, rTokens := int64(0), int64(200), int64(10), int64(25)
	left.DurationMs = &lDuration
	right.DurationMs = &rDuration
	left.Tokens = &lTokens
	right.Tokens = &rTokens
	if err := s.Save(ctx, left, le); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, right, re); err != nil {
		t.Fatal(err)
	}
	lRef, rRef := model.SnapshotRef{RunID: left.ID, SnapshotID: left.ImportInfo.SnapshotID}, model.SnapshotRef{RunID: right.ID, SnapshotID: right.ImportInfo.SnapshotID}
	if _, err := s.SaveAnnotation(ctx, left.ID, lRef.SnapshotID, nil, "rejected", "removed note", 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAnnotation(ctx, left.ID, lRef.SnapshotID, nil, "uncertain", "", 1, true); err != nil {
		t.Fatal(err)
	}
	report, err := s.Comparison(ctx, lRef, rRef)
	if err != nil {
		t.Fatal(err)
	}
	if *report.Metrics["durationMs"].Delta != 200 || *report.Metrics["tokens"].Delta != 15 || len(report.Left.Annotations) != 0 {
		t.Fatal(report.Metrics)
	}
	for _, format := range []string{"json", "markdown"} {
		if _, err = exchange.EncodeComparison(report, format); err != nil {
			t.Fatal(err)
		}
	}
	report.Right.Record.Runs[0].Tokens = nil
	data, err := exchange.EncodeComparison(report, "json")
	if err != nil {
		t.Fatal(err)
	}
	var decoded model.Comparison
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	metric := decoded.Metrics["tokens"]
	if metric.Left == nil || *metric.Left != 10 || metric.Right != nil || metric.Delta != nil {
		t.Fatal("export invented a missing token metric", metric)
	}
	if m := decoded.Metrics["durationMs"]; m.Left == nil || *m.Left != 0 || *m.Delta != 200 {
		t.Fatal("export lost a known zero duration", m)
	}
	if _, err = s.Comparison(ctx, lRef, model.SnapshotRef{RunID: left.ID, SnapshotID: rRef.SnapshotID}); err == nil {
		t.Fatal("accepted mismatched snapshot")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.Comparison(cancelled, lRef, rRef); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
