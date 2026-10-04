package protocol

import (
	"bytes"
	"encoding/json"
	"jeval/engine/internal/demo"
	"jeval/engine/internal/model"
	"strings"
	"testing"
)

func sample(t *testing.T) model.Record {
	t.Helper()
	r, err := demo.Load()
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func request(method, params string) Request {
	return Request{Type: "request", Version: 1, ID: "test", Method: method, Params: json.RawMessage(params)}
}
func TestInvalidRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  Request
		code string
	}{
		{"version", Request{Type: "request", Version: 2, ID: "test"}, "PROTOCOL_MISMATCH"},
		{"missing id", Request{Type: "request", Version: 1}, "INVALID_REQUEST"},
		{"method", request("nope", "{}"), "METHOD_NOT_FOUND"},
		{"negative offset", request("runs.list", `{"offset":-1}`), "INVALID_PARAMS"},
		{"zero limit", request("runs.list", `{"limit":0}`), "INVALID_PARAMS"},
		{"large limit", request("runs.list", `{"limit":101}`), "INVALID_PARAMS"},
		{"null params", request("runs.list", `null`), "INVALID_PARAMS"},
		{"invalid status", request("runs.list", `{"status":"success"}`), "INVALID_PARAMS"},
		{"missing run", request("runs.events", `{"runId":"missing"}`), "NOT_FOUND"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := dispatch(tc.req, sample(t))
			if res.Error == nil || res.Error.Code != tc.code {
				t.Fatalf("unexpected response: %+v", res)
			}
		})
	}
}
func TestSearchPaginationAndUnknownMetrics(t *testing.T) {
	r := sample(t)
	page := dispatch(request("runs.list", `{"search":"ACME","limit":1}`), r).Result.(Page[model.Run])
	if len(page.Items) != 1 || page.Total != 2 || page.NextOffset == nil || *page.NextOffset != 1 {
		t.Fatalf("bad page: %+v", page)
	}
	page = dispatch(request("runs.list", `{"status":"unknown"}`), r).Result.(Page[model.Run])
	if len(page.Items) != 1 || page.Items[0].Tokens != nil || page.Items[0].DurationMs != nil || page.Items[0].StartedAt != nil {
		t.Fatalf("unknown values lost: %+v", page)
	}
	page = dispatch(request("runs.list", `{"offset":999}`), r).Result.(Page[model.Run])
	b, _ := json.Marshal(page)
	if !strings.Contains(string(b), `"items":[]`) || page.NextOffset != nil {
		t.Fatalf("bad empty page: %s", b)
	}
}
func TestStreamRecoveryAndShutdown(t *testing.T) {
	input := "{broken}\n" + `{"type":"request","version":1,"id":"1","method":"hello"}` + "\n" + `{"type":"request","version":1,"id":"2","method":"shutdown"}` + "\n" + `{"type":"request","version":1,"id":"3","method":"hello"}` + "\n"
	var output bytes.Buffer
	if err := Serve(strings.NewReader(input), &output, sample(t)); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "PARSE_ERROR") || !strings.Contains(lines[1], "engineVersion") || !strings.Contains(lines[2], `"ok":true`) {
		t.Fatal(output.String())
	}
}
func TestOversizedFrame(t *testing.T) {
	if err := Serve(strings.NewReader(strings.Repeat("x", MaxFrameBytes+1)), &bytes.Buffer{}, sample(t)); err == nil {
		t.Fatal("oversized frame accepted")
	}
}
func TestEvidenceAndEventPagination(t *testing.T) {
	r := sample(t)
	for _, run := range r.Runs {
		page := dispatch(request("runs.events", `{"runId":"`+run.ID+`","limit":1}`), r).Result.(Page[model.Event])
		if page.Total != run.EventCount || len(page.Items) != 1 || page.NextOffset == nil {
			t.Fatalf("bad event page for %s", run.ID)
		}
	}
	ids := map[string]bool{}
	for _, e := range r.Events {
		if ids[e.ID] || e.Evidence.Location == "" || e.Evidence.Line < 1 {
			t.Fatalf("invalid evidence: %+v", e)
		}
		if e.ParentID != nil && !ids[*e.ParentID] {
			t.Fatalf("missing parent: %+v", e)
		}
		ids[e.ID] = true
	}
}

func TestEventSearchFiltersBeforePaginationAndKeepsEvidence(t *testing.T) {
	r := sample(t)
	page := dispatch(request("runs.events", `{"runId":"demo-cache","search":"  PROMISE  ","limit":1}`), r).Result.(Page[model.Event])
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].Sequence != 4 || page.NextOffset == nil || *page.NextOffset != 1 {
		t.Fatal(page)
	}
	if page.Items[0].Evidence.Line != 4 || *page.Items[0].ParentID != "cache-3" {
		t.Fatal("filtered event lost references")
	}
	page = dispatch(request("runs.events", `{"runId":"demo-cache","search":"promise","limit":1,"offset":1}`), r).Result.(Page[model.Event])
	if page.Total != 2 || page.Items[0].Sequence != 5 || page.NextOffset != nil {
		t.Fatal(page)
	}
	page = dispatch(request("runs.events", `{"runId":"demo-cache","search":"promise","kind":"verification"}`), r).Result.(Page[model.Event])
	if page.Total != 1 || page.Items[0].Sequence != 5 {
		t.Fatal(page)
	}
	page = dispatch(request("runs.events", `{"runId":"demo-cache","kind":"error"}`), r).Result.(Page[model.Event])
	if page.Total != 0 || page.Items == nil {
		t.Fatal(page)
	}
	if res := dispatch(request("runs.events", `{"runId":"demo-cache","kind":"other"}`), r); res.Error == nil || res.Error.Code != "INVALID_PARAMS" {
		t.Fatal(res)
	}
}
