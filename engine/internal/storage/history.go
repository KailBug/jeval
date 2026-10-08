package storage

import (
	"context"
	"errors"
	"jeval/engine/internal/model"
)

func (s *Store) SnapshotRun(ctx context.Context, runID, snapshotID string) (model.Run, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT run_json FROM snapshot_runs WHERE source_id=? AND snapshot_id=?`, runID, snapshotID).Scan(&data)
	if err != nil {
		return model.Run{}, err
	}
	run, err := decodeRun(data)
	if err != nil {
		return model.Run{}, err
	}
	if run.ID != runID || run.ImportInfo.SnapshotID != snapshotID {
		return model.Run{}, errors.New("snapshot key mismatch")
	}
	return run, nil
}

func (s *Store) Snapshots(ctx context.Context, runID string, offset, limit int) ([]model.Run, int, error) {
	if offset < 0 || offset > 1000000000 || limit < 1 || limit > 50 {
		return nil, 0, errors.New("invalid history page")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM snapshot_runs WHERE source_id=?`, runID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.run_json FROM snapshot_runs r JOIN snapshots s ON s.id=r.snapshot_id WHERE r.source_id=? ORDER BY s.rowid DESC LIMIT ? OFFSET ?`, runID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []model.Run{}
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			return nil, 0, err
		}
		run, e := decodeRun(data)
		if e != nil {
			return nil, 0, e
		}
		if run.ID != runID {
			return nil, 0, errors.New("snapshot source mismatch")
		}
		items = append(items, run)
	}
	return items, total, rows.Err()
}
