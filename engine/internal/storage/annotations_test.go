package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnnotationHistoryConflictAndFailureProtection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "annotations.sqlite")
	s := openTest(t, path)
	run, events, _ := sample(t)
	if err := s.Save(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	snapshot := run.ImportInfo.SnapshotID
	event := events[0].ID
	a, err := s.SaveAnnotation(ctx, run.ID, snapshot, &event, "rejected", "中文🙂 <script>plain note</script>", 0, false)
	if err != nil || a.Revision != 1 || a.Event.Evidence != events[0].Evidence {
		t.Fatal(a, err)
	}
	if _, err = s.SaveAnnotation(ctx, run.ID, snapshot, nil, "accepted", "snapshot review", 0, false); err != nil {
		t.Fatal(err)
	}
	newRun, newEvents := syntheticSnapshot(run.ID, 1)
	if err = s.Save(ctx, newRun, newEvents); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveAnnotation(ctx, "wrong", snapshot, &event, "accepted", "", 0, false); err == nil {
		t.Fatal("accepted wrong source")
	}
	if _, err = s.SaveAnnotation(ctx, run.ID, newRun.ImportInfo.SnapshotID, &event, "accepted", "", 0, false); err == nil {
		t.Fatal("accepted wrong snapshot event")
	}
	s.Close()
	s = openTest(t, path)
	a, err = s.Annotation(ctx, run.ID, snapshot, &event)
	if err != nil || a.Note != "中文🙂 <script>plain note</script>" {
		t.Fatal(a, err)
	}
	oldEvents, total, err := s.EventsAt(ctx, run.ID, snapshot, "", "", 0, 50)
	if err != nil || total != len(events) || oldEvents[0].Evidence != events[0].Evidence {
		t.Fatal("historical evidence lost", err)
	}
	history, total, err := s.Snapshots(ctx, run.ID, 0, 1)
	if err != nil || total != 2 || len(history) != 1 || history[0].ImportInfo.SnapshotID != newRun.ImportInfo.SnapshotID {
		t.Fatal(history, total, err)
	}
	if _, err = s.SaveAnnotation(ctx, run.ID, snapshot, &event, "accepted", "stale", 0, false); !errors.Is(err, ErrAnnotationConflict) {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER reject_annotation BEFORE UPDATE ON annotations BEGIN SELECT RAISE(ABORT,'injected annotation write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveAnnotation(ctx, run.ID, snapshot, &event, "accepted", "lost", 1, false); err == nil {
		t.Fatal("write failure hidden")
	}
	a, err = s.Annotation(ctx, run.ID, snapshot, &event)
	if err != nil || a.Revision != 1 || a.Judgement != "rejected" {
		t.Fatal(a, err)
	}
	s.db.Exec(`DROP TRIGGER reject_annotation`)
	a, err = s.SaveAnnotation(ctx, run.ID, snapshot, &event, "uncertain", "", 1, true)
	if err != nil || !a.Deleted || a.Revision != 2 {
		t.Fatal(a, err)
	}
	if _, err = s.SaveAnnotation(ctx, run.ID, snapshot, &event, "accepted", "stale recreation", 0, false); !errors.Is(err, ErrAnnotationConflict) {
		t.Fatal(err)
	}
	a, err = s.SaveAnnotation(ctx, run.ID, snapshot, &event, "accepted", "recreated", 2, false)
	if err != nil || a.Revision != 3 {
		t.Fatal(a, err)
	}
	for _, note := range []string{strings.Repeat("中", 1366), "bad\x00note", string([]byte{0xff})} {
		if _, err = s.SaveAnnotation(ctx, run.ID, snapshot, &event, "accepted", note, 3, false); !errors.Is(err, ErrAnnotationInput) {
			t.Fatal("invalid note", err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.SaveAnnotation(cancelled, run.ID, snapshot, &event, "accepted", "cancelled", 3, false); err == nil {
		t.Fatal("cancelled save committed")
	}
	list, total, err := s.Annotations(ctx, run.ID, snapshot, 0, 1)
	if err != nil || total != 2 || len(list) != 1 || list[0].EventID != nil {
		t.Fatal(list, total, err)
	}
	current, err := s.Current(ctx, run.ID)
	if err != nil || current.Runs[0].ImportInfo.SnapshotID != newRun.ImportInfo.SnapshotID {
		t.Fatal("annotation moved current snapshot", err)
	}
}

func TestAnnotationMigrationPreservesV5SnapshotsAndRollback(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v5.sqlite")
	s := openTest(t, path)
	if _, err := s.db.Exec(`DROP TABLE annotations; PRAGMA user_version=5`); err != nil {
		t.Fatal(err)
	}
	run, events, _ := sample(t)
	if err := s.Save(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	bad := append([]string{}, migrations...)
	bad[5] += ` CREATE TABLE snapshots(id TEXT);`
	if err := s.migrate(ctx, bad); err == nil {
		t.Fatal("failed migration committed")
	}
	var version int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != 5 {
		t.Fatal(version)
	}
	var count int
	s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='annotations'`).Scan(&count)
	if count != 0 {
		t.Fatal("partial annotation table")
	}
	s.Close()
	s = openTest(t, path)
	saved, err := s.Snapshot(ctx, run.ImportInfo.SnapshotID)
	if err != nil || saved.Events[0].Evidence != events[0].Evidence {
		t.Fatal(err)
	}
	if _, err = s.SaveAnnotation(ctx, run.ID, run.ImportInfo.SnapshotID, nil, "uncertain", "migrated", 0, false); err != nil {
		t.Fatal(err)
	}
}
