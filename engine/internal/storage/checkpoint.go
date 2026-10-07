package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"jeval/engine/internal/adapters/codex"
	"jeval/engine/internal/model"
)

func (s *Store) SaveCheckpoint(ctx context.Context, run model.Run, events []model.Event, cp *codex.Checkpoint) error {
	return s.save(ctx, run, events, false, cp)
}

func saveCheckpoint(ctx context.Context, tx *sql.Tx, run model.Run, cp *codex.Checkpoint) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM source_checkpoints WHERE source_id=?", run.ID); err != nil {
		return err
	}
	if cp == nil {
		return nil
	}
	if cp.Version != codex.CheckpointVersion || cp.SourceID != run.ID || cp.SnapshotID != run.ImportInfo.SnapshotID || cp.SHA256 != run.ImportInfo.SHA256 || cp.AdapterVersion != run.ImportInfo.AdapterVersion || cp.Offset < 1 || cp.Offset > codex.MaxFileBytes || cp.Lines < 0 || cp.Lines >= 50000 {
		return errors.New("checkpoint does not match snapshot")
	}
	data, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	if len(data) > codex.MaxCheckpointBytes {
		return errors.New("checkpoint exceeds metadata limit")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO source_checkpoints(source_id,snapshot_id,checkpoint_json,checksum) VALUES(?,?,?,?)", run.ID, cp.SnapshotID, data, fmt.Sprintf("%x", sha256.Sum256(data)))
	return err
}

// Invalid cache metadata triggers a full parse, not a broken library. Actual
// database read errors still surface rather than silently discarding evidence.
func (s *Store) Checkpoint(ctx context.Context, sourceID string) (*codex.Checkpoint, error) {
	var data []byte
	var checksum, snapshot string
	err := s.db.QueryRowContext(ctx, `SELECT checkpoint_json,checksum,snapshot_id FROM source_checkpoints c JOIN sources s ON s.id=c.source_id AND s.current_snapshot_id=c.snapshot_id WHERE s.id=? AND s.update_allowed=1`, sourceID).Scan(&data, &checksum, &snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cp codex.Checkpoint
	if len(data) > codex.MaxCheckpointBytes || fmt.Sprintf("%x", sha256.Sum256(data)) != checksum || json.Unmarshal(data, &cp) != nil || cp.SourceID != sourceID || cp.SnapshotID != snapshot {
		return &codex.Checkpoint{Version: -1}, nil
	}
	return &cp, nil
}
