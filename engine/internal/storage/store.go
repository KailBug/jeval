// Package storage is the A-slice SQLite validation backend. Desktop imports
// still use memory until the B-slice wires application data and bounded queries.
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

	_ "modernc.org/sqlite"
)

const schemaVersion = 1

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
);`}

type Store struct{ db *sql.DB }

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
	s := &Store{db: db}
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
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", len(steps))); err != nil {
		return err
	}
	return tx.Commit()
}

// Save publishes one immutable, preview-only normalized snapshot. Retaining
// previous versions ensures old evidence never silently points at new content.
func (s *Store) Save(ctx context.Context, run model.Run, events []model.Event) error {
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
	if _, err = tx.ExecContext(ctx, `INSERT INTO snapshots(id,source_id,record_json) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING`, info.SnapshotID, run.ID, data); err != nil {
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
	if _, err = tx.ExecContext(ctx, `INSERT INTO sources(id,current_snapshot_id) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET current_snapshot_id=excluded.current_snapshot_id`, run.ID, info.SnapshotID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Current(ctx context.Context, sourceID string) (model.Record, error) {
	return s.read(ctx, `SELECT record_json FROM snapshots JOIN sources ON snapshots.id=sources.current_snapshot_id WHERE sources.id=?`, sourceID)
}

func (s *Store) Snapshot(ctx context.Context, snapshotID string) (model.Record, error) {
	return s.read(ctx, "SELECT record_json FROM snapshots WHERE id=?", snapshotID)
}

func (s *Store) read(ctx context.Context, query, id string) (model.Record, error) {
	var data []byte
	var record model.Record
	if err := s.db.QueryRowContext(ctx, query, id).Scan(&data); err != nil {
		return record, err
	}
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

func validate(run model.Run, events []model.Event) error {
	i := run.ImportInfo
	if run.Demo || run.Source != "Codex" || run.ID == "" || i == nil || i.AdapterVersion == "" || i.File == "" {
		return errors.New("storage requires a normalized Codex snapshot")
	}
	digest, err := hex.DecodeString(i.SHA256)
	if err != nil || len(digest) != 32 || i.SnapshotID != model.SnapshotID(run.ID, i.SHA256, i.AdapterVersion) {
		return errors.New("invalid snapshot identity")
	}
	if run.EventCount != len(events) {
		return errors.New("event count mismatch")
	}
	ids := make(map[string]bool, len(events))
	for n, e := range events {
		if e.ID == "" || ids[e.ID] || e.RunID != run.ID || e.Sequence != n+1 || e.Evidence.SourceID != run.ID || e.Evidence.SnapshotID != i.SnapshotID || e.Evidence.Line < 1 || e.Evidence.Location != i.File {
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
