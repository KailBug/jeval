package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"jeval/engine/internal/model"
)

func TestV2MigrationPreservesSnapshotsAndLocalPermission(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v2.sqlite")
	run, events, _ := sample(t)
	record := model.Record{SchemaVersion: 1, Runs: []model.Run{run}, Events: events}
	db := createV1(t, path, []model.Record{record})
	legacy := &Store{db: db}
	if err := legacy.migrate(ctx, migrations[:2]); err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := db.QueryRow("SELECT record_json FROM snapshots").Scan(&before); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s := openTest(t, path)
	var version, allowed int
	var after []byte
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != SchemaVersion {
		t.Fatal(version, err)
	}
	if err := s.db.QueryRow("SELECT update_allowed FROM sources").Scan(&allowed); err != nil || allowed != 1 {
		t.Fatal(allowed, err)
	}
	if err := s.db.QueryRow("SELECT record_json FROM snapshots").Scan(&after); err != nil || string(before) != string(after) {
		t.Fatal("migration rewrote snapshot", err)
	}
	if got, err := s.Runs(ctx); err != nil || !reflect.DeepEqual(got, []model.Run{run}) {
		t.Fatal(got, err)
	}
	if got, err := s.Current(ctx, run.ID); err != nil || !reflect.DeepEqual(got, record) {
		t.Fatal(got, err)
	}
}

func TestV3MigrationFailurePreservesV2MarkerAndRecords(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "failed-v2.sqlite")
	run, events, _ := sample(t)
	db := createV1(t, path, []model.Record{{SchemaVersion: 1, Runs: []model.Run{run}, Events: events}})
	legacy := &Store{db: db}
	if err := legacy.migrate(ctx, migrations[:2]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ALTER TABLE sources ADD COLUMN update_allowed INTEGER DEFAULT 1"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := Open(ctx, path); err == nil {
		s.Close()
		t.Fatal("conflicting migration succeeded")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, count int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatal(version, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM snapshots").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestSaveExchangeDetachesSurvivesRestartAndExplicitSaveRebinds(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "detached.sqlite")
	s := openTest(t, path)
	run, events, source := sample(t)
	for range 2 {
		if err := s.SaveExchange(ctx, run, events); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.Runs(ctx); err != nil || len(got) != 1 || !got[0].ReadOnly {
		t.Fatal("exchange granted source read permission", got, err)
	}
	canonical, err := s.Current(ctx, run.ID)
	if err != nil || canonical.Runs[0].ReadOnly {
		t.Fatal("local permission leaked into snapshot", err)
	}
	data, err := json.Marshal(canonical)
	if err != nil || strings.Contains(string(data), "readOnly") {
		t.Fatal("canonical record contains local permission", err)
	}
	s.Close()
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	if got, err := s.Runs(ctx); err != nil || !got[0].ReadOnly {
		t.Fatal("restart authorized embedded source path", got, err)
	}
	if page, total, err := s.Events(ctx, run.ID, "", "", 0, 100); err != nil || total != len(events) || !reflect.DeepEqual(page, events) {
		t.Fatal("detached events lost", err)
	}
	// The normal Save API represents an explicit picker/confirmed source import.
	if err := s.Save(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Runs(ctx); err != nil || got[0].ReadOnly {
		t.Fatal("explicit binding not saved", got, err)
	}
	if err := s.SaveExchange(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Runs(ctx); err != nil || got[0].ReadOnly {
		t.Fatal("idempotent native import removed local permission", got, err)
	}
}

func TestSaveExchangeRejectsDifferentCurrentAndKeepsPreviousOnFailure(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, filepath.Join(t.TempDir(), "conflicts.sqlite"))
	run, events, _ := sample(t)
	if err := s.SaveExchange(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_binding BEFORE UPDATE ON sources BEGIN SELECT RAISE(ABORT,'injected binding failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, run, events); err == nil {
		t.Fatal("expected source permission write failure")
	}
	if got, err := s.Runs(ctx); err != nil || !got[0].ReadOnly {
		t.Fatal("failed save granted source permission", got, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_binding"); err != nil {
		t.Fatal(err)
	}
	changed := run
	info := *run.ImportInfo
	changed.ImportInfo = &info
	changed.ImportInfo.SHA256 = strings.Repeat("b", 64)
	changed.ImportInfo.SnapshotID = model.SnapshotID(run.ID, changed.ImportInfo.SHA256, info.AdapterVersion)
	changedEvents := append([]model.Event{}, events...)
	for n := range changedEvents {
		changedEvents[n].Evidence.SnapshotID = changed.ImportInfo.SnapshotID
	}
	if err := s.SaveExchange(ctx, changed, changedEvents); !errors.Is(err, ErrExchangeConflict) {
		t.Fatal("different current exchange accepted", err)
	}
	if _, err := s.Snapshot(ctx, changed.ImportInfo.SnapshotID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("rejected snapshot leaked", err)
	}
	if err := s.Save(ctx, changed, changedEvents); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveExchange(ctx, run, events); !errors.Is(err, ErrExchangeConflict) {
		t.Fatal("old exchange rolled back current snapshot", err)
	}
	if got, err := s.Current(ctx, run.ID); err != nil || got.Runs[0].ImportInfo.SnapshotID != changed.ImportInfo.SnapshotID {
		t.Fatal("collision altered current", err)
	}
	if got, err := s.Runs(ctx); err != nil || got[0].ReadOnly {
		t.Fatal("collision altered permission", got, err)
	}
}

func TestSaveExchangePublicationFailureLeavesNoPartialRows(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "failure.sqlite"))
	run, events, _ := sample(t)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_exchange BEFORE INSERT ON sources BEGIN SELECT RAISE(ABORT,'injected detached publish failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveExchange(context.Background(), run, events); err == nil {
		t.Fatal("expected detached publish failure")
	}
	for _, table := range []string{"snapshots", "sources", "snapshot_runs", "snapshot_events"} {
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial detached rows leaked", table, count, err)
		}
	}
}

func TestExportPathProtectsLibrarySourcesAndAliases(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "library.sqlite")
	s := openTest(t, database)
	run, events, source := sample(t)
	if err := s.Save(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{database, database + "-wal", database + "-shm", database + "-journal", source} {
		if err := s.CheckExportPath(ctx, target); err == nil {
			t.Fatal("protected export destination accepted", target)
		}
	}
	if runtime.GOOS == "windows" && s.CheckExportPath(ctx, strings.ToUpper(source)) == nil {
		t.Fatal("case alias accepted")
	}
	t.Run("hard link", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "alias.json")
		if err := os.Link(source, alias); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		if err := s.CheckExportPath(ctx, alias); err == nil {
			t.Fatal("source hard-link export accepted")
		}
	})
	t.Run("database hard link", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "alias.sqlite")
		if err := os.Link(database, alias); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		if err := s.CheckExportPath(ctx, alias); err == nil {
			t.Fatal("database hard-link export accepted")
		}
	})
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckExportPath(ctx, filepath.Join(t.TempDir(), "offline.json")); err != nil {
		t.Fatal("offline source prevented export", err)
	}
}

func TestDetachedLocatorNeverGetsFilesystemResolutionDuringExport(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, filepath.Join(t.TempDir(), "detached.sqlite"))
	run, events, _ := sample(t)
	// Any filesystem operation on this package-provided locator would fail
	// with an invalid filename. It remains valid evidence text in storage.
	info := *run.ImportInfo
	run.ImportInfo = &info
	run.ImportInfo.File = filepath.Join(t.TempDir(), "untrusted\x00-source.jsonl")
	for n := range events {
		events[n].Evidence.Location = run.ImportInfo.File
	}
	if err := s.SaveExchange(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "export.json")
	// Use an existing target so all normal local-path alias checks execute.
	if err := os.WriteFile(target, []byte("previous bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckExportPath(ctx, target); err != nil {
		t.Fatal("untrusted detached locator was resolved or statted", err)
	}
}
