// Package storage persists immutable previews, full normalized text and indexes
// for bounded queries. It never reads or changes source files.
package storage

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"jeval/engine/internal/model"
	"path/filepath"
	"strings"

	"jeval/engine/internal/adapters/codex"

	_ "modernc.org/sqlite"
)

const SchemaVersion = 6
const schemaVersion = SchemaVersion

const (
	maxCurrentSources = model.MaxImportedSources
	maxCurrentEvents  = model.MaxCurrentEvents
	maxSnapshotEvents = model.MaxSnapshotEvents
)

var migrations = []string{`
CREATE TABLE snapshots (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL,
  record_json BLOB NOT NULL,
  UNIQUE(source_id, id)
);
CREATE TABLE sources (
  id TEXT PRIMARY KEY,
  current_snapshot_id TEXT NOT NULL,
  FOREIGN KEY(id, current_snapshot_id) REFERENCES snapshots(source_id, id)
);`, `
CREATE TABLE snapshot_runs (
  snapshot_id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL,
  run_json BLOB NOT NULL,
  event_count INTEGER NOT NULL CHECK(event_count >= 0),
  FOREIGN KEY(source_id, snapshot_id) REFERENCES snapshots(source_id, id)
);
CREATE TABLE snapshot_events (
  snapshot_id TEXT NOT NULL REFERENCES snapshots(id),
  sequence INTEGER NOT NULL CHECK(sequence > 0),
  id TEXT NOT NULL,
  kind TEXT NOT NULL,
  search_text TEXT NOT NULL,
  event_json BLOB NOT NULL,
  PRIMARY KEY(snapshot_id, sequence),
  UNIQUE(snapshot_id, id)
);
CREATE INDEX snapshot_events_kind ON snapshot_events(snapshot_id, kind, sequence);
CREATE TABLE directories (
  id TEXT PRIMARY KEY,
  path TEXT NOT NULL UNIQUE
);`, `ALTER TABLE sources ADD COLUMN update_allowed INTEGER NOT NULL DEFAULT 1 CHECK(update_allowed IN (0,1));`, `
CREATE TABLE source_checkpoints (
 source_id TEXT PRIMARY KEY REFERENCES sources(id),
 snapshot_id TEXT NOT NULL,
 checkpoint_json BLOB NOT NULL CHECK(length(checkpoint_json) <= 1048576),
 checksum TEXT NOT NULL,
 FOREIGN KEY(source_id, snapshot_id) REFERENCES snapshots(source_id, id)
);`, `
CREATE TABLE event_contents (
 snapshot_id TEXT NOT NULL,
 event_id TEXT NOT NULL,
 body BLOB NOT NULL CHECK(length(body) <= 16777216),
 PRIMARY KEY(snapshot_id,event_id),
 FOREIGN KEY(snapshot_id,event_id) REFERENCES snapshot_events(snapshot_id,id)
);`, `
CREATE TABLE annotations (
 source_id TEXT NOT NULL,
 snapshot_id TEXT NOT NULL,
 target_key TEXT NOT NULL,
 event_id TEXT,
 judgement TEXT NOT NULL CHECK(judgement IN ('accepted','rejected','uncertain')),
 note TEXT NOT NULL CHECK(length(CAST(note AS BLOB)) <= 4096),
 revision INTEGER NOT NULL CHECK(revision > 0),
 updated_at TEXT NOT NULL,
 deleted INTEGER NOT NULL CHECK(deleted IN (0,1)),
 PRIMARY KEY(snapshot_id,target_key),
 CHECK(target_key=coalesce(event_id,'')),
 FOREIGN KEY(source_id,snapshot_id) REFERENCES snapshots(source_id,id),
 FOREIGN KEY(snapshot_id,event_id) REFERENCES snapshot_events(snapshot_id,id)
);`}

type Store struct {
	db   *sql.DB
	path string
}

// ErrExchangeConflict prevents an imported bundle from moving an already
// registered source back to a different (possibly older) current snapshot.
var ErrExchangeConflict = errors.New("record source already has a different current snapshot")

// Open requires a normal absolute filename; it neither creates parent directories
// nor accepts SQLite URI options from callers. The pool is limited to one
// connection; DSN pragmas are reapplied if the driver replaces that connection.
func Open(ctx context.Context, path string) (*Store, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "?#") {
		return nil, errors.New("database requires an absolute path without ? or #")
	}
	db, err := sql.Open("sqlite", filepath.ToSlash(path)+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(1000)&_pragma=synchronous(FULL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, path: filepath.Clean(path)}
	if err = s.migrate(ctx, migrations); err == nil {
		var mode string
		err = db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode)
		if err == nil && mode != "wal" {
			err = fmt.Errorf("unexpected journal mode: %s", mode)
		}
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open storage: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// All pending DDL and the schema marker commit together. Unknown newer schemas
// fail closed; never delete/recreate a database to hide a migration error.
func (s *Store) migrate(ctx context.Context, steps []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(steps) {
		return fmt.Errorf("database schema %d is newer than supported %d", version, len(steps))
	}
	for i := version; i < len(steps); i++ {
		if _, err = tx.ExecContext(ctx, steps[i]); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if i == 1 {
			if err = backfill(ctx, tx); err != nil {
				return fmt.Errorf("migration 2: %w", err)
			}
		}
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", len(steps))); err != nil {
		return err
	}
	return tx.Commit()
}

// Save publishes an immutable preview snapshot and any supplied full bodies. Retaining
// previous versions ensures old evidence never silently points at new content.
func (s *Store) Save(ctx context.Context, run model.Run, events []model.Event) error {
	return s.save(ctx, run, events, false, nil)
}

// SaveExchange records a detached snapshot. Its embedded source location is
// evidence, never authorization to read that location on this computer.
// An identical current snapshot preserves the existing update permission.
func (s *Store) SaveExchange(ctx context.Context, run model.Run, events []model.Event) error {
	return s.save(ctx, run, events, true, nil)
}

func (s *Store) save(ctx context.Context, run model.Run, events []model.Event, detached bool, checkpoint *codex.Checkpoint) error {
	// Update permission is local source configuration, not immutable data.
	run.ReadOnly = false
	if err := validate(run, events); err != nil {
		return err
	}
	data, err := json.Marshal(model.Record{SchemaVersion: 1, Runs: []model.Run{run}, Events: events})
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	info := run.ImportInfo
	if detached {
		var current string
		if err := tx.QueryRowContext(ctx, "SELECT current_snapshot_id FROM sources WHERE id=?", run.ID).Scan(&current); err == nil {
			if current != info.SnapshotID {
				return ErrExchangeConflict
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if err = checkCapacity(ctx, tx, run.ID, run.EventCount); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO snapshots(id,source_id,record_json) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING`, info.SnapshotID, run.ID, data)
	if err != nil {
		return err
	}
	// A code change without an adapter version bump must not rewrite history.
	var saved []byte
	if err = tx.QueryRowContext(ctx, "SELECT record_json FROM snapshots WHERE id=?", info.SnapshotID).Scan(&saved); err != nil {
		return err
	}
	if string(saved) != string(data) {
		return errors.New("snapshot identity conflict: normalization changed without a version change")
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted != 0 {
		if err = writeIndex(ctx, tx, run, events); err != nil {
			return err
		}
	}
	if !detached {
		if err = saveContents(ctx, tx, run, events); err != nil {
			return err
		}
	}
	query := `INSERT INTO sources(id,current_snapshot_id,update_allowed) VALUES(?,?,1) ON CONFLICT(id) DO UPDATE SET current_snapshot_id=excluded.current_snapshot_id,update_allowed=1`
	if detached {
		query = `INSERT INTO sources(id,current_snapshot_id,update_allowed) VALUES(?,?,0) ON CONFLICT(id) DO UPDATE SET current_snapshot_id=excluded.current_snapshot_id`
	}
	if _, err = tx.ExecContext(ctx, query, run.ID, info.SnapshotID); err != nil {
		return err
	}
	if !detached {
		if err = saveCheckpoint(ctx, tx, run, checkpoint); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Current(ctx context.Context, sourceID string) (model.Record, error) {
	return s.read(ctx, `SELECT record_json,snapshots.source_id,snapshots.id FROM snapshots JOIN sources ON snapshots.id=sources.current_snapshot_id AND snapshots.source_id=sources.id WHERE sources.id=?`, sourceID)
}

func (s *Store) Snapshot(ctx context.Context, snapshotID string) (model.Record, error) {
	return s.read(ctx, "SELECT record_json,source_id,id FROM snapshots WHERE id=?", snapshotID)
}

func (s *Store) read(ctx context.Context, query, id string) (model.Record, error) {
	var data []byte
	var sourceID, snapshotID string
	var record model.Record
	if err := s.db.QueryRowContext(ctx, query, id).Scan(&data, &sourceID, &snapshotID); err != nil {
		return record, err
	}
	record, err := decodeRecord(data)
	if err != nil {
		return model.Record{}, err
	}
	if record.Runs[0].ID != sourceID || record.Runs[0].ImportInfo.SnapshotID != snapshotID {
		return model.Record{}, errors.New("saved snapshot key mismatch")
	}
	return record, nil
}

func validate(run model.Run, events []model.Event) error {
	if err := validateRun(run); err != nil {
		return err
	}
	if run.EventCount != len(events) {
		return errors.New("event count mismatch")
	}
	ids := make(map[string]bool, len(events))
	for n, e := range events {
		if err := validateEvent(run, e, n+1); err != nil || ids[e.ID] {
			return errors.New("invalid event identity or evidence")
		}
		ids[e.ID] = true
	}
	for _, e := range events {
		if e.ParentID != nil && !ids[*e.ParentID] {
			return errors.New("parent event is outside snapshot")
		}
	}
	return nil
}

func validateRun(run model.Run) error {
	if run.ReadOnly {
		return errors.New("local update permission must not be stored in a snapshot")
	}
	i := run.ImportInfo
	if run.Demo || run.Source != "Codex" || run.ID == "" || i == nil || i.AdapterVersion == "" || i.File == "" {
		return errors.New("storage requires a normalized Codex snapshot")
	}
	digest, err := hex.DecodeString(i.SHA256)
	if err != nil || len(digest) != 32 || i.SnapshotID != model.SnapshotID(run.ID, i.SHA256, i.AdapterVersion) {
		return errors.New("invalid snapshot identity")
	}
	if run.EventCount < 0 || run.EventCount > maxSnapshotEvents {
		return errors.New("snapshot event limit exceeded")
	}
	return nil
}

func validateEvent(run model.Run, e model.Event, sequence int) error {
	i := run.ImportInfo
	if e.ID == "" || e.RunID != run.ID || e.Sequence != sequence || sequence < 1 || sequence > run.EventCount || e.Evidence.SourceID != run.ID || e.Evidence.SnapshotID != i.SnapshotID || e.Evidence.Line < 1 || e.Evidence.Location != i.File {
		return errors.New("invalid event identity or evidence")
	}
	return nil
}

func decodeRecord(data []byte) (model.Record, error) {
	var record model.Record
	if err := json.Unmarshal(data, &record); err != nil {
		return model.Record{}, fmt.Errorf("invalid saved snapshot: %w", err)
	}
	if record.SchemaVersion != 1 || len(record.Runs) != 1 {
		return model.Record{}, errors.New("unsupported saved record")
	}
	if err := validate(record.Runs[0], record.Events); err != nil {
		return model.Record{}, err
	}
	return record, nil
}
