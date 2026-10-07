package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"jeval/engine/internal/adapters/codex"
	"jeval/engine/internal/model"
)

func checkpointSample(t *testing.T) (model.Run, []model.Event, *codex.Checkpoint, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.jsonl")
	data, err := os.ReadFile("../../../fixtures/adapters/codex/classic.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	run, events, cp, _, err := codex.ReadUpdate(context.Background(), path, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return run, events, cp, path
}

func TestCheckpointTransactionRollbackRestartAndCacheInvalidation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.sqlite")
	s := openTest(t, path)
	run, events, cp, source := checkpointSample(t)
	if err := s.SaveCheckpoint(ctx, run, events, cp); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTest(t, path)
	got, err := s.Checkpoint(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(got, cp) {
		t.Fatal(got, err)
	}
	old, err := s.Current(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(source, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"type":"event_msg","payload":{"type":"task_complete"}}` + "\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	next, ev, nextCP, _, err := codex.ReadUpdate(ctx, source, cp, &old, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER fail_checkpoint BEFORE INSERT ON source_checkpoints BEGIN SELECT RAISE(ABORT,'checkpoint failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveCheckpoint(ctx, next, ev, nextCP); err == nil {
		t.Fatal("injected failure accepted")
	}
	s.Close()
	s = openTest(t, path)
	current, err := s.Current(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(current, old) {
		t.Fatal("snapshot advanced despite rollback", err)
	}
	got, err = s.Checkpoint(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(got, cp) {
		t.Fatal("checkpoint advanced despite rollback", err)
	}
	if _, err = s.Snapshot(ctx, next.ImportInfo.SnapshotID); err != sql.ErrNoRows {
		t.Fatal("failed snapshot persisted", err)
	}
	if _, err = s.db.Exec("DROP TRIGGER fail_checkpoint"); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = s.SaveCheckpoint(cancelled, next, ev, nextCP); err == nil {
		t.Fatal("cancelled save accepted")
	}
	if err = s.SaveCheckpoint(ctx, next, ev, nextCP); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE source_checkpoints SET checkpoint_json='{}'"); err != nil {
		t.Fatal(err)
	}
	got, err = s.Checkpoint(ctx, run.ID)
	if err != nil || got == nil || got.Version != -1 {
		t.Fatal("corrupt cache not detected", got, err)
	}
	if err = s.Save(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	if got, err = s.Checkpoint(ctx, run.ID); err != nil || got != nil {
		t.Fatal("explicit noncheckpoint save kept stale cache", got, err)
	}
}

func TestV3CheckpointMigrationPreservesRecordsAndPermissions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v3.sqlite")
	run, events, _, _ := checkpointSample(t)
	record := model.Record{SchemaVersion: 1, Runs: []model.Run{run}, Events: events}
	db := createV1(t, path, []model.Record{record})
	legacy := &Store{db: db}
	if err := legacy.migrate(ctx, migrations[:3]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE sources SET update_allowed=0"); err != nil {
		t.Fatal(err)
	}
	// Failure after the new DDL must restore the v3 version and table layout.
	broken := append([]string{}, migrations...)
	broken[3] += "; INVALID SQL"
	if err := legacy.migrate(ctx, broken); err == nil {
		t.Fatal("broken checkpoint migration accepted")
	}
	var version, tables int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 3 {
		t.Fatal("failed migration changed version", err)
	}
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='source_checkpoints'").Scan(&tables); err != nil || tables != 0 {
		t.Fatal("failed migration left checkpoint table", err)
	}
	db.Close()
	s := openTest(t, path)
	got, err := s.Current(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(got, record) {
		t.Fatal("migration changed evidence", err)
	}
	runs, err := s.Runs(ctx)
	if err != nil || !runs[0].ReadOnly {
		t.Fatal("migration granted access", err)
	}
	if cp, err := s.Checkpoint(ctx, run.ID); err != nil || cp != nil {
		t.Fatal(cp, err)
	}
	if err := s.SaveExchange(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	if cp, err := s.Checkpoint(ctx, run.ID); err != nil || cp != nil {
		t.Fatal("exchange supplied a local checkpoint", err)
	}
}
