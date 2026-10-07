package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"jeval/engine/internal/model"
)

const checkpointMeta = `{"type":"session_meta","payload":{"id":"checkpoint-fixture","history_mode":"paginated"}}` + "\n"
const checkpointUser = `{"type":"response_item","payload":{"type":"message","role":"user","content":"first"}}` + "\n"
const checkpointCall = `{"type":"response_item","payload":{"type":"function_call","call_id":"call-1","name":"test","arguments":"{}"}}` + "\n"
const checkpointOutput = `{"type":"response_item","payload":{"type":"function_call_output","call_id":"call-1","output":"ok"}}` + "\n"
const checkpointProjection = `{"type":"event_msg","payload":{"type":"item_completed","turn_id":"turn-1","item":{"type":"AgentMessage","id":"answer","content":"answer"}}}` + "\n"
const checkpointCanonical = `{"type":"response_item","payload":{"type":"message","id":"answer","role":"assistant","content":"answer"}}` + "\n"

func TestCheckpointMatchesFullReadAcrossChangesAndRestart(t *testing.T) {
	cases := []struct {
		name     string
		contents []string
		modes    []string
	}{
		{"append-tools", []string{checkpointMeta + checkpointUser + checkpointCall, checkpointMeta + checkpointUser + checkpointCall + checkpointOutput, checkpointMeta + checkpointUser + checkpointCall + checkpointOutput}, []string{"full", "incremental", "unchanged"}},
		{"partial-line", []string{checkpointMeta + checkpointUser, checkpointMeta + checkpointUser + `{"type":"response_item","payload":`, checkpointMeta + checkpointUser + checkpointCanonical}, []string{"full", "incremental", "full"}},
		{"no-final-newline", []string{strings.TrimSuffix(checkpointMeta+checkpointUser, "\n"), checkpointMeta + checkpointUser + checkpointCanonical}, []string{"full", "full"}},
		{"truncate-and-replace", []string{checkpointMeta + checkpointUser + checkpointCall + checkpointOutput, checkpointMeta + checkpointUser, checkpointMeta + strings.ReplaceAll(checkpointUser, "first", "other")}, []string{"full", "full", "full"}},
		{"late-canonical", []string{checkpointMeta + checkpointUser + checkpointProjection, checkpointMeta + checkpointUser + checkpointProjection + checkpointCanonical}, []string{"full", "full"}},
		{"late-projection", []string{checkpointMeta + checkpointUser + checkpointCanonical, checkpointMeta + checkpointUser + checkpointCanonical + checkpointProjection}, []string{"full", "full"}},
		{"late-tool-call", []string{checkpointMeta + checkpointUser + checkpointOutput, checkpointMeta + checkpointUser + checkpointOutput + checkpointCall}, []string{"full", "full"}},
		{"empty-prefix", []string{checkpointMeta, checkpointMeta + checkpointUser}, []string{"full", "full"}},
		{"title-after-empty-user", []string{checkpointMeta + strings.ReplaceAll(checkpointUser, "first", ""), checkpointMeta + strings.ReplaceAll(checkpointUser, "first", "") + checkpointUser}, []string{"full", "incremental"}},
		{"bom-and-crlf", []string{"\xef\xbb\xbf" + strings.ReplaceAll(checkpointMeta+checkpointUser, "\n", "\r\n"), "\xef\xbb\xbf" + strings.ReplaceAll(checkpointMeta+checkpointUser+checkpointCall, "\n", "\r\n")}, []string{"full", "incremental"}},
		{"warnings-and-long-text", []string{checkpointMeta + checkpointUser + "invalid\n", checkpointMeta + checkpointUser + "invalid\n" + strings.ReplaceAll(checkpointCanonical, "answer", strings.Repeat("中", 3000))}, []string{"full", "incremental"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.jsonl")
			var cp *Checkpoint
			var saved *model.Record
			for i, content := range tc.contents {
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				before, _ := json.Marshal(saved)
				run, events, next, report, err := ReadUpdate(context.Background(), path, cp, saved, nil)
				if err != nil {
					t.Fatal(i, err)
				}
				if report.Mode != tc.modes[i] {
					t.Fatalf("step %d: %+v", i, report)
				}
				wantRun, wantEvents, err := Read(path)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(run, wantRun) || !reflect.DeepEqual(events, wantEvents) {
					t.Fatalf("step %d diverged from full read\nrun=%+v\nwant=%+v\nevents=%+v\nwant=%+v", i, run, wantRun, events, wantEvents)
				}
				after, _ := json.Marshal(saved)
				if string(before) != string(after) {
					t.Fatal("old snapshot mutated")
				}
				if report.Mode == "incremental" && report.ParsedLines >= next.Lines+1 {
					t.Fatal("append parsed the full prefix")
				}
				// Serialize both parts, as a restarted engine does; no live parser state survives.
				encoded, _ := json.Marshal(next)
				cp = new(Checkpoint)
				if err := json.Unmarshal(encoded, cp); err != nil {
					t.Fatal(err)
				}
				record := model.Record{SchemaVersion: 1, Runs: []model.Run{run}, Events: events}
				encoded, _ = json.Marshal(record)
				saved = new(model.Record)
				if err := json.Unmarshal(encoded, saved); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestCheckpointFailureCancellationAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.jsonl")
	if err := os.WriteFile(path, []byte(checkpointMeta+checkpointUser+checkpointCall), 0600); err != nil {
		t.Fatal(err)
	}
	run, events, cp, _, err := ReadUpdate(context.Background(), path, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	saved := &model.Record{SchemaVersion: 1, Runs: []model.Run{run}, Events: events}
	if err := os.WriteFile(path, []byte(checkpointMeta+checkpointUser+checkpointCall+checkpointCall), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := ReadUpdate(context.Background(), path, cp, saved, nil); err == nil {
		t.Fatal("duplicate call accepted")
	}
	if err := os.WriteFile(path, []byte(checkpointMeta+checkpointUser+checkpointCall+checkpointOutput), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Checkpoint){func(c *Checkpoint) { c.Version++ }, func(c *Checkpoint) { c.Lines = 49999 }, func(c *Checkpoint) { c.SnapshotID = "bad" }, func(c *Checkpoint) { c.Offset = MaxFileBytes + 1 }} {
		bad := *cp
		mutate(&bad)
		got, ev, _, report, err := ReadUpdate(context.Background(), path, &bad, saved, nil)
		if err != nil {
			t.Fatal(err)
		}
		want, wantEv, err := Read(path)
		if err != nil {
			t.Fatal(err)
		}
		if report.Mode != "full" || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(ev, wantEv) {
			t.Fatal(report)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, _, _, _, err = ReadUpdate(ctx, path, cp, saved, func(phase string) {
		if phase == "parsing" {
			cancel()
		}
	})
	if err != context.Canceled {
		t.Fatalf("cancel = %v", err)
	}
}
