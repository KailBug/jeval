package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"jeval/engine/internal/model"
	"jeval/engine/internal/storage"
)

type faultyLibrary struct {
	libraryStore
	saveErr, directoryErr, eventsErr, runsErr error
	onSave                                    func()
}

func (f *faultyLibrary) Save(ctx context.Context, run model.Run, events []model.Event) error {
	if f.onSave != nil {
		f.onSave()
	}
	if f.saveErr != nil {
		return f.saveErr
	}
	return f.libraryStore.Save(ctx, run, events)
}
func (f *faultyLibrary) SaveDirectory(ctx context.Context, path string) (storage.Directory, error) {
	if f.directoryErr != nil {
		return storage.Directory{}, f.directoryErr
	}
	return f.libraryStore.SaveDirectory(ctx, path)
}
func (f *faultyLibrary) Runs(ctx context.Context) ([]model.Run, error) {
	if f.runsErr != nil {
		return nil, f.runsErr
	}
	return f.libraryStore.Runs(ctx)
}
func (f *faultyLibrary) Events(ctx context.Context, sourceID, search, kind string, offset, limit int) ([]model.Event, int, error) {
	if f.eventsErr != nil {
		return nil, 0, f.eventsErr
	}
	return f.libraryStore.Events(ctx, sourceID, search, kind, offset, limit)
}

func openLibrary(t *testing.T, path string) *storage.Store {
	t.Helper()
	s, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func persistentService(t *testing.T, store libraryStore) *scanService {
	t.Helper()
	s, err := newPersistentService(context.Background(), sample(t), store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)
	return s
}

func importFile(t *testing.T, s *scanService, path string) model.Run {
	t.Helper()
	params, _ := json.Marshal(map[string]string{"path": path})
	res := s.dispatch(request("codex.import", string(params)))
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	return res.Result.(map[string]any)["run"].(model.Run)
}

func TestPersistentLibraryRestoresMetadataAndBrowsesOffline(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "source.jsonl")
	database := filepath.Join(dir, "library.sqlite")
	writeScanFile(t, path, scanFixture+`{"type":"response_item","payload":{"type":"message","role":"assistant","content":"offline SEARCH marker"}}`+"\n")
	store := openLibrary(t, database)
	s := persistentService(t, store)
	run := importFile(t, s, path)
	if len(s.record.Events) != len(sample(t).Events) {
		t.Fatal("persisted events retained in service memory")
	}
	want, err := store.Current(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	store = openLibrary(t, database)
	s = persistentService(t, store)
	if s.scan != nil || len(s.candidates) != 0 {
		t.Fatal("restart started discovery")
	}
	res := s.dispatch(request("runs.get", `{"runId":"`+run.ID+`"}`))
	if res.Error != nil || !reflect.DeepEqual(res.Result, run) {
		t.Fatalf("run metadata lost: %+v", res)
	}
	first := s.dispatch(request("runs.events", `{"runId":"`+run.ID+`","limit":1}`)).Result.(Page[model.Event])
	if first.Total != 2 || first.NextOffset == nil || *first.NextOffset != 1 || !reflect.DeepEqual(first.Items, want.Events[:1]) {
		t.Fatal(first)
	}
	page := s.dispatch(request("runs.events", `{"runId":"`+run.ID+`","search":"  SeArCh  ","kind":"message","limit":1}`)).Result.(Page[model.Event])
	if page.Total != 1 || len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0], want.Events[1]) {
		t.Fatal(page)
	}
	res = s.dispatch(request("codex.update", `{"runId":"`+run.ID+`"}`))
	if res.Error == nil || res.Error.Code != "IMPORT_FAILED" {
		t.Fatal(res)
	}
	if got := s.dispatch(request("runs.get", `{"runId":"`+run.ID+`"}`)).Result; !reflect.DeepEqual(got, run) {
		t.Fatal("offline update damaged metadata")
	}
	if s.dispatch(request("runs.get", `{"runId":"missing"}`)).Error.Code != "NOT_FOUND" {
		t.Fatal("missing run accepted")
	}
}

func TestPersistentFailedSaveKeepsPreviousSnapshotAndScanCandidate(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "source.jsonl")
	writeScanFile(t, path, scanFixture)
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	fault := &faultyLibrary{libraryStore: store}
	s := persistentService(t, fault)
	run := importFile(t, s, path)
	want, err := store.Current(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, path, scanFixture+`{"type":"event_msg","payload":{"type":"task_complete"}}`+"\n")
	fault.saveErr = errors.New("injected disk write failure")
	for _, req := range []Request{
		request("codex.update", `{"runId":"`+run.ID+`"}`),
		request("codex.import", fmt.Sprintf(`{"path":%q}`, path)),
	} {
		res := s.dispatch(req)
		if res.Error == nil || res.Error.Code != "IMPORT_FAILED" || !strings.Contains(res.Error.Message, "injected") {
			t.Fatal(res)
		}
	}
	status := finishScan(t, s, startScan(t, s, root).ID)
	if !candidates(t, s, status.ID)[0].Existing {
		t.Fatal("registered record missing existing mark")
	}
	status = selectScan(t, s, status.ID, run.ID)
	if status.Failed != 1 || status.Updated != 0 || status.Imported != 0 {
		t.Fatal(status)
	}
	choice := candidates(t, s, status.ID)[0]
	if choice.Imported || !strings.Contains(choice.Error, "injected") {
		t.Fatal(choice)
	}
	got, err := store.Current(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("failed disk save changed current pointer")
	}
	if got := s.dispatch(request("runs.get", `{"runId":"`+run.ID+`"}`)).Result; !reflect.DeepEqual(got, run) {
		t.Fatal("failed disk save published metadata")
	}
	fault.saveErr = nil
	status = selectScan(t, s, status.ID, run.ID)
	if status.Updated != 1 || status.Failed != 0 {
		t.Fatal(status)
	}
	if got := s.dispatch(request("runs.get", `{"runId":"`+run.ID+`"}`)).Result.(model.Run); got.EventCount != 2 {
		t.Fatal(got)
	}
	old, err := store.Snapshot(ctx, run.ImportInfo.SnapshotID)
	if err != nil || !reflect.DeepEqual(old, want) {
		t.Fatal("successful update overwrote old evidence")
	}
}

func TestPersistentDirectoryConfigDoesNotImportAndSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.jsonl")
	writeScanFile(t, path, scanFixture)
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	s := persistentService(t, store)
	status := finishScan(t, s, startScan(t, s, root).ID)
	if status.Ready != 1 || len(s.record.Runs) != 3 {
		t.Fatal("discovery imported before confirmation")
	}
	choices := candidates(t, s, status.ID)
	selectScan(t, s, status.ID, choices[0].ID)
	items := s.dispatch(request("codex.directories.list", `{}`)).Result.(map[string]any)["items"].([]storage.Directory)
	if len(items) != 1 {
		t.Fatal(items)
	}
	s.close()
	s = persistentService(t, store)
	if s.scan != nil {
		t.Fatal("saved directory automatically scanned")
	}
	for _, tc := range []struct{ params, code string }{
		{`{"directoryId":"missing"}`, "NOT_FOUND"},
		{fmt.Sprintf(`{"path":%q,"directoryId":%q}`, root, items[0].ID), "INVALID_PARAMS"},
	} {
		res := s.dispatch(request("codex.scan.start", tc.params))
		if res.Error == nil || res.Error.Code != tc.code {
			t.Fatal(res)
		}
	}
	res := s.dispatch(request("codex.scan.start", `{"directoryId":"`+items[0].ID+`"}`))
	if res.Error != nil {
		t.Fatal(res)
	}
	status = finishScan(t, s, res.Result.(ScanStatus).ID)
	if status.Ready != 1 || !candidates(t, s, status.ID)[0].Existing {
		t.Fatal("restored record not recognized during saved-root scan")
	}
	res = s.dispatch(request("codex.directories.remove", `{"id":"`+items[0].ID+`"}`))
	if res.Error != nil {
		t.Fatal(res)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("config removal touched source file")
	}
	if len(s.record.Runs) != 4 {
		t.Fatal("config removal removed imported snapshot")
	}
	if items := s.dispatch(request("codex.directories.list", `{}`)).Result.(map[string]any)["items"].([]storage.Directory); len(items) != 0 {
		t.Fatal(items)
	}
}

func TestPersistentFaultsAreVisibleAndDirectorySavePrecedesScan(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, filepath.Join(root, "a.jsonl"), scanFixture)
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	fault := &faultyLibrary{libraryStore: store, directoryErr: errors.New("injected config write failure")}
	s := persistentService(t, fault)
	res := s.dispatch(request("codex.scan.start", fmt.Sprintf(`{"path":%q}`, root)))
	if res.Error == nil || res.Error.Code != "STORAGE_FAILED" || s.scan != nil {
		t.Fatal("scan started despite config write failure")
	}
	if items, err := store.Directories(context.Background()); err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
	fault.directoryErr = nil
	run := importFile(t, s, filepath.Join(root, "a.jsonl"))
	fault.eventsErr = errors.New("injected query failure")
	res = s.dispatch(request("runs.events", `{"runId":"`+run.ID+`"}`))
	if res.Error == nil || res.Error.Code != "STORAGE_FAILED" {
		t.Fatal(res)
	}
	fault.runsErr = errors.New("injected restore failure")
	if _, err := newPersistentService(context.Background(), sample(t), fault); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatal("restore silently fell back to memory")
	}
}

func TestPersistentQuotaRemainsAcrossRestart(t *testing.T) {
	root := t.TempDir()
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	s := persistentService(t, store)
	for i := 0; i < 20; i++ {
		path := filepath.Join(root, fmt.Sprintf("%02d.jsonl", i))
		writeScanFile(t, path, scanFixture)
		importFile(t, s, path)
	}
	s.close()
	s = persistentService(t, store)
	path := filepath.Join(root, "new.jsonl")
	writeScanFile(t, path, scanFixture)
	res := s.dispatch(request("codex.import", fmt.Sprintf(`{"path":%q}`, path)))
	if res.Error == nil || res.Error.Code != "IMPORT_LIMIT" || len(s.record.Runs) != 23 {
		t.Fatal(res)
	}
	// Updating an existing source consumes no additional file slot.
	run := importFile(t, s, filepath.Join(root, "00.jsonl"))
	if run.EventCount != 1 || len(s.record.Runs) != 23 {
		t.Fatal(run)
	}
}

func TestPersistentEventPagesRespectEncodedFrameBudget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "escaped.jsonl")
	var fixture strings.Builder
	fixture.WriteString(scanFixture)
	for i := 0; i < 70; i++ {
		line, err := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "assistant", "content": strings.Repeat("\\", 8192)}})
		if err != nil {
			t.Fatal(err)
		}
		fixture.Write(line)
		fixture.WriteByte('\n')
	}
	writeScanFile(t, path, fixture.String())
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	s := persistentService(t, store)
	run := importFile(t, s, path)
	offset, count := 0, 0
	for {
		res := s.dispatch(request("runs.events", fmt.Sprintf(`{"runId":%q,"offset":%d,"limit":100}`, run.ID, offset)))
		if res.Error != nil {
			t.Fatal(res.Error)
		}
		encoded, err := json.Marshal(res)
		if err != nil || len(encoded) >= MaxFrameBytes {
			t.Fatal("event page exceeded protocol frame budget")
		}
		page := res.Result.(Page[model.Event])
		if page.Total != run.EventCount || len(page.Items) == 0 {
			t.Fatal(page)
		}
		for _, event := range page.Items {
			count++
			if event.Sequence != count {
				t.Fatal("paged event skipped or repeated")
			}
		}
		if page.NextOffset == nil {
			break
		}
		if *page.NextOffset != offset+len(page.Items) {
			t.Fatal("page advanced by requested limit instead of returned count")
		}
		offset = *page.NextOffset
	}
	if count != run.EventCount || offset == 0 {
		t.Fatal("large escaped events did not use multiple complete pages")
	}
}

func TestPersistentSaveAndCancellationSerialize(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, filepath.Join(root, "a.jsonl"), scanFixture)
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	entered, release := make(chan struct{}), make(chan struct{})
	fault := &faultyLibrary{libraryStore: store, onSave: func() { close(entered); <-release }}
	s := persistentService(t, fault)
	status := finishScan(t, s, startScan(t, s, root).ID)
	ids := []string{candidates(t, s, status.ID)[0].ID}
	params, _ := json.Marshal(map[string]any{"id": status.ID, "ids": ids})
	if res := s.dispatch(request("codex.scan.import", string(params))); res.Error != nil {
		t.Fatal(res)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("save not entered")
	}
	ack := make(chan Response, 1)
	go func() { ack <- s.dispatch(request("codex.scan.cancel", `{"id":"`+status.ID+`"}`)) }()
	select {
	case <-ack:
		t.Fatal("cancellation acknowledged while commit still active")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	res := <-ack
	if res.Error != nil {
		t.Fatal(res)
	}
	status = finishScan(t, s, status.ID)
	if status.Imported != 1 {
		t.Fatal("completed save lost during cancellation")
	}
	current, err := store.Current(context.Background(), ids[0])
	if err != nil || current.Runs[0].ID != ids[0] {
		t.Fatal("published snapshot was not saved")
	}
}

func TestServeWithStoreAndMemoryCapabilities(t *testing.T) {
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	input := `{"type":"request","version":1,"id":"hello","method":"hello"}` + "\n" + `{"type":"request","version":1,"id":"get","method":"runs.get","params":{"runId":"demo-cache"}}` + "\n"
	var output bytes.Buffer
	if err := ServeWithStore(strings.NewReader(input), &output, sample(t), store); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "persistent-library") || !strings.Contains(output.String(), `"id":"demo-cache"`) {
		t.Fatal(output.String())
	}
	output.Reset()
	if err := Serve(strings.NewReader(input), &output, sample(t)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "persistent-library") || !strings.Contains(output.String(), `"id":"demo-cache"`) {
		t.Fatal(output.String())
	}
	s := &scanService{record: sample(t)}
	if res := s.dispatch(request("codex.directories.list", `{}`)); res.Error == nil || res.Error.Code != "METHOD_NOT_FOUND" {
		t.Fatal(res)
	}
}
