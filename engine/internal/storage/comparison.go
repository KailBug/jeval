package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"jeval/engine/internal/model"
)

func comparisonSide(ctx context.Context, tx *sql.Tx, ref model.SnapshotRef) (model.ComparisonSide, error) {
	var side model.ComparisonSide
	var data []byte
	if err := tx.QueryRowContext(ctx, `SELECT record_json FROM snapshots WHERE source_id=? AND id=?`, ref.RunID, ref.SnapshotID).Scan(&data); err != nil {
		return side, err
	}
	record, err := decodeRecord(data)
	if err != nil {
		return side, err
	}
	if record.Runs[0].ID != ref.RunID || record.Runs[0].ImportInfo.SnapshotID != ref.SnapshotID {
		return side, errors.New("comparison snapshot key mismatch")
	}
	side.Record = record
	side.Annotations = []model.Annotation{}
	rows, err := tx.QueryContext(ctx, `SELECT event_id,judgement,note,revision,updated_at FROM annotations WHERE source_id=? AND snapshot_id=? AND deleted=0 ORDER BY target_key`, ref.RunID, ref.SnapshotID)
	if err != nil {
		return side, err
	}
	defer rows.Close()
	for rows.Next() {
		a := model.Annotation{RunID: ref.RunID, SnapshotID: ref.SnapshotID}
		if err = rows.Scan(&a.EventID, &a.Judgement, &a.Note, &a.Revision, &a.UpdatedAt); err != nil {
			return side, err
		}
		side.Annotations = append(side.Annotations, a)
	}
	return side, rows.Err()
}

// Both immutable previews and mutable human notes come from one read transaction.
// The export never follows current pointers or accesses original source files.
func (s *Store) Comparison(ctx context.Context, left, right model.SnapshotRef) (model.Comparison, error) {
	report := model.Comparison{Format: "jeval-comparison", FormatVersion: 1, ContentScope: "normalized-preview", Metrics: map[string]model.MetricDifference{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	if report.Left, err = comparisonSide(ctx, tx, left); err != nil {
		return report, err
	}
	if report.Right, err = comparisonSide(ctx, tx, right); err != nil {
		return report, err
	}
	l, r := report.Left.Record.Runs[0], report.Right.Record.Runs[0]
	lc, rc := int64(l.EventCount), int64(r.EventCount)
	report.Metrics["durationMs"] = model.Difference(l.DurationMs, r.DurationMs)
	report.Metrics["tokens"] = model.Difference(l.Tokens, r.Tokens)
	report.Metrics["eventCount"] = model.Difference(&lc, &rc)
	if err = tx.Commit(); err != nil {
		return report, err
	}
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return report, nil
}
