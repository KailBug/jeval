package exchange

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"jeval/engine/internal/model"
)

func EncodeComparison(report model.Comparison, format string) ([]byte, error) {
	if report.Format != "jeval-comparison" || report.FormatVersion != 1 || report.ContentScope != "normalized-preview" || report.SourceFilesIncluded || report.FullContentIncluded {
		return nil, errors.New("invalid comparison report scope")
	}
	if _, err := time.Parse(time.RFC3339Nano, report.GeneratedAt); err != nil {
		return nil, err
	}
	for _, side := range []model.ComparisonSide{report.Left, report.Right} {
		if err := Validate(side.Record); err != nil {
			return nil, err
		}
		run := side.Record.Runs[0]
		ids := map[string]bool{}
		for _, e := range side.Record.Events {
			ids[e.ID] = true
		}
		targets := map[string]bool{}
		for _, a := range side.Annotations {
			key := ""
			if a.EventID != nil {
				key = *a.EventID
				if !ids[key] {
					return nil, errors.New("annotation event outside exported snapshot")
				}
			}
			if a.RunID != run.ID || a.SnapshotID != run.ImportInfo.SnapshotID || a.Deleted || a.Revision < 1 || len(a.Note) > model.MaxAnnotationNoteBytes || !utf8.ValidString(a.Note) || strings.ContainsRune(a.Note, 0) || targets[key] || a.Event != nil || (a.Judgement != "accepted" && a.Judgement != "rejected" && a.Judgement != "uncertain") {
				return nil, errors.New("invalid exported human annotation")
			}
			targets[key] = true
		}
	}
	// Recompute from validated saved values rather than trusting supplied deltas.
	l, r := report.Left.Record.Runs[0], report.Right.Record.Runs[0]
	lc, rc := int64(l.EventCount), int64(r.EventCount)
	report.Metrics = map[string]model.MetricDifference{"durationMs": model.Difference(l.DurationMs, r.DurationMs), "tokens": model.Difference(l.Tokens, r.Tokens), "eventCount": model.Difference(&lc, &rc)}
	var data []byte
	var err error
	switch format {
	case "json":
		data, err = json.MarshalIndent(report, "", "  ")
		data = append(data, '\n')
	case "markdown":
		var out strings.Builder
		out.WriteString("# jeval manual comparison\n\nFormat: jeval-comparison v1. Content: all normalized preview events from two explicitly selected snapshots and saved human annotations at export time. Original files, full text bodies, unsaved drafts and model diagnoses are not included. No automatic step alignment or causal performance conclusion is implied. Deltas are right minus left; missing metrics remain unknown.\n\n")
		out.WriteString(block("Generated at: " + report.GeneratedAt))
		out.WriteString("## Metric differences\n\n")
		for _, key := range []string{"durationMs", "tokens", "eventCount"} {
			m := report.Metrics[key]
			out.WriteString(block(fmt.Sprintf("%s: left=%s; right=%s; delta=%s", key, known(m.Left), known(m.Right), known(m.Delta))))
		}
		for i, side := range []model.ComparisonSide{report.Left, report.Right} {
			label := "Left"
			if i == 1 {
				label = "Right"
			}
			out.WriteString("## " + label + " snapshot\n\n")
			out.Write(markdown(side.Record))
			out.WriteString("## " + label + " human annotations\n\n")
			if len(side.Annotations) == 0 {
				out.WriteString("No saved human annotations.\n\n")
			}
			for _, a := range side.Annotations {
				target := "snapshot"
				if a.EventID != nil {
					target = *a.EventID
				}
				out.WriteString(block(fmt.Sprintf("Run: %s\nSnapshot: %s\nTarget: %s\nHuman judgement: %s\nRevision: %d\nUpdated at: %s\nNote:\n%s", a.RunID, a.SnapshotID, target, a.Judgement, a.Revision, a.UpdatedAt, a.Note)))
			}
		}
		data = []byte(out.String())
	default:
		return nil, errors.New("unsupported comparison format")
	}
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, errors.New("comparison report exceeds 64 MiB")
	}
	return data, nil
}
