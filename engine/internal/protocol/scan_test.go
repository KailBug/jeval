package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"jeval/engine/internal/model"
)

const scanFixture = `{"type":"session_meta","payload":{"id":"synthetic-scan"}}` + "\n" + `{"type":"response_item","payload":{"type":"message","role":"user","content":"synthetic scan"}}` + "\n"

func writeScanFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func startScan(t *testing.T, s *scanService, root string) ScanStatus {
	t.Helper()
	params, _ := json.Marshal(map[string]string{"path": root})
	res := s.dispatch(request("codex.scan.start", string(params)))
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	return res.Result.(ScanStatus)
}

func finishScan(t *testing.T, s *scanService, id string) ScanStatus {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(10 * time.Second):
		t.Fatal("scan did not finish")
	}
	res := s.dispatch(request("codex.scan.status", `{"id":"`+id+`"}`))
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	return res.Result.(ScanStatus)
}

func candidates(t *testing.T, s *scanService, id string) []ScanCandidate {
	t.Helper()
	res := s.dispatch(request("codex.scan.candidates", `{"id":"`+id+`","limit":100}`))
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	return res.Result.(Page[ScanCandidate]).Items
}

func selectScan(t *testing.T, s *scanService, id string, ids ...string) ScanStatus {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"id": id, "ids": ids})
	res := s.dispatch(request("codex.scan.import", string(params)))
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	return finishScan(t, s, id)
}

func TestScanRecursiveFailuresReplacementAndEvidence(t *testing.T) {
	t.Run("temporary path", func(t *testing.T) {
		testScanRecursiveFailuresReplacementAndEvidence(t, t.TempDir())
	})
	if runtime.GOOS == "windows" {
		t.Run("alternate path casing", func(t *testing.T) {
			testScanRecursiveFailuresReplacementAndEvidence(t, strings.ToUpper(t.TempDir()))
		})
	}
}

func testScanRecursiveFailuresReplacementAndEvidence(t *testing.T, root string) {
	t.Helper()
	nested := filepath.Join(root, "中文 空格")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "session.JSONL")
	writeScanFile(t, path, scanFixture)
	// Imports store resolved paths; TEMP may contain a Windows 8.3 alias or
	// different casing, and other platforms may use a symlinked temp directory.
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, filepath.Join(root, "broken.jsonl"), "not a rollout")
	writeScanFile(t, filepath.Join(root, "ignored.txt"), scanFixture)
	s := &scanService{record: sample(t)}
	defer s.close()
	first := startScan(t, s, root)
	status := finishScan(t, s, first.ID)
	if status.State != "completed" || status.Imported != 0 || status.Ready != 1 || status.Failed != 1 || status.Discovered != 2 || len(status.Issues) != 1 {
		t.Fatalf("unexpected scan: %+v", status)
	}
	page := s.dispatch(request("runs.list", `{"source":"codex"}`)).Result.(Page[model.Run])
	if page.Total != 0 {
		t.Fatal("discovery imported without a selection")
	}
	choices := candidates(t, s, first.ID)
	if choices[0].Title != "synthetic scan" || choices[0].Preview != "synthetic scan" {
		t.Fatal(choices)
	}
	selectScan(t, s, first.ID, choices[0].ID)
	page = s.dispatch(request("runs.list", `{"source":"codex"}`)).Result.(Page[model.Run])
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("expected one imported run: %+v", page)
	}
	run := page.Items[0]
	if run.ImportInfo == nil {
		t.Fatal("imported run is missing source information")
	}
	if run.ImportInfo.File != resolvedPath {
		t.Fatalf("imported path=%q, want resolved path=%q (input=%q)", run.ImportInfo.File, resolvedPath, path)
	}
	events := s.dispatch(request("runs.events", `{"runId":"`+run.ID+`"}`)).Result.(Page[model.Event])
	if events.Items[0].Evidence.Line != 2 {
		t.Fatal("lost physical evidence line")
	}
	data, _ := os.ReadFile(path)
	if string(data) != scanFixture {
		t.Fatal("source changed")
	}
	writeScanFile(t, path, scanFixture+`{"type":"event_msg","payload":{"type":"task_complete"}}`+"\n")
	next := startScan(t, s, root)
	status = finishScan(t, s, next.ID)
	if status.Updated != 0 {
		t.Fatal("discovery updated a record")
	}
	if !candidates(t, s, next.ID)[0].Existing {
		t.Fatal("existing run not marked")
	}
	status = selectScan(t, s, next.ID, choices[0].ID)
	if status.Imported != 0 || status.Updated != 1 {
		t.Fatalf("not replaced: %+v", status)
	}
	page = s.dispatch(request("runs.list", `{"source":"codex"}`)).Result.(Page[model.Run])
	if page.Total != 1 || page.Items[0].EventCount != 2 {
		t.Fatal("replacement duplicated or lost events")
	}
	if s.dispatch(request("codex.scan.status", `{"id":"`+first.ID+`"}`)).Error.Code != "NOT_FOUND" {
		t.Fatal("stale scan retained")
	}
	writeScanFile(t, path, "broken replacement")
	status = finishScan(t, s, startScan(t, s, root).ID)
	if status.Failed != 2 || status.Updated != 0 {
		t.Fatal(status)
	}
	page = s.dispatch(request("runs.list", `{"source":"codex"}`)).Result.(Page[model.Run])
	if page.Items[0].EventCount != 2 {
		t.Fatal("failed read damaged previous snapshot")
	}
}

func TestScanCancelBusyQueriesAndShutdown(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, filepath.Join(root, "a.jsonl"), scanFixture)
	// Hold the worker before it starts: cancellation must not depend on disk speed.
	ctx, cancel := context.WithCancel(context.Background())
	s := &scanService{record: sample(t), scan: &ScanStatus{ID: "held", Root: root, State: "running", Issues: []ScanIssue{}}, cancel: cancel, done: make(chan struct{})}
	for _, method := range []string{"codex.scan.start", "codex.import"} {
		if res := s.dispatch(request(method, `{}`)); res.Error == nil || res.Error.Code != "SCAN_BUSY" {
			t.Fatal(res)
		}
	}
	if res := s.dispatch(request("runs.list", `{}`)); res.Error != nil || res.Result.(Page[model.Run]).Total != 3 {
		t.Fatal(res)
	}
	res := s.dispatch(request("codex.scan.cancel", `{"id":"held"}`))
	if res.Result.(ScanStatus).State != "cancelling" {
		t.Fatal(res)
	}
	go s.run(ctx, root, s.done)
	defer s.close()
	status := finishScan(t, s, "held")
	if status.State != "cancelled" || status.Imported != 0 || status.Failed != 0 {
		t.Fatal(status)
	}
	if res := s.dispatch(request("codex.scan.cancel", `{"id":"held"}`)); res.Result.(ScanStatus).State != "cancelled" {
		t.Fatal("cancel not idempotent")
	}
	startScan(t, s, root)
	s.dispatch(request("shutdown", `{}`))
	s.close()
	if s.active() {
		t.Fatal("worker survived shutdown")
	}
}

func TestScanBoundsAndEmptyDirectory(t *testing.T) {
	s := &scanService{record: sample(t)}
	defer s.close()
	root := t.TempDir()
	status := finishScan(t, s, startScan(t, s, root).ID)
	if status.State != "completed" || status.Discovered != 0 {
		t.Fatal(status)
	}
	for i := 0; i < maxScanFiles+1; i++ {
		writeScanFile(t, filepath.Join(root, fmt.Sprintf("%03d.jsonl", i)), "invalid")
	}
	status = finishScan(t, s, startScan(t, s, root).ID)
	if status.State != "limited" || status.Discovered != maxScanFiles || status.Failed != maxScanFiles || len(status.Issues) != 30 {
		t.Fatal(status)
	}
	for i := 0; i < model.MaxImportedSources-20; i++ {
		s.record.Runs = append(s.record.Runs, model.Run{ID: fmt.Sprintf("saved-%d", i), Source: "Codex"})
	}
	root = t.TempDir()
	for i := 0; i < 21; i++ {
		writeScanFile(t, filepath.Join(root, fmt.Sprintf("%03d.jsonl", i)), scanFixture)
	}
	status = finishScan(t, s, startScan(t, s, root).ID)
	if status.State != "completed" || status.Imported != 0 || status.Ready != 21 {
		t.Fatal(status)
	}
	choices := candidates(t, s, status.ID)
	ids := []string{}
	for _, choice := range choices {
		ids = append(ids, choice.ID)
	}
	params, _ := json.Marshal(map[string]any{"id": status.ID, "ids": ids})
	res := s.dispatch(request("codex.scan.import", string(params)))
	if res.Error == nil || res.Error.Code != "IMPORT_LIMIT" || !strings.Contains(res.Error.Message, fmt.Sprintf("%d 个文件", model.MaxImportedSources)) {
		t.Fatal(res)
	}
	if len(s.record.Runs) != model.MaxImportedSources-20+3 {
		t.Fatal("over-limit selection partially imported")
	}
	status = selectScan(t, s, status.ID, ids[:20]...)
	if status.Imported != 20 {
		t.Fatal(status)
	}
	root = t.TempDir()
	nested := root
	for i := 0; i <= maxScanDepth; i++ {
		nested = filepath.Join(nested, "d")
	}
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	status = finishScan(t, s, startScan(t, s, root).ID)
	if status.State != "limited" {
		t.Fatal(status)
	}
}

func TestScanSkipsSymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writeScanFile(t, filepath.Join(outside, "private.jsonl"), scanFixture)
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s := &scanService{record: sample(t)}
	defer s.close()
	status := finishScan(t, s, startScan(t, s, root).ID)
	if status.Skipped != 1 || status.Discovered != 0 {
		t.Fatal(status)
	}
}

func TestScanInvalidRequests(t *testing.T) {
	s := &scanService{record: sample(t)}
	for _, tc := range []struct{ method, params, code string }{
		{"codex.scan.start", `null`, "INVALID_PARAMS"},
		{"codex.scan.start", `{"path":"relative"}`, "INVALID_PARAMS"},
		{"codex.scan.status", `{}`, "INVALID_PARAMS"},
		{"codex.scan.cancel", `{"id":"missing"}`, "NOT_FOUND"},
	} {
		res := s.dispatch(request(tc.method, tc.params))
		if res.Error == nil || res.Error.Code != tc.code {
			t.Fatal(res)
		}
	}
}

func TestSelectionRejectsChangedFilesAndOnlyImportsChosen(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		writeScanFile(t, filepath.Join(root, name+".jsonl"), scanFixture)
	}
	s := &scanService{record: sample(t)}
	defer s.close()
	status := finishScan(t, s, startScan(t, s, root).ID)
	choices := candidates(t, s, status.ID)
	for _, ids := range [][]string{nil, {}, {choices[0].ID, choices[0].ID}, {"forged"}} {
		params, _ := json.Marshal(map[string]any{"id": status.ID, "ids": ids})
		res := s.dispatch(request("codex.scan.import", string(params)))
		if res.Error == nil || res.Error.Code != "INVALID_PARAMS" {
			t.Fatal(res)
		}
	}
	writeScanFile(t, choices[0].Path, scanFixture+`{"type":"event_msg","payload":{"type":"task_complete"}}`+"\n")
	status = selectScan(t, s, status.ID, choices[0].ID, choices[1].ID)
	if status.Imported != 1 || status.Failed != 1 || !strings.Contains(status.Issues[0].Message, "发生变化") {
		t.Fatal(status)
	}
	page := s.dispatch(request("runs.list", `{"source":"codex"}`)).Result.(Page[model.Run])
	if page.Total != 1 || page.Items[0].ID != choices[1].ID {
		t.Fatal("unselected/changed files imported")
	}
	params, _ := json.Marshal(map[string]any{"id": status.ID, "ids": []string{choices[1].ID}})
	if res := s.dispatch(request("codex.scan.import", string(params))); res.Error == nil {
		t.Fatal("consumed candidate accepted")
	}
	status = selectScan(t, s, status.ID, choices[2].ID)
	if status.Imported != 1 {
		t.Fatal(status)
	}
	// Re-scan, then delete a previously imported source: import failure retains it.
	status = finishScan(t, s, startScan(t, s, root).ID)
	if err := os.Remove(choices[1].Path); err != nil {
		t.Fatal(err)
	}
	status = selectScan(t, s, status.ID, choices[1].ID)
	if status.Failed != 1 || len(s.record.Runs) != 5 {
		t.Fatal("failed selection lost old data")
	}
}

func TestCandidatePagination(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 55; i++ {
		writeScanFile(t, filepath.Join(root, fmt.Sprintf("%03d.jsonl", i)), scanFixture)
	}
	s := &scanService{record: sample(t)}
	defer s.close()
	status := finishScan(t, s, startScan(t, s, root).ID)
	res := s.dispatch(request("codex.scan.candidates", `{"id":"`+status.ID+`"}`))
	page := res.Result.(Page[ScanCandidate])
	if page.Total != 55 || len(page.Items) != 50 || page.NextOffset == nil || *page.NextOffset != 50 {
		t.Fatal(page)
	}
	res = s.dispatch(request("codex.scan.candidates", `{"id":"`+status.ID+`","offset":50}`))
	if page = res.Result.(Page[ScanCandidate]); len(page.Items) != 5 || page.NextOffset != nil {
		t.Fatal(page)
	}
	if len(s.record.Runs) != 3 {
		t.Fatal("preview must not add runs")
	}
}
