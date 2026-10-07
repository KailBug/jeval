package protocol

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"jeval/engine/internal/adapters/codex"
	"jeval/engine/internal/storage"
)

func TestLegacyContentManualUpgradeAndRequestValidation(t *testing.T) {
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "old.jsonl")
	body := strings.Repeat("中🙂", 8000) + "unseen tail"
	row, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]string{"type": "message", "role": "user", "content": body}})
	writeScanFile(t, source, `{"type":"session_meta","payload":{"id":"legacy"}}`+"\n"+string(row)+"\n")
	run, events, cp, _, err := codex.ReadUpdate(ctx, source, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	events[0].FullContent = nil
	store := openLibrary(t, filepath.Join(t.TempDir(), "legacy.sqlite"))
	if err = store.SaveCheckpoint(ctx, run, events, cp); err != nil {
		t.Fatal(err)
	}
	s := persistentService(t, store)
	params := map[string]any{"runId": run.ID, "snapshotId": run.ImportInfo.SnapshotID, "eventId": events[0].ID, "offset": 0}
	get := func() Response {
		b, _ := json.Marshal(params)
		return s.dispatch(request("runs.eventContent", string(b)))
	}
	res := get()
	if res.Error != nil || res.Result.(storage.ContentPage).Available {
		t.Fatal("startup fabricated full content", res)
	}
	res = s.dispatch(request("codex.update", `{"runId":"`+run.ID+`"}`))
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	report := res.Result.(map[string]any)["update"].(codex.UpdateReport)
	if report.Mode != "full" || report.Reason != "content-not-saved" {
		t.Fatal("legacy update didn't reparse", report)
	}
	res = get()
	if res.Error != nil || !res.Result.(storage.ContentPage).Available {
		t.Fatal("manual update failed to supplement", res)
	}
	encoded, _ := json.Marshal(res)
	if len(encoded) >= MaxFrameBytes {
		t.Fatal("body broke frame budget")
	}
	for _, tc := range []struct {
		key   string
		value any
		code  string
	}{
		{"offset", nil, "INVALID_PARAMS"}, {"offset", 1, "INVALID_PARAMS"}, {"offset", -1, "INVALID_PARAMS"}, {"offset", 0.5, "INVALID_PARAMS"}, {"offset", storage.MaxBodyBytes + 1, "INVALID_PARAMS"},
		{"eventId", "missing", "NOT_FOUND"}, {"snapshotId", "missing", "NOT_FOUND"}, {"runId", "other", "NOT_FOUND"}, {"runId", "", "INVALID_PARAMS"},
	} {
		old := params[tc.key]
		params[tc.key] = tc.value
		res = get()
		if res.Error == nil || res.Error.Code != tc.code {
			t.Fatalf("%s=%v: %+v", tc.key, tc.value, res)
		}
		params[tc.key] = old
	}
	// Exchange export must still exclude the tail of saved full content.
	dest := filepath.Join(t.TempDir(), "preview.json")
	res = s.dispatch(exportRequest(run.ID, dest, "json"))
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	detached := persistentService(t, openLibrary(t, filepath.Join(t.TempDir(), "detached.sqlite")))
	nativeRun(t, detached, dest)
	b, _ := json.Marshal(params)
	res = detached.dispatch(request("runs.eventContent", string(b)))
	if res.Error != nil || res.Result.(storage.ContentPage).Available {
		t.Fatal("exchange acquired full body", res)
	}
}
