package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"jeval/engine/internal/adapters/codex"
	"jeval/engine/internal/exchange"
	"jeval/engine/internal/model"
)

func exportRequest(runID, path, format string) Request {
	params, _ := json.Marshal(map[string]string{"runId": runID, "path": path, "format": format})
	return request("records.export", string(params))
}

func nativeRequest(path string) Request {
	params, _ := json.Marshal(map[string]string{"path": path})
	return request("records.import", string(params))
}

func saveBundle(t *testing.T, run model.Run, events []model.Event) string {
	t.Helper()
	data, err := exchange.Encode(model.Record{SchemaVersion: 1, Runs: []model.Run{run}, Events: events}, "json")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "record.jeval.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func nativeRun(t *testing.T, s *scanService, path string) model.Run {
	t.Helper()
	res := s.dispatch(nativeRequest(path))
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	return res.Result.(map[string]any)["run"].(model.Run)
}

func TestRecordExchangeOfflineFullRoundtripAndDetachedRestart(t *testing.T) {
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source.jsonl")
	var fixture strings.Builder
	fixture.WriteString(scanFixture)
	for n := range 121 {
		fixture.WriteString(fmt.Sprintf(`{"type":"response_item","payload":{"type":"message","role":"assistant","content":"full snapshot message %d"}}`, n))
		fixture.WriteByte('\n')
	}
	writeScanFile(t, source, fixture.String())
	store := openLibrary(t, filepath.Join(t.TempDir(), "original.sqlite"))
	s := persistentService(t, store)
	run := importFile(t, s, source)
	want, err := store.Current(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if page := s.dispatch(request("runs.events", `{"runId":"`+run.ID+`","search":"message 120","limit":1}`)).Result.(Page[model.Event]); page.Total != 1 {
		t.Fatal(page)
	}
	output := filepath.Join(t.TempDir(), "all-events.jeval.json")
	res := s.dispatch(exportRequest(run.ID, output, "json"))
	if res.Error != nil {
		t.Fatalf("export failed: %s: %s", res.Error.Code, res.Error.Message)
	}
	if res.Result.(map[string]any)["eventCount"] != 122 {
		t.Fatal(res.Result)
	}
	before, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), "readOnly") {
		t.Fatal("local permission leaked into exchange")
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if res := s.dispatch(exportRequest(run.ID, output, "json")); res.Error != nil {
		t.Fatal("offline export failed", res)
	}
	after, _ := os.ReadFile(output)
	if string(before) != string(after) {
		t.Fatal("offline export bytes changed")
	}
	markdown := filepath.Join(t.TempDir(), "report.md")
	if res := s.dispatch(exportRequest(run.ID, markdown, "markdown")); res.Error != nil {
		t.Fatal(res)
	}
	data, _ := os.ReadFile(markdown)
	if !strings.Contains(string(data), "full snapshot message 120") {
		t.Fatal("Markdown omitted events outside current UI page")
	}

	database := filepath.Join(t.TempDir(), "imported.sqlite")
	otherStore := openLibrary(t, database)
	other := persistentService(t, otherStore)
	imported := nativeRun(t, other, output)
	if !imported.ReadOnly || imported.ID != run.ID || imported.ImportInfo.SnapshotID != run.ImportInfo.SnapshotID {
		t.Fatal(imported)
	}
	got, err := otherStore.Current(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("native roundtrip changed canonical record or evidence", err)
	}
	if res := other.dispatch(nativeRequest(output)); res.Error != nil || !res.Result.(map[string]any)["replaced"].(bool) || !res.Result.(map[string]any)["run"].(model.Run).ReadOnly {
		t.Fatal("repeated import changed detached permission", res)
	}
	other.close()
	if err := otherStore.Close(); err != nil {
		t.Fatal(err)
	}
	otherStore = openLibrary(t, database)
	other = persistentService(t, otherStore)
	if got := other.dispatch(request("runs.get", `{"runId":"`+run.ID+`"}`)).Result.(model.Run); !got.ReadOnly {
		t.Fatal("restart authorized embedded source", got)
	}
	if res := other.dispatch(request("codex.update", `{"runId":"`+run.ID+`"}`)); res.Error == nil || !strings.Contains(res.Error.Message, "只读") {
		t.Fatal("detached update attempted embedded path read", res)
	}
	page := other.dispatch(request("runs.events", `{"runId":"`+run.ID+`","offset":120,"limit":50}`)).Result.(Page[model.Event])
	if page.Total != 122 || !reflect.DeepEqual(page.Items, want.Events[120:]) {
		t.Fatal("detached offline events or evidence changed", page)
	}
	copy := filepath.Join(t.TempDir(), "detached-copy.json")
	if res := other.dispatch(exportRequest(run.ID, copy, "json")); res.Error != nil {
		t.Fatal(res)
	}
	data, _ = os.ReadFile(copy)
	if string(data) != string(before) {
		t.Fatal("detached re-export changed canonical exchange")
	}
}

func TestNativeImportPreservesBindingAndRejectsCurrentSnapshotCollision(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.jsonl")
	writeScanFile(t, source, scanFixture)
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	s := persistentService(t, store)
	run := importFile(t, s, source)
	old, err := store.Current(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	bundle := saveBundle(t, run, old.Events)
	if got := nativeRun(t, s, bundle); got.ReadOnly {
		t.Fatal("reimport removed existing local source permission")
	}
	writeScanFile(t, source, scanFixture+`{"type":"event_msg","payload":{"type":"task_complete"}}`+"\n")
	updated := importFile(t, s, source)
	res := s.dispatch(nativeRequest(bundle))
	if res.Error == nil || res.Error.Code != "RECORD_CONFLICT" {
		t.Fatal("old bundle rolled current snapshot back", res)
	}
	if got := s.dispatch(request("runs.get", `{"runId":"`+run.ID+`"}`)).Result.(model.Run); !reflect.DeepEqual(got, updated) {
		t.Fatal("collision changed published metadata")
	}
	// A detached snapshot can bind again only via an explicit local selection.
	newStore := openLibrary(t, filepath.Join(t.TempDir(), "detached.sqlite"))
	other := persistentService(t, newStore)
	if !nativeRun(t, other, bundle).ReadOnly {
		t.Fatal("native import authorized local path")
	}
	if rebound := importFile(t, other, source); rebound.ReadOnly || rebound.EventCount != 2 {
		t.Fatal("explicit import did not rebind", rebound)
	}
	if res := other.dispatch(request("codex.update", `{"runId":"`+run.ID+`"}`)); res.Error != nil {
		t.Fatal("rebound source update failed", res)
	}
}

func TestNativeImportFailureAndQuotasPublishNothing(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.jsonl")
	writeScanFile(t, source, scanFixture)
	run, events, err := codex.Read(source)
	if err != nil {
		t.Fatal(err)
	}
	bundle := saveBundle(t, run, events)
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	fault := &faultyLibrary{libraryStore: store, saveErr: errors.New("injected native save failure")}
	s := persistentService(t, fault)
	res := s.dispatch(nativeRequest(bundle))
	if res.Error == nil || res.Error.Code != "IMPORT_FAILED" || len(s.record.Runs) != 3 {
		t.Fatal("failed save published detached metadata", res)
	}
	if runs, err := store.Runs(context.Background()); err != nil || len(runs) != 0 {
		t.Fatal("failed save persisted source", runs, err)
	}
	fault.saveErr = nil
	for n := range model.MaxImportedSources {
		path := filepath.Join(t.TempDir(), fmt.Sprintf("source-%d.jsonl", n))
		writeScanFile(t, path, scanFixture)
		importFile(t, s, path)
	}
	res = s.dispatch(nativeRequest(bundle))
	if res.Error == nil || res.Error.Code != "IMPORT_LIMIT" || len(s.record.Runs) != model.MaxImportedSources+3 {
		t.Fatal("native import bypassed library quota", res)
	}
	if runs, err := store.Runs(context.Background()); err != nil || len(runs) != model.MaxImportedSources {
		t.Fatal("overlimit import changed library", runs, err)
	}
	invalid := filepath.Join(t.TempDir(), "invalid.json")
	writeScanFile(t, invalid, `{"format":"unsupported"}`)
	if res := s.dispatch(nativeRequest(invalid)); res.Error == nil || res.Error.Code != "IMPORT_FAILED" {
		t.Fatal("invalid exchange accepted", res)
	}
}

func TestExportProtectsSourceDatabaseAndPreservesTargetsOnFailure(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.jsonl")
	writeScanFile(t, source, scanFixture)
	database := filepath.Join(t.TempDir(), "library.sqlite")
	store := openLibrary(t, database)
	s := persistentService(t, store)
	run := importFile(t, s, source)
	for _, path := range []string{source, database, database + "-wal", database + "-shm"} {
		before, readErr := os.ReadFile(path)
		res := s.dispatch(exportRequest(run.ID, path, "json"))
		if res.Error == nil || res.Error.Code != "EXPORT_FAILED" {
			t.Fatal("protected destination accepted", res)
		}
		after, nextErr := os.ReadFile(path)
		if (readErr == nil) != (nextErr == nil) || string(before) != string(after) {
			t.Fatal("failed protected export changed bytes", path)
		}
	}
	output := filepath.Join(t.TempDir(), "existing.json")
	writeScanFile(t, output, "previous bytes")
	if res := s.dispatch(exportRequest(run.ID, output, "invalid-format")); res.Error == nil || res.Error.Code != "INVALID_PARAMS" {
		t.Fatal(res)
	}
	if data, _ := os.ReadFile(output); string(data) != "previous bytes" {
		t.Fatal("invalid export changed old target")
	}
	dir := t.TempDir()
	if res := s.dispatch(exportRequest(run.ID, dir, "json")); res.Error == nil || res.Error.Code != "EXPORT_FAILED" {
		t.Fatal("directory export target accepted", res)
	}
	if got, err := store.Current(context.Background(), run.ID); err != nil || got.Runs[0].ImportInfo.SnapshotID != run.ImportInfo.SnapshotID {
		t.Fatal("export failure changed saved snapshot", err)
	}
}

func TestExchangeParametersAndMemoryModeAreExplicit(t *testing.T) {
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	s := persistentService(t, store)
	for _, req := range []Request{
		request("records.export", `null`), request("records.export", `{"runId":"a","path":"relative","format":"json"}`),
		request("records.export", `{"runId":"a","path":"relative","format":"markdown"}`),
		request("records.import", `null`), request("records.import", `{"path":"relative"}`),
	} {
		if res := s.dispatch(req); res.Error == nil || res.Error.Code != "INVALID_PARAMS" {
			t.Fatal(res)
		}
	}
	if res := s.dispatch(exportRequest("demo-cache", filepath.Join(t.TempDir(), "demo.json"), "json")); res.Error == nil || res.Error.Code != "NOT_FOUND" {
		t.Fatal("demo exchange unexpectedly supported", res)
	}
	memory := &scanService{record: sample(t)}
	for _, req := range []Request{nativeRequest(filepath.Join(t.TempDir(), "record.json")), exportRequest("demo-cache", filepath.Join(t.TempDir(), "demo.json"), "json")} {
		if res := memory.dispatch(req); res.Error == nil || res.Error.Code != "METHOD_NOT_FOUND" {
			t.Fatal("memory exchange advertised as persistent", res)
		}
	}
}

func TestNativeEmbeddedLocatorIsEvidenceOnlyDuringUpdateAndExport(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.jsonl")
	writeScanFile(t, source, scanFixture)
	run, events, err := codex.Read(source)
	if err != nil {
		t.Fatal(err)
	}
	// This locator cannot be passed to filesystem calls. Native import and
	// export carry it as text, without trying to authorize or resolve it.
	run.ImportInfo.File = filepath.Join(t.TempDir(), "embedded\x00-path.jsonl")
	for n := range events {
		events[n].Evidence.Location = run.ImportInfo.File
		events[n].FullContent = nil // The exchange intentionally carries only previews.
	}
	bundle := saveBundle(t, run, events)
	store := openLibrary(t, filepath.Join(t.TempDir(), "library.sqlite"))
	s := persistentService(t, store)
	if got := nativeRun(t, s, bundle); !got.ReadOnly {
		t.Fatal("native import granted source permission")
	}
	if res := s.dispatch(request("codex.update", `{"runId":"`+run.ID+`"}`)); res.Error == nil || !strings.Contains(res.Error.Message, "只读") {
		t.Fatal("update attempted to access embedded locator", res)
	}
	output := filepath.Join(t.TempDir(), "export.json")
	writeScanFile(t, output, "previous bytes")
	if res := s.dispatch(exportRequest(run.ID, output, "json")); res.Error != nil {
		t.Fatalf("embedded locator was resolved during export: %s", res.Error.Message)
	}
	got, err := exchange.Read(output)
	if err != nil || !reflect.DeepEqual(got.Events, events) || got.Runs[0].ImportInfo.File != run.ImportInfo.File {
		t.Fatal("export changed evidence locator", err)
	}
}
