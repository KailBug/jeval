package storage

import (
	"context"
	"database/sql"
	"errors"
	"unicode/utf8"

	"jeval/engine/internal/model"
)

const MaxBodyBytes = 16 * 1024 * 1024
const MaxSnapshotBodyBytes = 32 * 1024 * 1024
const ContentPageBytes = 32 * 1024

var ErrContentOffset = errors.New("invalid content byte offset")

type ContentPage struct {
	SnapshotID string `json:"snapshotId"`
	EventID    string `json:"eventId"`
	Available  bool   `json:"available"`
	Content    string `json:"content"`
	Offset     int    `json:"offset"`
	TotalBytes int    `json:"totalBytes"`
	NextOffset *int   `json:"nextOffset"`
}

func saveContents(ctx context.Context, tx *sql.Tx, run model.Run, events []model.Event) error {
	total := 0
	for _, e := range events {
		if e.FullContent == nil {
			continue
		}
		body := *e.FullContent
		total += len(body)
		if len(body) > MaxBodyBytes || total > MaxSnapshotBodyBytes || !utf8.ValidString(body) {
			return errors.New("full content exceeds limits or is not UTF-8")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO event_contents(snapshot_id,event_id,body) VALUES(?,?,?) ON CONFLICT DO NOTHING`, run.ImportInfo.SnapshotID, e.ID, []byte(body)); err != nil {
			return err
		}
		var same bool
		if err := tx.QueryRowContext(ctx, `SELECT body=? FROM event_contents WHERE snapshot_id=? AND event_id=?`, []byte(body), run.ImportInfo.SnapshotID, e.ID).Scan(&same); err != nil {
			return err
		}
		if !same {
			return errors.New("immutable event content conflict")
		}
	}
	// Include earlier supplements when only some bodies were supplied this time.
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(length(body)),0) FROM event_contents WHERE snapshot_id=?`, run.ImportInfo.SnapshotID).Scan(&total); err != nil {
		return err
	}
	if total > MaxSnapshotBodyBytes {
		return errors.New("snapshot full content exceeds 32 MiB")
	}
	return nil
}

// RestoreContents is used only for a requested update, never during startup,
// timeline queries or export. A missing body forces a full source reparse.
func (s *Store) RestoreContents(ctx context.Context, record *model.Record) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,body FROM event_contents WHERE snapshot_id=?`, record.Runs[0].ImportInfo.SnapshotID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	bodies := make(map[string]string)
	total := 0
	for rows.Next() {
		var id, body string
		if err = rows.Scan(&id, &body); err != nil {
			return false, err
		}
		total += len(body)
		if len(body) > MaxBodyBytes || total > MaxSnapshotBodyBytes || !utf8.ValidString(body) {
			return false, errors.New("invalid saved full content")
		}
		bodies[id] = body
	}
	if err = rows.Err(); err != nil {
		return false, err
	}
	for i := range record.Events {
		body, ok := bodies[record.Events[i].ID]
		if !ok {
			return false, nil
		}
		record.Events[i].FullContent = &body
	}
	return true, nil
}

// SQLite slices the BLOB before returning it, so a page does not materialize a
// large body in Go. Clients retain the exact snapshot and follow NextOffset.
func (s *Store) EventContent(ctx context.Context, runID, snapshotID, eventID string, offset int) (ContentPage, error) {
	p := ContentPage{SnapshotID: snapshotID, EventID: eventID, Offset: offset}
	if offset < 0 || offset > MaxBodyBytes {
		return p, ErrContentOffset
	}
	var size sql.NullInt64
	var body []byte
	err := s.db.QueryRowContext(ctx, `SELECT length(c.body),substr(c.body,?,?) FROM snapshot_events e JOIN snapshots s ON s.id=e.snapshot_id LEFT JOIN event_contents c ON c.snapshot_id=e.snapshot_id AND c.event_id=e.id WHERE s.source_id=? AND e.snapshot_id=? AND e.id=?`, offset+1, ContentPageBytes+4, runID, snapshotID, eventID).Scan(&size, &body)
	if err != nil {
		return p, err
	}
	if !size.Valid {
		if offset != 0 {
			return p, ErrContentOffset
		}
		return p, nil
	}
	if size.Int64 < 0 || size.Int64 > MaxBodyBytes {
		return p, errors.New("invalid saved body size")
	}
	p.Available, p.TotalBytes = true, int(size.Int64)
	if offset > p.TotalBytes || len(body) > 0 && !utf8.RuneStart(body[0]) {
		return p, ErrContentOffset
	}
	end := min(ContentPageBytes, len(body))
	for end < len(body) && end > 0 && !utf8.RuneStart(body[end]) {
		end--
	}
	if !utf8.Valid(body[:end]) {
		return p, errors.New("invalid saved body encoding")
	}
	p.Content = string(body[:end])
	if offset+end < p.TotalBytes {
		next := offset + end
		p.NextOffset = &next
	}
	return p, nil
}
