package codex

import (
	"jeval/engine/internal/model"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotIdentityBindsSourceBytesAndAdapter(t *testing.T) {
	data, err := os.ReadFile("../../../../fixtures/adapters/codex/classic.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := input(t, data)
	first, events, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.ImportInfo.AdapterVersion != AdapterVersion || first.ImportInfo.SnapshotID == "" {
		t.Fatal("missing snapshot metadata")
	}
	for _, e := range events {
		if e.Evidence.SnapshotID != first.ImportInfo.SnapshotID {
			t.Fatal("unbound event evidence")
		}
	}
	again, _, err := Read(path)
	if err != nil || again.ImportInfo.SnapshotID != first.ImportInfo.SnapshotID {
		t.Fatal("unstable snapshot", err)
	}
	copyPath := filepath.Join(t.TempDir(), "copy.jsonl")
	if err = os.WriteFile(copyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	copyRun, _, err := Read(copyPath)
	if err != nil || copyRun.ImportInfo.SnapshotID == first.ImportInfo.SnapshotID {
		t.Fatal("merged distinct sources", err)
	}
	// Even a non-displayed byte change versions the evidence snapshot.
	if err = os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	changed, _, err := Read(path)
	if err != nil || changed.ID != first.ID || changed.ImportInfo.SnapshotID == first.ImportInfo.SnapshotID {
		t.Fatal("source update did not version snapshot", err)
	}
	i := first.ImportInfo
	if model.SnapshotID(first.ID, i.SHA256, "next-adapter") == i.SnapshotID {
		t.Fatal("adapter change did not version snapshot")
	}
}
