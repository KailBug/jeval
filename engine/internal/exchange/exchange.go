// Package exchange reads and writes versioned normalized previews. Source
// locators inside a record are evidence text; this package never opens them.
package exchange

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"jeval/engine/internal/model"
)

const (
	MaxFileBytes    = 64 * 1024 * 1024
	MaxContentBytes = 8192
	MaxEvents       = model.MaxSnapshotEvents
	maxSafeInteger  = int64(9007199254740991)
	previewMarker   = "\n[内容预览已截断，请按来源行号查看原文件]"
)

type envelope struct {
	Format              string       `json:"format"`
	FormatVersion       int          `json:"formatVersion"`
	ContentScope        string       `json:"contentScope"`
	MaxContentBytes     int          `json:"maxContentBytes"`
	SourceFilesIncluded bool         `json:"sourceFilesIncluded"`
	Record              model.Record `json:"record"`
}

// Validate enforces the v1 exchange subset without interpreting source paths.
func Validate(record model.Record) error {
	if record.SchemaVersion != 1 || len(record.Runs) != 1 || record.Events == nil {
		return errors.New("exchange requires record schema 1 with one run and an event array")
	}
	r := record.Runs[0]
	i := r.ImportInfo
	if r.Demo || r.ReadOnly || r.Source != "Codex" || i == nil || i.AdapterVersion != "codex-rollout-v1" {
		return errors.New("exchange supports non-demo codex-rollout-v1 snapshots only")
	}
	if !prefixedDigest(r.ID, "codex-") || !digest(i.SHA256) || i.SnapshotID != model.SnapshotID(r.ID, i.SHA256, i.AdapterVersion) {
		return errors.New("invalid source or snapshot digest identity")
	}
	if i.HistoryMode != "classic" && i.HistoryMode != "paginated" {
		return errors.New("unsupported Codex history mode")
	}
	if r.Status != "completed" && r.Status != "failed" && r.Status != "unknown" {
		return errors.New("invalid run status")
	}
	if r.EventCount != len(record.Events) || r.EventCount < 0 || r.EventCount > MaxEvents {
		return fmt.Errorf("exchange event count mismatch or %d-event limit exceeded", MaxEvents)
	}
	for _, value := range []struct {
		name, value string
		max         int
		required    bool
	}{
		{"run title", r.Title, 16384, true}, {"project", r.Project, 16384, true},
		{"source file", i.File, 2048, true}, {"session ID", i.SessionID, 1024, true},
		{"CLI version", i.CLIVersion, 1024, false}, {"parent thread ID", i.ParentThreadID, 1024, false},
		{"forked source ID", i.ForkedFromID, 1024, false},
	} {
		if err := text(value.name, value.value, value.max, value.required); err != nil {
			return err
		}
	}
	if err := timestamp("startedAt", r.StartedAt); err != nil {
		return err
	}
	for _, value := range []*int64{r.DurationMs, r.Tokens} {
		if value != nil && (*value < 0 || *value > maxSafeInteger) {
			return errors.New("metric must be null or a nonnegative safe JavaScript integer")
		}
	}
	if i.Warnings == nil || len(i.Warnings) > 30 || i.WarningCount < len(i.Warnings) || int64(i.WarningCount) > maxSafeInteger {
		return errors.New("invalid import warning counts")
	}
	for _, warning := range i.Warnings {
		if warning.Line < 1 || int64(warning.Line) > maxSafeInteger {
			return errors.New("invalid warning line")
		}
		if err := text("warning message", warning.Message, 16384, true); err != nil {
			return err
		}
	}
	metadata, err := json.Marshal(r)
	if err != nil || len(metadata) > 256*1024 {
		return errors.New("run metadata exceeds 256 KiB encoded limit")
	}
	ids := make(map[string]int, len(record.Events))
	for n, event := range record.Events {
		if event.RunID != r.ID || event.Sequence != n+1 || event.Evidence.SourceID != r.ID || event.Evidence.SnapshotID != i.SnapshotID || event.Evidence.Location != i.File || event.Evidence.Line < 1 || int64(event.Evidence.Line) > maxSafeInteger || event.ID != fmt.Sprintf("%s:%d", r.ID, event.Evidence.Line) {
			return fmt.Errorf("event %d has invalid identity, sequence or evidence", n+1)
		}
		if _, found := ids[event.ID]; found {
			return errors.New("duplicate event ID")
		}
		ids[event.ID] = n
		if !oneOf(event.Kind, "message", "tool_call", "tool_result", "verification", "lifecycle", "error") || !oneOf(event.Role, "user", "assistant", "system", "developer", "tool") {
			return errors.New("invalid event kind or role")
		}
		if err := text("event title", event.Title, 16384, true); err != nil {
			return err
		}
		body := event.Content
		if strings.HasSuffix(body, previewMarker) {
			body = strings.TrimSuffix(body, previewMarker)
		}
		if err := text("event content", body, MaxContentBytes, false); err != nil {
			return err
		}
		if err := timestamp("event timestamp", event.Timestamp); err != nil {
			return err
		}
	}
	// A three-state traversal detects cycles without repeatedly walking every
	// ancestor in a long chain. Forward references remain valid.
	state := make([]uint8, len(record.Events))
	var visit func(int) error
	visit = func(n int) error {
		if state[n] == 1 {
			return errors.New("event parent cycle")
		}
		if state[n] == 2 {
			return nil
		}
		state[n] = 1
		if parent := record.Events[n].ParentID; parent != nil {
			p, found := ids[*parent]
			if !found {
				return errors.New("parent event is outside snapshot")
			}
			if err := visit(p); err != nil {
				return err
			}
		}
		state[n] = 2
		return nil
	}
	for n := range record.Events {
		if err := visit(n); err != nil {
			return err
		}
	}
	return nil
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func digest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func prefixedDigest(value, prefix string) bool {
	return strings.HasPrefix(value, prefix) && digest(strings.TrimPrefix(value, prefix))
}

func text(name, value string, max int, required bool) error {
	if !utf8.ValidString(value) || len(value) > max || required && value == "" {
		return fmt.Errorf("invalid %s: empty, invalid UTF-8 or byte limit exceeded", name)
	}
	return nil
}

var rfc3339 = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$`)

func timestamp(name string, value *string) error {
	if value == nil {
		return nil
	}
	if len(*value) > 128 || !rfc3339.MatchString(*value) {
		return fmt.Errorf("invalid RFC3339 %s", name)
	}
	if !strings.HasSuffix(*value, "Z") {
		zone := (*value)[len(*value)-6:]
		if zone[1:3] > "23" || zone[4:6] > "59" {
			return fmt.Errorf("invalid RFC3339 %s offset", name)
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, *value); err != nil {
		return fmt.Errorf("invalid RFC3339 %s", name)
	}
	return nil
}

// Encode produces deterministic JSON or a plain-text Markdown report. It
// refuses invalid data rather than silently changing IDs, unknowns or previews.
func Encode(record model.Record, format string) ([]byte, error) {
	if err := Validate(record); err != nil {
		return nil, err
	}
	var data []byte
	var err error
	switch format {
	case "json":
		data, err = json.MarshalIndent(envelope{"jeval-record", 1, "normalized-preview", MaxContentBytes, false, record}, "", "  ")
		data = append(data, '\n')
	case "markdown":
		data = markdown(record)
	default:
		return nil, errors.New("exchange format must be json or markdown")
	}
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, errors.New("exchange file exceeds 64 MiB")
	}
	return data, nil
}

// Read accepts a regular UTF-8 JSON file containing an exact versioned shape.
// It never reads original evidence locations carried by that file.
func Read(path string) (model.Record, error) {
	var empty model.Record
	info, err := os.Lstat(path)
	if err != nil {
		return empty, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
		return empty, errors.New("exchange import requires a regular file at most 64 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return empty, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return empty, errors.New("exchange import file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return empty, err
	}
	if len(data) > MaxFileBytes {
		return empty, errors.New("exchange file exceeds 64 MiB")
	}
	if !utf8.Valid(data) {
		return empty, errors.New("exchange JSON must be UTF-8")
	}
	if err = uniqueKeys(data); err != nil {
		return empty, err
	}
	if err = validUnicodeEscapes(data); err != nil {
		return empty, err
	}
	if err = strictShape(data, reflect.TypeFor[envelope](), "exchange"); err != nil {
		return empty, err
	}
	var value envelope
	if err = json.Unmarshal(data, &value); err != nil {
		return empty, err
	}
	if value.Format != "jeval-record" || value.FormatVersion != 1 || value.ContentScope != "normalized-preview" || value.MaxContentBytes != MaxContentBytes || value.SourceFilesIncluded {
		return empty, errors.New("unsupported exchange format, version or content scope")
	}
	if err = Validate(value.Record); err != nil {
		return empty, err
	}
	return value.Record, nil
}

// Write prepares and syncs a same-directory temporary file before replacing the
// selected target. Existing bytes survive every pre-rename error. As documented
// by os.Rename, replacement is not guaranteed atomic on non-Unix platforms;
// this does not promise power-loss durability of directory entries either.
func Write(path string, data []byte) error {
	return write(path, data, os.Rename)
}

func write(path string, data []byte, rename func(string, string) error) error {
	if len(data) > MaxFileBytes {
		return errors.New("exchange file exceeds 64 MiB")
	}
	if err := writableTarget(path); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".jeval-export-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = writableTarget(path); err != nil {
		return err
	}
	return rename(name, path)
}

func writableTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("exchange export target must be a regular file")
	}
	return nil
}

// Encoding/json permits duplicate keys and case-insensitive struct fields.
// Check tokens and exact required keys before using its typed decoder.
func uniqueKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return errors.New("exchange JSON nesting limit exceeded")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("exchange JSON has a duplicate object key")
				}
				seen[name] = true
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("exchange JSON has trailing data")
	}
	return nil
}

// Encoding/json replaces a lone UTF-16 surrogate escape with U+FFFD. Reject
// that lossy input while retaining valid surrogate pairs and literal escapes.
// JSON syntax has already been checked by uniqueKeys.
func validUnicodeEscapes(data []byte) error {
	inside := false
	for n := 0; n < len(data); n++ {
		if data[n] == '"' {
			inside = !inside
			continue
		}
		if !inside || data[n] != '\\' {
			continue
		}
		n++
		if data[n] != 'u' {
			continue
		}
		unit, _ := strconv.ParseUint(string(data[n+1:n+5]), 16, 16)
		n += 4
		if unit >= 0xdc00 && unit <= 0xdfff {
			return errors.New("exchange JSON has an unpaired Unicode surrogate")
		}
		if unit >= 0xd800 && unit <= 0xdbff {
			if n+6 >= len(data) || data[n+1] != '\\' || data[n+2] != 'u' {
				return errors.New("exchange JSON has an unpaired Unicode surrogate")
			}
			low, err := strconv.ParseUint(string(data[n+3:n+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return errors.New("exchange JSON has an unpaired Unicode surrogate")
			}
			n += 6
		}
	}
	return nil
}

func strictShape(data json.RawMessage, kind reflect.Type, path string) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		if kind.Kind() == reflect.Pointer {
			return nil
		}
		return fmt.Errorf("%s must not be null", path)
	}
	if kind.Kind() == reflect.Pointer {
		return strictShape(data, kind.Elem(), path)
	}
	switch kind.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			return fmt.Errorf("%s must be an object", path)
		}
		if kind == reflect.TypeFor[model.Run]() {
			if _, found := object["readOnly"]; found {
				return errors.New("readOnly is local library metadata, not an exchange field")
			}
		}
		for n := 0; n < kind.NumField(); n++ {
			field := kind.Field(n)
			tag := strings.Split(field.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			value, found := object[tag[0]]
			if !found {
				if len(tag) > 1 && tag[1] == "omitempty" {
					continue
				}
				return fmt.Errorf("%s.%s is required", path, tag[0])
			}
			delete(object, tag[0])
			if err := strictShape(value, field.Type, path+"."+tag[0]); err != nil {
				return err
			}
		}
		if len(object) != 0 {
			return fmt.Errorf("%s contains an unknown field", path)
		}
	case reflect.Slice:
		var values []json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return fmt.Errorf("%s must be an array", path)
		}
		for n, value := range values {
			if err := strictShape(value, kind.Elem(), fmt.Sprintf("%s[%d]", path, n)); err != nil {
				return err
			}
		}
	}
	return nil
}
