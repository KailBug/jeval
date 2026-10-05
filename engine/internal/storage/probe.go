package storage

import (
	"context"
	"fmt"
	"jeval/engine/internal/model"
	"os"
	"path/filepath"
	"reflect"
)

// Probe uses only synthetic data in a freshly created directory. It can run from
// the distributed engine without Go, Node or any external SQLite DLL.
func Probe(ctx context.Context, parent string) (err error) {
	if !filepath.IsAbs(parent) {
		return fmt.Errorf("storage check requires an absolute directory")
	}
	dir, err := os.MkdirTemp(parent, "jeval-storage-check-")
	if err != nil {
		return err
	}
	defer func() {
		if cleanup := os.RemoveAll(dir); err == nil {
			err = cleanup
		}
	}()
	path := filepath.Join(dir, "中文 snapshot.sqlite")
	s, err := Open(ctx, path)
	if err != nil {
		return err
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	info := &model.ImportInfo{File: "synthetic:storage-check", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AdapterVersion: "storage-probe-v1", Warnings: []model.ImportWarning{}}
	run := model.Run{ID: "synthetic-storage-probe", Source: "Codex", Title: "合成存储检查", Status: "unknown", EventCount: 1, ImportInfo: info}
	info.SnapshotID = model.SnapshotID(run.ID, info.SHA256, info.AdapterVersion)
	events := []model.Event{{ID: "probe-event", RunID: run.ID, Sequence: 1, Kind: "message", Role: "user", Title: "合成消息", Content: "Synthetic preview", Evidence: model.EvidenceRef{SourceID: run.ID, SnapshotID: info.SnapshotID, Location: info.File, Line: 1}}}
	if err = s.Save(ctx, run, events); err != nil {
		return err
	}
	directory, err := s.SaveDirectory(ctx, dir)
	if err != nil {
		return err
	}
	if err = s.Close(); err != nil {
		return err
	}
	s, err = Open(ctx, path)
	if err != nil {
		return err
	}
	got, err := s.Current(ctx, run.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(got.Runs, []model.Run{run}) || !reflect.DeepEqual(got.Events, events) {
		return fmt.Errorf("storage reopen mismatch")
	}
	runs, err := s.Runs(ctx)
	if err != nil {
		return err
	}
	page, total, err := s.Events(ctx, run.ID, "合成", "message", 0, 1)
	if err != nil {
		return err
	}
	directories, err := s.Directories(ctx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(runs, []model.Run{run}) || total != 1 || !reflect.DeepEqual(page, events) || !reflect.DeepEqual(directories, []Directory{directory}) {
		return fmt.Errorf("storage index or directory reopen mismatch")
	}
	var integrity string
	if err = s.db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("storage integrity: %s", integrity)
	}
	return nil
}
