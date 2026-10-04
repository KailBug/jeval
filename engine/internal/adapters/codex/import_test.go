package codex

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

const meta = `{"type":"session_meta","payload":{"id":"test-session","cli_version":"synthetic"}}` + "\n"

func input(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "中文 sample.jsonl")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClassicMappingAndReadOnly(t *testing.T) {
	data, err := os.ReadFile("../../../../fixtures/adapters/codex/classic.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := input(t, data)
	run, events, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if run.Demo || run.Source != "Codex" || run.Status != "unknown" || run.Tokens != nil || run.DurationMs != nil {
		t.Fatalf("invented metadata: %+v", run)
	}
	if run.Title != "检查示例项目的测试结果" || len(events) != 5 || run.EventCount != 5 {
		t.Fatalf("wrong mapping: %+v, %d", run, len(events))
	}
	if events[0].Evidence.Line != 3 || events[2].ParentID == nil || *events[2].ParentID != events[1].ID {
		t.Fatalf("wrong evidence or tool relation: %+v", events)
	}
	if run.ImportInfo.WarningCount != 2 || run.ImportInfo.ForkedFromID != "synthetic-parent-001" || len(run.ImportInfo.SHA256) != 64 {
		t.Fatalf("wrong provenance: %+v", run.ImportInfo)
	}
	before := run.ImportInfo.SHA256
	if err := os.WriteFile(path, append(data, []byte("\n"+`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}}`)...), 0600); err != nil {
		t.Fatal(err)
	}
	updated, _, err := Read(path)
	if err != nil || updated.ID != run.ID || updated.EventCount != 6 || updated.ImportInfo.SHA256 == before {
		t.Fatalf("append identity: %+v %v", updated, err)
	}
	// Read itself does not change even malformed input bytes.
	copyPath := input(t, data)
	if _, _, err := Read(copyPath); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(copyPath)
	if !bytes.Equal(after, data) {
		t.Fatal("source was changed")
	}
}

func TestBoundedPreviewsPartialLinesAndUnknowns(t *testing.T) {
	line, _ := json.Marshal(map[string]any{"timestamp": "bad-date", "type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "call_id": "c", "input": strings.Repeat("中文", 10000)}})
	data := append([]byte("\xef\xbb\xbf"+meta), line...)
	data = append(data, []byte("\n"+`{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"missing","output":{"text":"unknown structure"}}}`+"\n"+`{"unfinished":`)...)
	run, events, err := Read(input(t, data))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Timestamp != nil || !utf8.ValidString(events[0].Content) || len(events[0].Content) > MaxTextBytes+150 || !strings.Contains(events[0].Content, "已截断") {
		t.Fatalf("unbounded preview: %+v", events[0])
	}
	if events[1].ParentID != nil || run.ImportInfo.WarningCount != 4 {
		t.Fatalf("missing warnings: %+v", run.ImportInfo)
	}
}

func TestRejectInvalidOrUnsupportedInputs(t *testing.T) {
	for name, data := range map[string][]byte{
		"not codex":       []byte(`{"type":"response_item","payload":{"type":"message"}}`),
		"duplicate meta":  []byte(meta + meta),
		"unknown history": []byte(`{"type":"session_meta","payload":{"id":"s","history_mode":"future"}}`),
		"invalid utf8":    append([]byte(meta), 0xff),
		"too many lines":  []byte(meta + strings.Repeat("\n", 50000)),
		"too large":       bytes.Repeat([]byte("x"), MaxFileBytes+1),
		"duplicate calls": []byte(meta + strings.Repeat(`{"type":"response_item","payload":{"type":"function_call","call_id":"c","name":"test"}}`+"\n", 2)),
		"too many events": []byte(meta + strings.Repeat(`{"type":"response_item","payload":{"type":"message","role":"user","content":"x"}}`+"\n", MaxEvents+1)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Read(input(t, data)); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	for _, path := range []string{"relative.jsonl", filepath.Join(t.TempDir(), "missing.jsonl"), t.TempDir()} {
		if _, _, err := Read(path); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
}

func TestWarningsAreBoundedAndEmptySessionIsExplicit(t *testing.T) {
	run, events, err := Read(input(t, []byte(meta+strings.Repeat("bad\n", 100))))
	if err != nil || len(events) != 0 || run.ImportInfo.WarningCount != 101 || len(run.ImportInfo.Warnings) != 30 {
		t.Fatalf("warning bounds: %+v %v", run, err)
	}
}

func TestPaginatedMessagesDeduplicateWithinTurnAndPreserveFallbacks(t *testing.T) {
	data, err := os.ReadFile("../../../../fixtures/adapters/codex/paginated.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	run, events, err := Read(input(t, data))
	if err != nil {
		t.Fatal(err)
	}
	if run.ImportInfo.HistoryMode != "paginated" || run.ImportInfo.ParentThreadID != "synthetic-parent-thread" || run.ImportInfo.ForkedFromID != "" {
		t.Fatalf("incorrect provenance: %+v", run.ImportInfo)
	}
	if len(events) != 9 || run.ImportInfo.WarningCount != 0 {
		t.Fatalf("mapping: %d events, %+v", len(events), run.ImportInfo)
	}
	wantLines := []int{2, 3, 6, 7, 8, 14, 15, 16, 17}
	for i, event := range events {
		if event.Evidence.Line != wantLines[i] {
			t.Fatalf("event %d has line %d", i, event.Evidence.Line)
		}
	}
	if events[7].Content != events[1].Content {
		t.Fatal("repeated prompt in another turn lost")
	}
	if events[8].Content != "这是第二个回合的独立消息。" || events[4].ParentID == nil || *events[4].ParentID != events[3].ID {
		t.Fatal("fallback text or tool relation lost")
	}
	if run.Status != "unknown" {
		t.Fatal("turn completion misclassified as session success")
	}
}
