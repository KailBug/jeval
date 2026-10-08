package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"jeval/engine/internal/model"
)

const MaxAnnotationBytes = model.MaxAnnotationNoteBytes
const MaxAnnotationTargets = 100000

var ErrAnnotationConflict = errors.New("annotation changed; reload before saving")
var ErrAnnotationLimit = errors.New("annotation target limit exceeded")
var ErrAnnotationInput = errors.New("invalid annotation: judgement, UTF-8 note or revision")

func targetKey(eventID *string) string {
	if eventID == nil {
		return ""
	}
	return *eventID
}

func annotationTarget(ctx context.Context, tx *sql.Tx, runID, snapshotID string, eventID *string) (*model.Event, error) {
	var data []byte
	if err := tx.QueryRowContext(ctx, `SELECT run_json FROM snapshot_runs WHERE source_id=? AND snapshot_id=?`, runID, snapshotID).Scan(&data); err != nil {
		return nil, err
	}
	run, err := decodeRun(data)
	if err != nil {
		return nil, err
	}
	if run.ID != runID || run.ImportInfo.SnapshotID != snapshotID {
		return nil, errors.New("snapshot key mismatch")
	}
	if eventID == nil {
		return nil, nil
	}
	if *eventID == "" {
		return nil, ErrAnnotationInput
	}
	var sequence int
	if err = tx.QueryRowContext(ctx, `SELECT sequence,event_json FROM snapshot_events WHERE snapshot_id=? AND id=?`, snapshotID, *eventID).Scan(&sequence, &data); err != nil {
		return nil, err
	}
	var event model.Event
	if err = json.Unmarshal(data, &event); err != nil {
		return nil, err
	}
	if err = validateEvent(run, event, sequence); err != nil {
		return nil, err
	}
	return &event, nil
}

func readAnnotation(ctx context.Context, tx *sql.Tx, runID, snapshotID string, eventID *string) (*model.Annotation, error) {
	event, err := annotationTarget(ctx, tx, runID, snapshotID, eventID)
	if err != nil {
		return nil, err
	}
	a := &model.Annotation{RunID: runID, SnapshotID: snapshotID, EventID: eventID, Event: event}
	err = tx.QueryRowContext(ctx, `SELECT judgement,note,revision,updated_at,deleted FROM annotations WHERE snapshot_id=? AND target_key=?`, snapshotID, targetKey(eventID)).Scan(&a.Judgement, &a.Note, &a.Revision, &a.UpdatedAt, &a.Deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (s *Store) Annotation(ctx context.Context, runID, snapshotID string, eventID *string) (*model.Annotation, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	a, err := readAnnotation(ctx, tx, runID, snapshotID, eventID)
	if err != nil {
		return nil, err
	}
	return a, tx.Commit()
}

// Revision checks and the write share a transaction. Tombstones retain revisions
// so a deleted/recreated note cannot accept an old editor's stale revision.
func (s *Store) SaveAnnotation(ctx context.Context, runID, snapshotID string, eventID *string, judgement, note string, expected int, deleted bool) (*model.Annotation, error) {
	if expected < 0 || expected > 1000000000 || len(note) > MaxAnnotationBytes || !utf8.ValidString(note) || strings.ContainsRune(note, 0) || (judgement != "accepted" && judgement != "rejected" && judgement != "uncertain") {
		return nil, ErrAnnotationInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	old, err := readAnnotation(ctx, tx, runID, snapshotID, eventID)
	if err != nil {
		return nil, err
	}
	revision := 0
	if old != nil {
		revision = old.Revision
	}
	if revision != expected {
		return nil, ErrAnnotationConflict
	}
	if deleted && (old == nil || old.Deleted) {
		return nil, ErrAnnotationConflict
	}
	if old == nil {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM annotations`).Scan(&count); err != nil {
			return nil, err
		}
		if count >= MaxAnnotationTargets {
			return nil, ErrAnnotationLimit
		}
	}
	if deleted {
		note = ""
		judgement = "uncertain"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO annotations(source_id,snapshot_id,target_key,event_id,judgement,note,revision,updated_at,deleted) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(snapshot_id,target_key) DO UPDATE SET judgement=excluded.judgement,note=excluded.note,revision=excluded.revision,updated_at=excluded.updated_at,deleted=excluded.deleted`, runID, snapshotID, targetKey(eventID), eventID, judgement, note, revision+1, time.Now().UTC().Format(time.RFC3339Nano), deleted)
	if err != nil {
		return nil, err
	}
	a, err := readAnnotation(ctx, tx, runID, snapshotID, eventID)
	if err != nil {
		return nil, err
	}
	return a, tx.Commit()
}

func (s *Store) Annotations(ctx context.Context, runID, snapshotID string, offset, limit int) ([]model.Annotation, int, error) {
	if offset < 0 || offset > 1000000000 || limit < 1 || limit > 50 {
		return nil, 0, ErrAnnotationInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	if _, err = annotationTarget(ctx, tx, runID, snapshotID, nil); err != nil {
		return nil, 0, err
	}
	var total int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM annotations WHERE snapshot_id=? AND deleted=0`, snapshotID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT event_id FROM annotations WHERE snapshot_id=? AND deleted=0 ORDER BY target_key LIMIT ? OFFSET ?`, snapshotID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	ids := []*string{}
	for rows.Next() {
		var id *string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	items := []model.Annotation{}
	for _, id := range ids {
		a, e := readAnnotation(ctx, tx, runID, snapshotID, id)
		if e != nil {
			return nil, 0, e
		}
		if a == nil {
			return nil, 0, errors.New("missing annotation")
		}
		items = append(items, *a)
	}
	return items, total, tx.Commit()
}
