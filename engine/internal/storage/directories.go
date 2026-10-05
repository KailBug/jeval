package storage

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Directory struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

const maxDirectories = 20
const maxDirectoryPathBytes = 2048

// SaveDirectory registers an explicitly chosen directory. It only resolves and
// validates that root; saving configuration never scans or imports its files.
func (s *Store) SaveDirectory(ctx context.Context, path string) (Directory, error) {
	if err := ctx.Err(); err != nil {
		return Directory{}, err
	}
	if !filepath.IsAbs(path) || len(path) > maxDirectoryPathBytes {
		return Directory{}, errors.New("source directory requires an absolute path of at most 2048 bytes")
	}
	canonical, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return Directory{}, err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return Directory{}, err
	}
	if !info.IsDir() {
		return Directory{}, errors.New("source path is not a directory")
	}
	if len(canonical) > maxDirectoryPathBytes {
		return Directory{}, errors.New("resolved source directory path exceeds 2048 bytes")
	}
	directory := Directory{ID: directoryID(canonical), Path: canonical}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Directory{}, err
	}
	defer tx.Rollback()
	var others int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM directories WHERE id<>?", directory.ID).Scan(&others); err != nil {
		return Directory{}, err
	}
	if others >= maxDirectories {
		return Directory{}, errors.New("at most 20 source directories can be saved")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO directories(id,path) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET path=excluded.path`, directory.ID, directory.Path); err != nil {
		return Directory{}, err
	}
	if err = tx.Commit(); err != nil {
		return Directory{}, err
	}
	return directory, nil
}

// Directories returns saved roots even when they are currently offline.
func (s *Store) Directories(ctx context.Context) ([]Directory, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,path FROM directories ORDER BY rowid LIMIT ?", maxDirectories+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	directories := []Directory{}
	for rows.Next() {
		var directory Directory
		if err = rows.Scan(&directory.ID, &directory.Path); err != nil {
			return nil, err
		}
		directories = append(directories, directory)
		if len(directories) > maxDirectories || directory.ID != directoryID(directory.Path) || len(directory.Path) > maxDirectoryPathBytes || !filepath.IsAbs(directory.Path) {
			return nil, errors.New("invalid saved source directory configuration")
		}
	}
	return directories, rows.Err()
}

func directoryID(path string) string {
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return fmt.Sprintf("directory-%x", sha256.Sum256([]byte(path)))
}

// RemoveDirectory forgets configuration only. Imported snapshots and original
// source files are independent and are never deleted by this operation.
func (s *Store) RemoveDirectory(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("source directory ID is required")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM directories WHERE id=?", id)
	return err
}
