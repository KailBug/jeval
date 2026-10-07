package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"jeval/engine/internal/adapters/codex"
	"jeval/engine/internal/model"
)

func contentSample(t *testing.T, body string) (model.Run, []model.Event, *codex.Checkpoint, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.jsonl")
	row, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]string{"type": "message", "role": "user", "content": body}})
	if err := os.WriteFile(p, append([]byte(`{"type":"session_meta","payload":{"id":"full-body"}}`+"\n"), append(row, '\n')...), 0600); err != nil {
		t.Fatal(err)
	}
	run, events, cp, _, err := codex.ReadUpdate(context.Background(), p, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return run, events, cp, p
}

func TestContentRestartPagingIdentityAndAtomicity(t *testing.T) {
	ctx := context.Background()
	body := strings.Repeat("中🙂\x00\n", 12000) + "TAIL"
	run, events, cp, source := contentSample(t, body)
	if events[0].FullContent == nil || *events[0].FullContent != body || strings.Contains(events[0].Content, "TAIL") {
		t.Fatal("adapter lost full text or leaked tail to preview")
	}
	path := filepath.Join(t.TempDir(), "library.sqlite")
	s := openTest(t, path)
	if err := s.SaveCheckpoint(ctx, run, events, cp); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	var joined strings.Builder
	for offset := 0; ; {
		p, err := s.EventContent(ctx, run.ID, run.ImportInfo.SnapshotID, events[0].ID, offset)
		if err != nil || !p.Available || p.TotalBytes != len(body) || len(p.Content) > ContentPageBytes || !utf8.ValidString(p.Content) {
			t.Fatalf("invalid page: %+v %v", p, err)
		}
		joined.WriteString(p.Content)
		if p.NextOffset == nil {
			break
		}
		if *p.NextOffset <= offset {
			t.Fatal("no progress")
		}
		offset = *p.NextOffset
	}
	if joined.String() != body {
		t.Fatal("body page gap or overlap")
	}
	for _, offset := range []int{-1, 1, len(body) + 1, MaxBodyBytes + 1} {
		if _, err := s.EventContent(ctx, run.ID, run.ImportInfo.SnapshotID, events[0].ID, offset); !errors.Is(err, ErrContentOffset) {
			t.Fatal("accepted bad offset", offset, err)
		}
	}
	if _, err := s.EventContent(ctx, "wrong-run", run.ImportInfo.SnapshotID, events[0].ID, 0); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross run content", err)
	}
	record, err := s.Current(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Events[0].FullContent != nil {
		t.Fatal("preview query eagerly loaded full body")
	}
	if complete, err := s.RestoreContents(ctx, &record); err != nil || !complete || *record.Events[0].FullContent != body {
		t.Fatal("update seed lost body", err)
	}
	// Existing full text is immutable even when preview identity is unchanged.
	changed := body + "different"
	events[0].FullContent = &changed
	if err = s.SaveCheckpoint(ctx, run, events, cp); err == nil {
		t.Fatal("overwrote full body")
	}
	events[0].FullContent = &body
	// Fail after body insertion but before checkpoint publication, then reopen.
	next, ev, nextCP, _ := contentSample(t, "new body")
	if _, err = s.db.Exec(`CREATE TRIGGER fail_body_checkpoint BEFORE INSERT ON source_checkpoints BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveCheckpoint(ctx, next, ev, nextCP); err == nil {
		t.Fatal("failure injection ignored")
	}
	s.Close()
	s = openTest(t, path)
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM event_contents WHERE snapshot_id=?", next.ImportInfo.SnapshotID).Scan(&count); err != nil || count != 0 {
		t.Fatal("orphan body after rollback", err)
	}
	if _, err = s.EventContent(ctx, run.ID, run.ImportInfo.SnapshotID, events[0].ID, 0); err != nil {
		t.Fatal("old body damaged", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = s.SaveCheckpoint(cancelled, next, ev, nextCP); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled save", err)
	}
}

func TestContentV4MigrationMissingEmptyAndExchange(t *testing.T) {
	ctx := context.Background()
	run, events, cp, source := contentSample(t, "")
	path := filepath.Join(t.TempDir(), "v4.sqlite")
	db := createV1(t, path, []model.Record{{SchemaVersion: 1, Runs: []model.Run{run}, Events: events}})
	legacy := &Store{db: db}
	if err := legacy.migrate(ctx, migrations[:4]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE sources SET update_allowed=0"); err != nil {
		t.Fatal(err)
	}
	broken := append([]string{}, migrations...)
	broken[4] += "; INVALID SQL"
	if err := legacy.migrate(ctx, broken); err == nil {
		t.Fatal("bad migration accepted")
	}
	var version, tables int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 4 {
		t.Fatal("migration version advanced", err)
	}
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='event_contents'").Scan(&tables); err != nil || tables != 0 {
		t.Fatal("partial DDL", err)
	}
	db.Close()
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	s := openTest(t, path)
	p, err := s.EventContent(ctx, run.ID, run.ImportInfo.SnapshotID, events[0].ID, 0)
	if err != nil || p.Available {
		t.Fatal("migration invented body", err)
	}
	runs, err := s.Runs(ctx)
	if err != nil || !runs[0].ReadOnly {
		t.Fatal("migration granted source access", err)
	}
	if err = s.SaveExchange(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	p, err = s.EventContent(ctx, run.ID, run.ImportInfo.SnapshotID, events[0].ID, 0)
	if err != nil || p.Available {
		t.Fatal("exchange supplied body", err)
	}
	// A newly authorized parse can supplement the same immutable preview.
	if err = s.SaveCheckpoint(ctx, run, events, cp); err != nil {
		t.Fatal(err)
	}
	p, err = s.EventContent(ctx, run.ID, run.ImportInfo.SnapshotID, events[0].ID, 0)
	if err != nil || !p.Available || p.TotalBytes != 0 || p.Content != "" || p.NextOffset != nil {
		t.Fatal("missing confused with empty", p, err)
	}
}

func TestContentSizeLimitsRollback(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, filepath.Join(t.TempDir(), "library.sqlite"))
	for _, size := range []int{MaxBodyBytes + 1, MaxBodyBytes} {
		run, events := syntheticSnapshot("limits", 3)
		body := strings.Repeat("x", size)
		for i := range events {
			events[i].FullContent = &body
		}
		if err := s.Save(ctx, run, events); err == nil {
			t.Fatal("body limit bypassed", size)
		}
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM snapshots").Scan(&count); err != nil || count != 0 {
			t.Fatal("partial snapshot after limit", err)
		}
	}
	// Separate supplements cannot bypass the per-snapshot aggregate bound.
	run, events := syntheticSnapshot("supplements", 3)
	body := strings.Repeat("x", MaxBodyBytes)
	events[0].FullContent, events[1].FullContent = &body, &body
	if err := s.Save(ctx, run, events); err != nil {
		t.Fatal(err)
	}
	events[0].FullContent, events[1].FullContent = nil, nil
	extra := "one more byte"
	events[2].FullContent = &extra
	if err := s.Save(ctx, run, events); err == nil {
		t.Fatal("supplements exceeded snapshot bound")
	}
	p, err := s.EventContent(ctx, run.ID, run.ImportInfo.SnapshotID, events[2].ID, 0)
	if err != nil || p.Available {
		t.Fatal("failed supplement survived rollback", err)
	}
}
