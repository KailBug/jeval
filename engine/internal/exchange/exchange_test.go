package exchange

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"jeval/engine/internal/model"
)

func fixture(t *testing.T) model.Record {
	t.Helper()
	record, err := Read(filepath.Join("..", "..", "..", "contracts", "exchange", "preview-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func pointer[T any](value T) *T { return &value }

func TestFixtureRoundTripAndMarkdown(t *testing.T) {
	record := fixture(t)
	first, err := Encode(record, "json")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "往返 with spaces.jeval.json")
	if err = Write(path, first); err != nil {
		t.Fatal(err)
	}
	returned, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(record, returned) {
		t.Fatal("round trip changed a record")
	}
	second, err := Encode(returned, "json")
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("JSON export is not deterministic")
	}
	if returned.Runs[0].Tokens != nil || returned.Events[0].Timestamp != nil || returned.Events[0].ParentID != nil {
		t.Fatal("unknown values changed")
	}
	report, err := Encode(record, "markdown")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"normalized-preview", "Original source files are not included", "unknown (null)", record.Runs[0].ImportInfo.SnapshotID, record.Runs[0].ImportInfo.File, record.Events[0].ID, "Physical line: 2", previewMarker} {
		if !bytes.Contains(report, []byte(value)) {
			t.Fatalf("report missing %q", value)
		}
	}
	// The synthetic event attempts to close a fence and inject executable HTML.
	if !bytes.Contains(report, []byte("````text\n"+record.Events[0].Content+"\n````")) {
		t.Fatal("record content escaped its text fence")
	}
	if _, err = Encode(record, "html"); err == nil {
		t.Fatal("unsupported export format accepted")
	}
}

func TestValidateRejectsMalformedRecords(t *testing.T) {
	cases := map[string]func(*model.Record){
		"future record":      func(r *model.Record) { r.SchemaVersion = 2 },
		"multiple runs":      func(r *model.Record) { r.Runs = append(r.Runs, r.Runs[0]) },
		"missing events":     func(r *model.Record) { r.Events = nil },
		"demo":               func(r *model.Record) { r.Runs[0].Demo = true },
		"local permission":   func(r *model.Record) { r.Runs[0].ReadOnly = true },
		"unsupported source": func(r *model.Record) { r.Runs[0].Source = "ATIF" },
		"missing import":     func(r *model.Record) { r.Runs[0].ImportInfo = nil },
		"future adapter":     func(r *model.Record) { r.Runs[0].ImportInfo.AdapterVersion = "codex-rollout-v2" },
		"history mode":       func(r *model.Record) { r.Runs[0].ImportInfo.HistoryMode = "future" },
		"source digest":      func(r *model.Record) { r.Runs[0].ID = "codex-not-a-digest" },
		"snapshot digest":    func(r *model.Record) { r.Runs[0].ImportInfo.SnapshotID = "snapshot-" + strings.Repeat("0", 64) },
		"source SHA":         func(r *model.Record) { r.Runs[0].ImportInfo.SHA256 = strings.Repeat("A", 64) },
		"bad status":         func(r *model.Record) { r.Runs[0].Status = "running" },
		"wrong count":        func(r *model.Record) { r.Runs[0].EventCount++ },
		"huge locator":       func(r *model.Record) { r.Runs[0].ImportInfo.File = strings.Repeat("x", 2049) },
		"empty session":      func(r *model.Record) { r.Runs[0].ImportInfo.SessionID = "" },
		"invalid UTF8":       func(r *model.Record) { r.Runs[0].Title = string([]byte{255}) },
		"huge title":         func(r *model.Record) { r.Runs[0].Title = strings.Repeat("x", 16385) },
		"bad startedAt":      func(r *model.Record) { r.Runs[0].StartedAt = pointer("2026-02-30T00:00:00Z") },
		"bad zone":           func(r *model.Record) { r.Runs[0].StartedAt = pointer("2026-10-05T00:00:00+24:00") },
		"unsafe integer":     func(r *model.Record) { r.Runs[0].Tokens = pointer(maxSafeInteger + 1) },
		"negative metric":    func(r *model.Record) { r.Runs[0].DurationMs = pointer(int64(-1)) },
		"warning array":      func(r *model.Record) { r.Runs[0].ImportInfo.Warnings = nil },
		"warning count":      func(r *model.Record) { r.Runs[0].ImportInfo.WarningCount = -1 },
		"warning line":       func(r *model.Record) { r.Runs[0].ImportInfo.Warnings[0].Line = 0 },
		"event ID":           func(r *model.Record) { r.Events[0].ID = "other" },
		"duplicate event": func(r *model.Record) {
			r.Events[1].ID = r.Events[0].ID
			r.Events[1].Evidence.Line = r.Events[0].Evidence.Line
		},
		"sequence gap":          func(r *model.Record) { r.Events[1].Sequence = 3 },
		"wrong run":             func(r *model.Record) { r.Events[0].RunID = "elsewhere" },
		"evidence source":       func(r *model.Record) { r.Events[0].Evidence.SourceID = "elsewhere" },
		"evidence snapshot":     func(r *model.Record) { r.Events[0].Evidence.SnapshotID = "elsewhere" },
		"evidence locator":      func(r *model.Record) { r.Events[0].Evidence.Location = "elsewhere" },
		"evidence line":         func(r *model.Record) { r.Events[0].Evidence.Line = 0 },
		"bad event kind":        func(r *model.Record) { r.Events[0].Kind = "future" },
		"bad role":              func(r *model.Record) { r.Events[0].Role = "future" },
		"bad event timestamp":   func(r *model.Record) { r.Events[0].Timestamp = pointer("2026-10-05T00:00:00,3Z") },
		"oversized content":     func(r *model.Record) { r.Events[0].Content = strings.Repeat("x", 8193) },
		"oversized marker body": func(r *model.Record) { r.Events[0].Content = strings.Repeat("x", 8193) + previewMarker },
		"outside parent":        func(r *model.Record) { r.Events[0].ParentID = pointer("outside") },
		"self cycle":            func(r *model.Record) { r.Events[0].ParentID = pointer(r.Events[0].ID) },
		"parent cycle": func(r *model.Record) {
			r.Events[0].ParentID = pointer(r.Events[1].ID)
			r.Events[1].ParentID = pointer(r.Events[0].ID)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			record := fixture(t)
			change(&record)
			if err := Validate(record); err == nil {
				t.Fatal("invalid exchange accepted")
			}
		})
	}
	record := fixture(t)
	record.Events[0].Content = strings.Repeat("x", 8192) + previewMarker
	if err := Validate(record); err != nil {
		t.Fatalf("existing bounded preview marker rejected: %v", err)
	}
	record.Events[0].Content = strings.Repeat("界", 2731)
	if err := Validate(record); err == nil {
		t.Fatal("content bound counted characters instead of UTF-8 bytes")
	}
}

func TestReadRejectsInvalidJSONShape(t *testing.T) {
	data, err := Encode(fixture(t), "json")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"duplicate key":          bytes.Replace(data, []byte(`"format": "jeval-record"`), []byte(`"format": "wrong", "format": "jeval-record"`), 1),
		"nested duplicate":       bytes.Replace(data, []byte(`"sequence": 1`), []byte(`"sequence": 8, "sequence": 1`), 1),
		"unknown field":          bytes.Replace(data, []byte(`"format":`), []byte(`"unknown": false, "format":`), 1),
		"wrong casing":           bytes.Replace(data, []byte(`"format":`), []byte(`"Format":`), 1),
		"missing null":           bytes.Replace(data, []byte(`"startedAt": null,`), nil, 1),
		"null boolean":           bytes.Replace(data, []byte(`"demo": false`), []byte(`"demo": null`), 1),
		"null integer":           bytes.Replace(data, []byte(`"eventCount": 2`), []byte(`"eventCount": null`), 1),
		"null string":            bytes.Replace(data, []byte(`"source": "Codex"`), []byte(`"source": null`), 1),
		"local permission false": bytes.Replace(data, []byte(`"demo": false`), []byte(`"demo": false, "readOnly": false`), 1),
		"unsafe integer":         bytes.Replace(data, []byte(`"tokens": null`), []byte(`"tokens": 9007199254740992`), 1),
		"fractional count":       bytes.Replace(data, []byte(`"eventCount": 2`), []byte(`"eventCount": 2.0`), 1),
		"future envelope":        bytes.Replace(data, []byte(`"formatVersion": 1`), []byte(`"formatVersion": 2`), 1),
		"false body scope":       bytes.Replace(data, []byte(`"sourceFilesIncluded": false`), []byte(`"sourceFilesIncluded": true`), 1),
		"trailing JSON":          append(append([]byte{}, data...), []byte(`{}`)...),
		"invalid UTF8":           bytes.Replace(data, []byte(`"jeval-record"`), []byte{'"', 255, '"'}, 1),
		"lone high surrogate":    bytes.Replace(data, []byte(`Synthetic ordinary text`), []byte(`\ud800`), 1),
		"lone low surrogate":     bytes.Replace(data, []byte(`Synthetic ordinary text`), []byte(`\udfff`), 1),
		"two high surrogates":    bytes.Replace(data, []byte(`Synthetic ordinary text`), []byte(`\ud800\ud801`), 1),
		"empty document":         {},
		"top-level null":         []byte(`null`),
	}
	for name, invalid := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.json")
			if err := os.WriteFile(path, invalid, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Read(path); err == nil {
				t.Fatal("invalid JSON accepted")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "oversized.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(MaxFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err = Read(path); err == nil {
		t.Fatal("oversized file accepted")
	}
	if _, err = Read(t.TempDir()); err == nil {
		t.Fatal("directory accepted")
	}
	paired := bytes.Replace(data, []byte(`Synthetic ordinary text`), []byte(`\ud83d\ude80 literal \\ud800`), 1)
	path = filepath.Join(t.TempDir(), "surrogate-pair.json")
	if err = os.WriteFile(path, paired, 0600); err != nil {
		t.Fatal(err)
	}
	valid, err := Read(path)
	if err != nil {
		t.Fatalf("valid surrogate pair rejected: %v", err)
	}
	if !strings.HasPrefix(valid.Events[0].Content, "🚀 literal \\ud800") {
		t.Fatal("valid Unicode escape content changed")
	}
}

func TestWriteFailurePreservesTargetAndCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.json")
	before := []byte("old complete export")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("synthetic rename failure")
	err := write(path, []byte("new export"), func(temp, target string) error {
		prepared, err := os.ReadFile(temp)
		if err != nil || string(prepared) != "new export" {
			t.Fatal("rename attempted before a complete temporary file")
		}
		current, _ := os.ReadFile(target)
		if !bytes.Equal(current, before) {
			t.Fatal("target truncated before rename")
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("failure lost: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed export changed target")
	}
	temporaries, _ := filepath.Glob(filepath.Join(dir, ".jeval-export-*"))
	if len(temporaries) != 0 {
		t.Fatal("failed export left a temporary file")
	}
	if err = Write(path, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(path)
	if string(after) != "replacement" {
		t.Fatal("existing target not replaced")
	}
	if err = Write(dir, []byte("bad")); err == nil {
		t.Fatal("directory export target accepted")
	}
	link := filepath.Join(dir, "link.json")
	if err = os.Symlink(path, link); err == nil {
		if err = Write(link, []byte("bad")); err == nil {
			t.Fatal("symlink export target accepted")
		}
		if _, err = Read(link); err == nil {
			t.Fatal("symlink input accepted")
		}
	} else {
		t.Logf("symlink creation unavailable: %v", err)
	}
}
