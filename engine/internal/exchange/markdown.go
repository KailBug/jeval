package exchange

import (
	"fmt"
	"strings"

	"jeval/engine/internal/model"
)

// Every value from a record goes inside a text fence longer than every backtick
// run it contains. HTML, links and injected headings stay ordinary report text.
func block(value string) string {
	longest, current := 0, 0
	for _, char := range value {
		if char == '`' {
			current++
			if current > longest {
				longest = current
			}
		} else {
			current = 0
		}
	}
	length := max(3, longest+1)
	fence := strings.Repeat("`", length)
	return fence + "text\n" + value + "\n" + fence + "\n\n"
}

func known[T any](value *T) string {
	if value == nil {
		return "unknown (null)"
	}
	return fmt.Sprint(*value)
}

func markdown(record model.Record) []byte {
	run := record.Runs[0]
	info := run.ImportInfo
	var out strings.Builder
	out.WriteString("# jeval normalized record preview\n\n")
	out.WriteString("Format: jeval-record v1; record schema: 1.\n\nContent scope: normalized-preview, at most 8192 UTF-8 body bytes per event, excluding the adapter's fixed truncation notice. Original source files are not included. Previews may be truncated; the source SHA-256 describes the original source bytes, not a backup in this report. This report contains no model diagnosis.\n\n")
	out.WriteString("## Run and original source\n\n")
	out.WriteString(block(fmt.Sprintf("Title: %s\nRun/source ID: %s\nProject: %s\nSource: %s\nStatus: %s\nStarted at: %s\nDuration (ms): %s\nTokens: %s\nEvents: %d\nOriginal locator: %s\nSource SHA-256: %s\nSnapshot: %s\nAdapter: %s\nSession ID: %s\nCLI version: %s\nHistory mode: %s\nParent thread: %s\nForked from: %s\nImport warnings: %d (showing %d)", run.Title, run.ID, run.Project, run.Source, run.Status, known(run.StartedAt), known(run.DurationMs), known(run.Tokens), run.EventCount, info.File, info.SHA256, info.SnapshotID, info.AdapterVersion, info.SessionID, emptyUnknown(info.CLIVersion), info.HistoryMode, emptyUnknown(info.ParentThreadID), emptyUnknown(info.ForkedFromID), info.WarningCount, len(info.Warnings))))
	for _, warning := range info.Warnings {
		out.WriteString(block(fmt.Sprintf("Source line %d: %s", warning.Line, warning.Message)))
	}
	out.WriteString("## Events\n\n")
	for _, event := range record.Events {
		fmt.Fprintf(&out, "### Event %d\n\n", event.Sequence)
		out.WriteString(block(fmt.Sprintf("ID: %s\nKind: %s\nRole: %s\nTitle: %s\nTimestamp: %s\nParent event: %s\nEvidence source: %s\nOriginal locator: %s\nPhysical line: %d\nSnapshot: %s", event.ID, event.Kind, event.Role, event.Title, known(event.Timestamp), known(event.ParentID), event.Evidence.SourceID, event.Evidence.Location, event.Evidence.Line, event.Evidence.SnapshotID)))
		out.WriteString(block(event.Content))
	}
	return []byte(out.String())
}

func emptyUnknown(value string) string {
	if value == "" {
		return "unknown (not provided)"
	}
	return value
}
