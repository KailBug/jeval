package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"jeval/engine/internal/model"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func syntheticSnapshot(id string, count int) (model.Run, []model.Event) {
	info := &model.ImportInfo{File: "synthetic:" + id, SHA256: strings.Repeat("a", 64), AdapterVersion: "storage-test-v1", Warnings: []model.ImportWarning{}}
	info.SnapshotID = model.SnapshotID(id, info.SHA256, info.AdapterVersion)
	run := model.Run{ID: id, Source: "Codex", Title: "Synthetic", Status: "unknown", EventCount: count, ImportInfo: info}
	events := make([]model.Event, count)
	for n := range events {
		events[n] = model.Event{ID: fmt.Sprintf("event-%d", n+1), RunID: id, Sequence: n + 1, Kind: "message", Role: "assistant", Title: "Résumé", Content: "Σ 中文 EVENT", Evidence: model.EvidenceRef{SourceID: id, SnapshotID: info.SnapshotID, Location: info.File, Line: n + 1}}
	}
	return run, events
}

func createV1(t *testing.T, path string, records []model.Record) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(migrations[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		run := record.Runs[0]
		data, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, err = db.Exec("INSERT INTO snapshots(id,source_id,record_json) VALUES(?,?,?)", run.ImportInfo.SnapshotID, run.ID, data); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO sources(id,current_snapshot_id) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET current_snapshot_id=excluded.current_snapshot_id`, run.ID, run.ImportInfo.SnapshotID); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestV1BackfillPreservesHistoryAndQueriesCurrent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.sqlite")
	old, oldEvents := syntheticSnapshot("same-source", 2)
	current, currentEvents := syntheticSnapshot("same-source", 4)
	current.ImportInfo.SHA256 = strings.Repeat("b", 64)
	current.ImportInfo.SnapshotID = model.SnapshotID(current.ID, current.ImportInfo.SHA256, current.ImportInfo.AdapterVersion)
	for n := range currentEvents {
		currentEvents[n].Evidence.SnapshotID = current.ImportInfo.SnapshotID
	}
	db := createV1(t, path, []model.Record{{SchemaVersion: 1, Runs: []model.Run{old}, Events: oldEvents}, {SchemaVersion: 1, Runs: []model.Run{current}, Events: currentEvents}})
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s := openTest(t, path)
	var version, indexed int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatal("migration version", version, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM snapshot_events").Scan(&indexed); err != nil || indexed != 6 {
		t.Fatal("historical indexes", indexed, err)
	}
	runs, err := s.Runs(ctx)
	if err != nil || !reflect.DeepEqual(runs, []model.Run{current}) {
		t.Fatal("current metadata", runs, err)
	}
	page, total, err := s.Events(ctx, current.ID, " RÉSUMÉ ", "all", 1, 2)
	if err != nil || total != 4 || !reflect.DeepEqual(page, currentEvents[1:3]) {
		t.Fatal("migrated Unicode page", page, total, err)
	}
	history, err := s.Snapshot(ctx, old.ImportInfo.SnapshotID)
	if err != nil || !reflect.DeepEqual(history.Events, oldEvents) {
		t.Fatal("historical record", err)
	}
}

func TestV1InvalidHistoryRollsBackEntireBackfill(t *testing.T) {
	for _, corruption := range []string{"evidence", "key", "schema"} {
		t.Run(corruption, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid-v1.sqlite")
			valid, validEvents := syntheticSnapshot("valid", 3)
			invalid, invalidEvents := syntheticSnapshot("invalid", 2)
			record := model.Record{SchemaVersion: 1, Runs: []model.Run{invalid}, Events: invalidEvents}
			if corruption == "evidence" {
				invalidEvents[0].Evidence.SnapshotID = "wrong"
			}
			if corruption == "schema" {
				record.SchemaVersion = 999
			}
			db := createV1(t, path, []model.Record{{SchemaVersion: 1, Runs: []model.Run{valid}, Events: validEvents}, record})
			if corruption == "key" {
				if _, err := db.Exec("UPDATE snapshots SET source_id='wrong' WHERE id=?", invalid.ImportInfo.SnapshotID); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()
			if s, err := Open(context.Background(), path); err == nil {
				s.Close()
				t.Fatal("accepted invalid historical record")
			}
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var version, tables, snapshots int
			if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
				t.Fatal("migration marker changed", version, err)
			}
			if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('snapshot_runs','snapshot_events','directories')").Scan(&tables); err != nil || tables != 0 {
				t.Fatal("partial v2 schema leaked", tables, err)
			}
			if err = db.QueryRow("SELECT count(*) FROM snapshots").Scan(&snapshots); err != nil || snapshots != 2 {
				t.Fatal("original records changed", snapshots, err)
			}
		})
	}
}

func TestSQLPagingUnicodeLiteralSearchAndBoundedDecode(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pages.sqlite")
	s := openTest(t, path)
	run, events := syntheticSnapshot("unicode", 120)
	events[2].Kind = "error"
	events[2].Content = "100%_LITERAL 雪"
	events[3].Content = "other"
	if err := s.Save(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		search, kind         string
		offset, limit, total int
		indices              []int
	}{
		{"σ 中文 event", "all", 0, 2, 118, []int{0, 1}},
		{"RÉSUMÉ", "message", 117, 2, 119, []int{118, 119}},
		{"100%_literal", "error", 0, 1, 1, []int{2}},
		{"no matches", "", 0, 50, 0, nil},
		{"", "", 1000, 50, 120, nil},
	} {
		page, total, err := s.Events(ctx, run.ID, test.search, test.kind, test.offset, test.limit)
		if err != nil || total != test.total || len(page) != len(test.indices) {
			t.Fatalf("search %q: page=%d total=%d err=%v", test.search, len(page), total, err)
		}
		for n, index := range test.indices {
			if !reflect.DeepEqual(page[n], events[index]) {
				t.Fatal("evidence or ordered page changed", page[n])
			}
		}
	}
	if _, _, err := s.Events(ctx, "missing", "", "", 0, 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("missing source", err)
	}
	for _, test := range []struct{ offset, limit int }{{-1, 1}, {0, 0}, {0, 101}, {1000000001, 1}} {
		if _, _, err := s.Events(ctx, run.ID, "", "", test.offset, test.limit); err == nil {
			t.Fatal("accepted invalid page", test)
		}
	}
	// Metadata restoration and an early page must not decode the whole record
	// or event rows outside the requested page. History reads still validate it.
	if _, err := s.db.Exec("UPDATE snapshots SET record_json='broken'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE snapshot_events SET event_json='broken' WHERE sequence=120"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTest(t, path)
	if runs, err := s.Runs(ctx); err != nil || !reflect.DeepEqual(runs, []model.Run{run}) {
		t.Fatal("startup decoded full immutable record", err)
	}
	if page, total, err := s.Events(ctx, run.ID, "", "", 0, 2); err != nil || total != 120 || len(page) != 2 {
		t.Fatal("page decoded unrelated rows", total, err)
	}
	if _, _, err := s.Events(ctx, run.ID, "", "", 119, 1); err == nil {
		t.Fatal("accepted damaged requested event")
	}
	if _, err := s.Current(ctx, run.ID); err == nil {
		t.Fatal("history API accepted damaged record")
	}
}

func TestDerivedIndexFailureRollsBackSnapshotAndPointer(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, filepath.Join(t.TempDir(), "atomic.sqlite"))
	run, events := syntheticSnapshot("atomic", 3)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_event BEFORE INSERT ON snapshot_events WHEN NEW.sequence=2 BEGIN SELECT RAISE(ABORT,'injected event index failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, run, events); err == nil {
		t.Fatal("expected derived index write failure")
	}
	for _, table := range []string{"snapshots", "sources", "snapshot_runs", "snapshot_events"} {
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial snapshot leaked", table, count, err)
		}
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_event"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	wrong, _ := syntheticSnapshot("wrong-source", 3)
	wrongJSON, err := json.Marshal(wrong)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE snapshot_runs SET run_json=?", wrongJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Runs(ctx); err == nil {
		t.Fatal("accepted invalid metadata index")
	}
	if _, _, err := s.Events(ctx, run.ID, "", "", 0, 1); err == nil {
		t.Fatal("accepted metadata belonging to another source")
	}
}

func TestStorageKeepsImportCapsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "caps.sqlite")
	s := openTest(t, path)
	for n := range 20 {
		run, events := syntheticSnapshot(fmt.Sprintf("source-%d", n), 0)
		if err := s.Save(ctx, run, events); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s = openTest(t, path)
	run, events := syntheticSnapshot("source-20", 0)
	if err := s.Save(ctx, run, events); err == nil {
		t.Fatal("accepted twenty-first source after restart")
	}
	run, events = syntheticSnapshot("source-0", 1)
	run.ImportInfo.SHA256 = strings.Repeat("b", 64)
	run.ImportInfo.SnapshotID = model.SnapshotID(run.ID, run.ImportInfo.SHA256, run.ImportInfo.AdapterVersion)
	events[0].Evidence.SnapshotID = run.ImportInfo.SnapshotID
	if err := s.Save(ctx, run, events); err != nil {
		t.Fatal("replacement rejected at source cap", err)
	}
	run, events = syntheticSnapshot("too-large", 5001)
	if err := s.Save(ctx, run, events); err == nil {
		t.Fatal("accepted per-snapshot event limit violation")
	}
}

func TestCurrentEventCapKeepsPreviousSnapshotAndAllowsReplacement(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, filepath.Join(t.TempDir(), "event-cap.sqlite"))
	for n := range 10 {
		run, events := syntheticSnapshot(fmt.Sprintf("large-%d", n), 5000)
		if err := s.Save(ctx, run, events); err != nil {
			t.Fatal(err)
		}
	}
	run, events := syntheticSnapshot("over-limit", 1)
	if err := s.Save(ctx, run, events); err == nil {
		t.Fatal("accepted more than 50000 current events")
	}
	if _, err := s.Snapshot(ctx, run.ImportInfo.SnapshotID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("over-limit snapshot leaked", err)
	}
	replacement, fewer := syntheticSnapshot("large-0", 1)
	replacement.ImportInfo.SHA256 = strings.Repeat("b", 64)
	replacement.ImportInfo.SnapshotID = model.SnapshotID(replacement.ID, replacement.ImportInfo.SHA256, replacement.ImportInfo.AdapterVersion)
	fewer[0].Evidence.SnapshotID = replacement.ImportInfo.SnapshotID
	if err := s.Save(ctx, replacement, fewer); err != nil {
		t.Fatal("replacement did not free current event budget", err)
	}
	if err := s.Save(ctx, run, events); err != nil {
		t.Fatal("new source rejected after shrinking current snapshot", err)
	}
	old, _ := syntheticSnapshot("large-0", 5000)
	if history, err := s.Snapshot(ctx, old.ImportInfo.SnapshotID); err != nil || len(history.Events) != 5000 {
		t.Fatal("event budget removed immutable history", err)
	}
}

func TestDirectoriesPersistOfflineWithoutImportAndRemovalKeepsSnapshots(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dirs.sqlite")
	s := openTest(t, path)
	root := filepath.Join(t.TempDir(), "中文 chosen root")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	directory, err := s.SaveDirectory(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.SaveDirectory(ctx, filepath.Join(root, "."))
	if err != nil || directory != duplicate {
		t.Fatal("directory identity unstable", duplicate, err)
	}
	if runs, err := s.Runs(ctx); err != nil || len(runs) != 0 {
		t.Fatal("saving directory imported records", err)
	}
	run, events := syntheticSnapshot("saved", 2)
	if err := s.Save(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	if directories, err := s.Directories(ctx); err != nil || !reflect.DeepEqual(directories, []Directory{directory}) {
		t.Fatal("offline directory configuration", directories, err)
	}
	if err := s.RemoveDirectory(ctx, directory.ID); err != nil {
		t.Fatal(err)
	}
	if directories, err := s.Directories(ctx); err != nil || len(directories) != 0 {
		t.Fatal("directory removal", directories, err)
	}
	if record, err := s.Current(ctx, run.ID); err != nil || !reflect.DeepEqual(record.Events, events) {
		t.Fatal("directory removal deleted snapshot", err)
	}
	for _, invalid := range []string{"relative", filepath.Join(t.TempDir(), "missing"), path, strings.Repeat("x", 2049)} {
		if _, err := s.SaveDirectory(ctx, invalid); err == nil {
			t.Fatal("accepted invalid directory", invalid)
		}
	}
}

func TestDirectoryCapAllowsIdempotentSave(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "dirs.sqlite"))
	ctx := context.Background()
	parent := t.TempDir()
	first := ""
	for n := range 20 {
		path := filepath.Join(parent, fmt.Sprintf("dir-%d", n))
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			first = path
		}
		if _, err := s.SaveDirectory(ctx, path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SaveDirectory(ctx, first); err != nil {
		t.Fatal("idempotent save rejected at directory cap", err)
	}
	if _, err := s.SaveDirectory(ctx, parent); err == nil {
		t.Fatal("accepted twenty-first directory")
	}
}
