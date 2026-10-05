package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type protectedExportPath struct {
	path  string
	local bool
}

// CheckExportPath protects the library and registered source files before an
// explicitly selected destination is written. No source content is opened.
// Existing hard links and filesystem path aliases are compared as well as names.
func (s *Store) CheckExportPath(ctx context.Context, target string) error {
	if !filepath.IsAbs(target) || len(target) > 2048 {
		return errors.New("export requires an absolute path of at most 2048 bytes")
	}
	targetIdentity, err := exportIdentity(target)
	if err != nil {
		return err
	}
	targetInfo, err := os.Stat(target)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	protected := []protectedExportPath{{s.path, true}, {s.path + "-wal", true}, {s.path + "-shm", true}, {s.path + "-journal", true}}
	runs, err := s.Runs(ctx)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.ImportInfo != nil {
			protected = append(protected, protectedExportPath{run.ImportInfo.File, !run.ReadOnly})
		}
	}
	for _, protectedPath := range protected {
		path := protectedPath.path
		// Detached locators are evidence text from another machine. Never
		// resolve or stat them, even when they resemble a local or UNC path.
		if !protectedPath.local {
			identity := normalizedPath(path)
			if identity == normalizedPath(target) || identity == targetIdentity {
				return errors.New("export destination matches the record's original source locator")
			}
			continue
		}
		if !filepath.IsAbs(path) {
			continue
		}
		identity, identityErr := exportIdentity(path)
		if identityErr != nil {
			// An offline source may have a missing parent. Its recorded path
			// still remains protected; its availability never blocks export.
			if !os.IsNotExist(identityErr) {
				return identityErr
			}
			identity = normalizedPath(path)
		}
		if identity == targetIdentity {
			return errors.New("export destination is a registered source or library database")
		}
		// Windows EvalSymlinks may preserve a junction spelling. Compare the
		// parent directories too, including sidecars that don't yet exist.
		if normalizedPath(filepath.Base(path)) == normalizedPath(filepath.Base(target)) {
			targetParent, targetErr := os.Stat(filepath.Dir(target))
			protectedParent, protectedErr := os.Stat(filepath.Dir(path))
			if targetErr == nil && protectedErr == nil && os.SameFile(targetParent, protectedParent) {
				return errors.New("export destination aliases a registered source or library database")
			}
		}
		if targetInfo != nil {
			info, statErr := os.Stat(path)
			if statErr == nil && os.SameFile(targetInfo, info) {
				return errors.New("export destination aliases a registered source or library database")
			}
			if statErr != nil && !os.IsNotExist(statErr) {
				return fmt.Errorf("check protected export path: %w", statErr)
			}
		}
	}
	return ctx.Err()
}

func exportIdentity(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil && os.IsNotExist(err) {
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(path))
		if parentErr != nil {
			return "", parentErr
		}
		resolved, err = filepath.Join(parent, filepath.Base(path)), nil
	}
	if err != nil {
		return "", err
	}
	return normalizedPath(resolved), nil
}

func normalizedPath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
