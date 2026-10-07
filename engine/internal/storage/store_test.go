package storage

import (
	"context"
	"database/sql"
	"errors"
	"jeval/engine/internal/adapters/codex"
	"jeval/engine/internal/model"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sample(t *testing.T) (model.Run, []model.Event, string) {
	t.Helper()
	data, err := os.ReadFile("../../../fixtures/adapters/codex/classic.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "中文 source.jsonl")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	run, events, err := codex.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	// These existing fixtures exercise preview-only persistence and migration.
	for i := range events {
		events[i].FullContent = nil
	}
	return run, events, path
}

func openTest(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSnapshotsSurviveRestartAndSourceRemoval(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "中文 library.sqlite")
	s := openTest(t, path)
	run, events, source := sample(t)
	for range 2 {
		if err := s.Save(ctx, run, events); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM snapshots").Scan(&count); err != nil || count != 1 {
		t.Fatalf("dedup: %d %v", count, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	got, err := s.Current(ctx, run.ID)
	want := model.Record{SchemaVersion: 1, Runs: []model.Run{run}, Events: events}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("offline restore: %v", err)
	}
	got, err = s.Snapshot(ctx, run.ImportInfo.SnapshotID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot restore: %v", err)
	}
}

func TestAtomicPublishAndImmutableHistory(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, filepath.Join(t.TempDir(), "library.sqlite"))
	run, events, path := sample(t)
	if err := s.Save(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	data = append(data, []byte("\n"+`{"type":"event_msg","payload":{"type":"error","message":"new failure"}}`+"\n")...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	updated, next, err := codex.Read(path)
	for i := range next {
		next[i].FullContent = nil
	}
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != run.ID || updated.ImportInfo.SnapshotID == run.ImportInfo.SnapshotID {
		t.Fatal("replacement identity")
	}
	// Fail after the new snapshot INSERT, just before publishing the pointer.
	if _, err = s.db.Exec(`CREATE TRIGGER fail_publish BEFORE UPDATE ON sources BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, updated, next); err == nil {
		t.Fatal("expected publication failure")
	}
	got, err := s.Current(ctx, run.ID)
	if err != nil || got.Runs[0].ImportInfo.SnapshotID != run.ImportInfo.SnapshotID {
		t.Fatal("old snapshot lost", err)
	}
	if _, err = s.Snapshot(ctx, updated.ImportInfo.SnapshotID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("failed snapshot leaked", err)
	}
	for _, table := range []string{"snapshot_runs", "snapshot_events"} {
		var leaked int
		if err = s.db.QueryRow("SELECT count(*) FROM "+table+" WHERE snapshot_id=?", updated.ImportInfo.SnapshotID).Scan(&leaked); err != nil || leaked != 0 {
			t.Fatal("failed derived rows leaked", table, leaked, err)
		}
	}
	if _, err = s.db.Exec("DROP TRIGGER fail_publish"); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = s.Save(cancelled, updated, next); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation", err)
	}
	if err = s.Save(ctx, updated, next); err != nil {
		t.Fatal(err)
	}
	old, err := s.Snapshot(ctx, run.ImportInfo.SnapshotID)
	if err != nil || !reflect.DeepEqual(old.Events, events) {
		t.Fatal("history changed", err)
	}
	got, err = s.Current(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(got.Events, next) {
		t.Fatal("replacement missing", err)
	}
	next[0].Content = "unversioned normalization change"
	if err = s.Save(ctx, updated, next); err == nil {
		t.Fatal("immutable snapshot overwritten")
	}
}

func TestMigrationRollbackAndFutureSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.sqlite")
	s := openTest(t, path)
	run, events, _ := sample(t)
	if err := s.Save(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	steps := append(append([]string{}, migrations...), "CREATE TABLE should_rollback(id TEXT); INVALID SQL")
	if err := s.migrate(ctx, steps); err == nil {
		t.Fatal("expected migration failure")
	}
	var version, count int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatal("version changed", err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='should_rollback'").Scan(&count); err != nil || count != 0 {
		t.Fatal("DDL did not roll back", err)
	}
	if _, err := s.Current(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version=999"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if future, err := Open(ctx, path); err == nil {
		future.Close()
		t.Fatal("opened future schema")
	}
	// Rejection must preserve both the future version marker and saved records.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 999 {
		t.Fatal("future schema changed", err)
	}
	if err = db.QueryRow("SELECT count(*) FROM snapshots").Scan(&count); err != nil || count != 1 {
		t.Fatal("future data changed", err)
	}
}

func TestCorruptionAndInvalidEvidence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "broken.sqlite")
	bytes := []byte("not a database")
	if err := os.WriteFile(path, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(ctx, path); err == nil {
		s.Close()
		t.Fatal("accepted corruption")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(bytes) {
		t.Fatal("replaced damaged database")
	}
	s := openTest(t, filepath.Join(t.TempDir(), "library.sqlite"))
	run, events, _ := sample(t)
	events[0].Evidence.SnapshotID = "wrong"
	if err := s.Save(ctx, run, events); err == nil {
		t.Fatal("accepted invalid evidence")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM sources").Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid record published", err)
	}
}

func TestAbruptProcessExit(t *testing.T) {
	if path := os.Getenv("JEVAL_STORAGE_CRASH_TEST"); path != "" {
		s, err := Open(context.Background(), path)
		if err != nil {
			panic(err)
		}
		tx, err := s.db.Begin()
		if err != nil {
			panic(err)
		}
		// Force dirty pages to spill before abrupt process exit; no defers run.
		if _, err = tx.Exec("PRAGMA cache_size=1"); err != nil {
			panic(err)
		}
		if _, err = tx.Exec("UPDATE source_checkpoints SET checkpoint_json='{}'"); err != nil {
			panic(err)
		}
		if _, err = tx.Exec("UPDATE snapshots SET record_json=?", strings.Repeat("x", 1024*1024)); err != nil {
			panic(err)
		}
		os.Exit(23)
	}
	path := filepath.Join(t.TempDir(), "crash.sqlite")
	s := openTest(t, path)
	run, events, cp, _ := checkpointSample(t)
	if err := s.SaveCheckpoint(context.Background(), run, events, cp); err != nil {
		t.Fatal(err)
	}
	s.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAbruptProcessExit$")
	cmd.Env = append(os.Environ(), "JEVAL_STORAGE_CRASH_TEST="+path)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("child failed: %v %s", err, output)
	}
	s = openTest(t, path)
	got, err := s.Current(context.Background(), run.ID)
	if err != nil || !reflect.DeepEqual(got.Events, events) {
		t.Fatal("uncommitted crash damaged snapshot", err)
	}
	if gotCP, err := s.Checkpoint(context.Background(), run.ID); err != nil || !reflect.DeepEqual(gotCP, cp) {
		t.Fatal("uncommitted crash damaged checkpoint", err)
	}
	var result string
	if err = s.db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil || result != "ok" {
		t.Fatal(result, err)
	}
}
