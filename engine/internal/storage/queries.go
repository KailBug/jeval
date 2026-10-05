package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"jeval/engine/internal/model"
	"strings"
)

// backfill runs inside the DDL transaction and validates each historical
// snapshot before deriving its index. It does not publish a partially migrated
// database when an old record is malformed or a write fails.
func backfill(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT id, source_id, record_json FROM snapshots ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, sourceID string
		var data []byte
		if err = rows.Scan(&id, &sourceID, &data); err != nil {
			return err
		}
		record, decodeErr := decodeRecord(data)
		if decodeErr != nil {
			return fmt.Errorf("invalid historical snapshot: %w", decodeErr)
		}
		run := record.Runs[0]
		if run.ID != sourceID || run.ImportInfo.SnapshotID != id {
			return errors.New("historical snapshot key mismatch")
		}
		if err = writeIndex(ctx, tx, run, record.Events); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	var missing int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sources LEFT JOIN snapshot_runs ON sources.id=snapshot_runs.source_id AND sources.current_snapshot_id=snapshot_runs.snapshot_id WHERE snapshot_runs.snapshot_id IS NULL`).Scan(&missing); err != nil {
		return err
	}
	if missing != 0 {
		return errors.New("current source references a missing snapshot")
	}
	return checkCapacity(ctx, tx, "", 0)
}

func writeIndex(ctx context.Context, tx *sql.Tx, run model.Run, events []model.Event) error {
	runJSON, err := json.Marshal(run)
	if err != nil {
		return err
	}
	id := run.ImportInfo.SnapshotID
	if _, err = tx.ExecContext(ctx, "INSERT INTO snapshot_runs(snapshot_id, source_id, run_json, event_count) VALUES(?,?,?,?)", id, run.ID, runJSON, run.EventCount); err != nil {
		return err
	}
	insert, err := tx.PrepareContext(ctx, "INSERT INTO snapshot_events(snapshot_id, sequence, id, kind, search_text, event_json) VALUES(?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer insert.Close()
	for _, event := range events {
		data, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return marshalErr
		}
		// SQLite's built-in lower() only handles ASCII. Normalize in Go so
		// Unicode search keeps the original in-memory strings.ToLower behavior.
		search := strings.ToLower(event.Title + " " + event.Content + " " + event.Role)
		if _, err = insert.ExecContext(ctx, id, event.Sequence, event.ID, event.Kind, search, data); err != nil {
			return err
		}
	}
	return nil
}

func checkCapacity(ctx context.Context, tx *sql.Tx, replacingID string, count int) error {
	var sources, events int
	if err := tx.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(snapshot_runs.event_count),0) FROM sources JOIN snapshot_runs ON sources.current_snapshot_id=snapshot_runs.snapshot_id AND sources.id=snapshot_runs.source_id WHERE sources.id<>?`, replacingID).Scan(&sources, &events); err != nil {
		return err
	}
	if replacingID != "" {
		sources++
	}
	if sources > maxCurrentSources || events+count > maxCurrentEvents {
		return errors.New("task library limit exceeded: maximum 20 files and 50000 events")
	}
	return nil
}

func decodeRun(data []byte) (model.Run, error) {
	var run model.Run
	if err := json.Unmarshal(data, &run); err != nil {
		return run, fmt.Errorf("invalid saved run index: %w", err)
	}
	if err := validateRun(run); err != nil {
		return model.Run{}, err
	}
	return run, nil
}

// Runs restores only current metadata, never record_json or event rows. The
// existing import cap bounds this small metadata set; event queries stay in SQL.
func (s *Store) Runs(ctx context.Context) ([]model.Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sources.id,sources.current_snapshot_id,snapshot_runs.event_count,snapshot_runs.run_json FROM sources LEFT JOIN snapshot_runs ON sources.id=snapshot_runs.source_id AND sources.current_snapshot_id=snapshot_runs.snapshot_id ORDER BY sources.rowid LIMIT ?`, maxCurrentSources+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []model.Run{}
	events := 0
	for rows.Next() {
		var data []byte
		var sourceID, snapshotID string
		var eventCount int
		if err = rows.Scan(&sourceID, &snapshotID, &eventCount, &data); err != nil {
			return nil, err
		}
		run, decodeErr := decodeRun(data)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if run.ID != sourceID || run.ImportInfo.SnapshotID != snapshotID || run.EventCount != eventCount {
			return nil, errors.New("saved run index key mismatch")
		}
		runs = append(runs, run)
		events += run.EventCount
		if len(runs) > maxCurrentSources || events > maxCurrentEvents {
			return nil, errors.New("saved task library exceeds import limits")
		}
	}
	return runs, rows.Err()
}

// Events filters the saved previews and loads one bounded SQL page. Count and
// page share a read transaction so a concurrently published snapshot cannot
// mix totals, metadata or evidence from two versions.
func (s *Store) Events(ctx context.Context, sourceID, search, kind string, offset, limit int) ([]model.Event, int, error) {
	if offset < 0 || offset > 1000000000 || limit < 1 || limit > 100 || len(search) > 1000 {
		return nil, 0, errors.New("invalid event page: offset must be 0..1000000000, limit 1..100, search at most 1000 bytes")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var snapshotID string
	var data []byte
	var eventCount int
	if err = tx.QueryRowContext(ctx, `SELECT sources.current_snapshot_id, snapshot_runs.event_count,snapshot_runs.run_json FROM sources LEFT JOIN snapshot_runs ON sources.id=snapshot_runs.source_id AND sources.current_snapshot_id=snapshot_runs.snapshot_id WHERE sources.id=?`, sourceID).Scan(&snapshotID, &eventCount, &data); err != nil {
		return nil, 0, err
	}
	run, err := decodeRun(data)
	if err != nil {
		return nil, 0, err
	}
	if run.ID != sourceID || run.ImportInfo.SnapshotID != snapshotID || run.EventCount != eventCount {
		return nil, 0, errors.New("saved run index key mismatch")
	}
	search = strings.ToLower(strings.TrimSpace(search))
	if kind == "all" {
		kind = ""
	}
	const where = `snapshot_id=? AND (?='' OR kind=?) AND instr(search_text,?)>0`
	var total int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM snapshot_events WHERE "+where, snapshotID, kind, kind, search).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT sequence,kind,event_json FROM snapshot_events WHERE "+where+" ORDER BY sequence LIMIT ? OFFSET ?", snapshotID, kind, kind, search, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	items := []model.Event{}
	for rows.Next() {
		var sequence int
		var savedKind string
		var event model.Event
		if err = rows.Scan(&sequence, &savedKind, &data); err == nil {
			err = json.Unmarshal(data, &event)
		}
		if err == nil {
			err = validateEvent(run, event, sequence)
		}
		if err == nil && event.Kind != savedKind {
			err = errors.New("saved event kind mismatch")
		}
		if err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("invalid saved event index: %w", err)
		}
		items = append(items, event)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	if err = rows.Close(); err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
