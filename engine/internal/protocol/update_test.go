package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"jeval/engine/internal/adapters/codex"
)

type pausedCheckpointLibrary struct {
	libraryStore
	entered chan struct{}
}

func (s *pausedCheckpointLibrary) Checkpoint(ctx context.Context, id string) (*codex.Checkpoint, error) {
	close(s.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}

func startUpdate(t *testing.T, s *scanService, id string) UpdateStatus {
	t.Helper()
	params, _ := json.Marshal(map[string]string{"runId": id})
	result := s.dispatch(request("codex.update.start", string(params)))
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	return result.Result.(UpdateStatus)
}

func updateRequest(t *testing.T, s *scanService, method, id string) UpdateStatus {
	t.Helper()
	result := s.dispatch(request(method, `{"id":"`+id+`"}`))
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	return result.Result.(UpdateStatus)
}

func finishUpdate(t *testing.T, s *scanService, id string) UpdateStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := updateRequest(t, s, "codex.update.status", id)
		if status.State != "running" && status.State != "cancelling" {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("update timed out")
	return UpdateStatus{}
}

func TestUpdateCancellationKeepsOldDataAndDoesNotCancelViaOldScan(t *testing.T) {
	ctx := context.Background()
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	path := filepath.Join(t.TempDir(), "source.jsonl")
	writeScanFile(t, path, scanFixture)
	s := persistentService(t, store)
	run := importFile(t, s, path)
	old, err := store.Current(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := store.Checkpoint(ctx, run.ID)
	if err != nil || cp == nil {
		t.Fatal(cp, err)
	}
	// Pause outside the publication lock. Browsing and cancelling must still work.
	paused := &pausedCheckpointLibrary{libraryStore: store, entered: make(chan struct{})}
	s.store = paused
	s.scan = &ScanStatus{ID: "old-scan", State: "completed", Phase: "discovery", Issues: []ScanIssue{}}
	status := startUpdate(t, s, run.ID)
	select {
	case <-paused.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("not reading checkpoint")
	}
	if got := s.dispatch(request("runs.get", `{"runId":"`+run.ID+`"}`)); got.Error != nil || !reflect.DeepEqual(got.Result, run) {
		t.Fatal("browsing blocked", got)
	}
	if got := s.dispatch(request("codex.update.start", `{"runId":"`+run.ID+`"}`)); got.Error == nil || got.Error.Code != "SCAN_BUSY" {
		t.Fatal(got)
	}
	for _, method := range []string{"codex.import", "codex.scan.start", "records.import"} {
		if got := s.dispatch(request(method, `{}`)); got.Error == nil || got.Error.Code != "SCAN_BUSY" {
			t.Fatal(method, got)
		}
	}
	if got := s.dispatch(request("codex.scan.cancel", `{"id":"old-scan"}`)); got.Error != nil {
		t.Fatal(got)
	}
	if got := updateRequest(t, s, "codex.update.status", status.ID); got.State != "running" {
		t.Fatal("old scan cancelled update", got)
	}
	if got := updateRequest(t, s, "codex.update.cancel", status.ID); got.State != "cancelling" {
		t.Fatal(got)
	}
	if got := finishUpdate(t, s, status.ID); got.State != "cancelled" {
		t.Fatal(got)
	}
	if got := updateRequest(t, s, "codex.update.cancel", status.ID); got.State != "cancelled" {
		t.Fatal(got)
	}
	got, err := store.Current(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(got, old) {
		t.Fatal("cancel changed record", err)
	}
	gotCP, err := store.Checkpoint(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(gotCP, cp) {
		t.Fatal("cancel changed checkpoint", err)
	}
}

func TestUpdatePublishesAtomicallyAndRecoversFailures(t *testing.T) {
	ctx := context.Background()
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	fault := &faultyLibrary{libraryStore: store}
	s := persistentService(t, fault)
	path := filepath.Join(t.TempDir(), "source.jsonl")
	writeScanFile(t, path, scanFixture)
	run := importFile(t, s, path)
	old, err := store.Current(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := store.Checkpoint(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, path, scanFixture+`{"type":"event_msg","payload":{"type":"task_complete"}}`+"\n")
	fault.saveErr = errors.New("disk unavailable")
	status := finishUpdate(t, s, startUpdate(t, s, run.ID).ID)
	if status.State != "failed" || status.Run != nil {
		t.Fatal(status)
	}
	got, err := store.Current(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(got, old) {
		t.Fatal("failure changed record", err)
	}
	gotCP, err := store.Checkpoint(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(gotCP, cp) {
		t.Fatal("failure changed checkpoint", err)
	}
	fault.saveErr = nil
	status = finishUpdate(t, s, startUpdate(t, s, run.ID).ID)
	if status.State != "completed" || status.Report.Mode != "incremental" || status.Run.EventCount != run.EventCount+1 {
		t.Fatal(status)
	}
	last := status.ID
	status = finishUpdate(t, s, startUpdate(t, s, run.ID).ID)
	if status.Report.Mode != "unchanged" || status.Report.ParsedLines != 0 {
		t.Fatal(status)
	}
	if result := s.dispatch(request("codex.update.status", `{"id":"`+last+`"}`)); result.Error == nil || result.Error.Code != "NOT_FOUND" {
		t.Fatal(result)
	}
	// Detached records cannot use their embedded source path for updates.
	s.record.Runs[len(s.record.Runs)-1].ReadOnly = true
	result := s.dispatch(request("codex.update.start", `{"runId":"`+run.ID+`"}`))
	if result.Error == nil || result.Error.Code != "IMPORT_FAILED" {
		t.Fatal(result)
	}
	for _, method := range []string{"codex.update.start", "codex.update.status", "codex.update.cancel"} {
		if res := s.dispatch(request(method, `{}`)); res.Error == nil || res.Error.Code != "INVALID_PARAMS" {
			t.Fatal(res)
		}
	}
}

func TestUpdateShutdownCancelsAndRestartForgetsJob(t *testing.T) {
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	s := persistentService(t, store)
	path := filepath.Join(t.TempDir(), "source.jsonl")
	writeScanFile(t, path, scanFixture)
	run := importFile(t, s, path)
	paused := &pausedCheckpointLibrary{libraryStore: store, entered: make(chan struct{})}
	s.store = paused
	status := startUpdate(t, s, run.ID)
	<-paused.entered
	if res := s.dispatch(request("shutdown", `{}`)); res.Error != nil {
		t.Fatal(res)
	}
	s.close()
	if got := updateRequest(t, s, "codex.update.status", status.ID); got.State != "cancelled" {
		t.Fatal(got)
	}
	restarted := persistentService(t, store)
	if res := restarted.dispatch(request("codex.update.status", `{"id":"`+status.ID+`"}`)); res.Error == nil || res.Error.Code != "NOT_FOUND" {
		t.Fatal(res)
	}
	if got := finishUpdate(t, restarted, startUpdate(t, restarted, run.ID).ID); got.State != "completed" || got.Report.Mode != "unchanged" {
		t.Fatal(got)
	}
}
