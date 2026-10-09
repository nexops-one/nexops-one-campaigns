// SPDX-License-Identifier: Apache-2.0

package push_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/push"
)

var ctx = context.Background()

type request struct {
	method, path, auth string
	body               []byte
}

type fakeEngine struct {
	mu       sync.Mutex
	requests []request
}

func (f *fakeEngine) got() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.requests...)
}

// newFake starts a fake engine; respond gets the 1-based request number.
func newFake(t *testing.T, respond func(n int, body []byte) (int, string)) (*fakeEngine, *push.Client) {
	t.Helper()
	f := &fakeEngine{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, request{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
		n := len(f.requests)
		f.mu.Unlock()
		status, resp := respond(n, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	c := push.New(srv.URL+"/", "tok")
	c.Backoff = time.Millisecond
	return f, c
}

func accept(n int, body []byte) (int, string) {
	b, err := adapter.DecodeBatch(strings.NewReader(string(body)))
	if err != nil {
		return 400, `{"error":{"code":"invalid_json","message":"bad"}}`
	}
	return 201, fmt.Sprintf(`{"ingestion_id":"ing-%d","batch_id":%q,"accepted":%d,"snapshot_id":"rev-%d","unknown_field":true}`, n, b.BatchID(), b.RecordCount(), n)
}

var manifest = adapter.Manifest{Name: "vendor.inventory", Version: "1.0.0", SchemaVersion: "0.1.0",
	Supplies: map[string][]string{"ict_provider": {"provider_id_code", "legal_name"}},
	Modes:    []adapter.Mode{adapter.ModeFull, adapter.ModeIncremental}}

func batchOf(n int, mode adapter.Mode) adapter.Batch {
	b := adapter.NewBuilder(manifest, "vendor-db").Mode(mode).BatchID("b1")
	for i := 1; i <= n; i++ {
		b.Add("ict_provider", adapter.Record{"provider_id_code": fmt.Sprintf("P%d", i), "legal_name": strings.Repeat("x", 100)})
	}
	return b.Build()
}

func decode(t *testing.T, body []byte) adapter.Batch {
	t.Helper()
	b, err := adapter.DecodeBatch(strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRegisterManifest(t *testing.T) {
	f, c := newFake(t, func(int, []byte) (int, string) { return 200, `{}` })
	if err := c.RegisterManifest(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	r := f.got()
	if len(r) != 1 || r[0].method != "PUT" || r[0].path != "/api/v1/adapters/vendor.inventory/manifest" || r[0].auth != "Bearer tok" {
		t.Fatalf("requests = %+v", r)
	}
	var m adapter.Manifest
	if err := json.Unmarshal(r[0].body, &m); err != nil || m.Name != manifest.Name {
		t.Fatalf("body = %s", r[0].body)
	}
}

func TestPushSendsOneRequestWhenItFits(t *testing.T) {
	f, c := newFake(t, accept)
	res, err := c.Push(ctx, batchOf(3, adapter.ModeFull))
	if err != nil {
		t.Fatal(err)
	}
	r := f.got()
	if len(r) != 1 || r[0].method != "POST" || r[0].path != "/api/v1/ingestions" || decode(t, r[0].body).BatchID() != "b1" {
		t.Fatalf("requests = %+v", r)
	}
	if len(res) != 1 || res[0].IngestionID != "ing-1" || res[0].Accepted != 3 || res[0].SnapshotID != "rev-1" {
		t.Fatalf("results = %+v", res)
	}
}

func TestPushSplitsLargeIncrementalBatch(t *testing.T) {
	f, c := newFake(t, accept)
	c.MaxRecords = 2
	res, err := c.Push(ctx, batchOf(5, adapter.ModeIncremental))
	if err != nil {
		t.Fatal(err)
	}
	var ids, codes []string
	var sizes []int
	for _, r := range f.got() {
		b := decode(t, r.body)
		ids = append(ids, b.BatchID())
		sizes = append(sizes, b.RecordCount())
		if b.Mode() != adapter.ModeIncremental || b.Source.Adapter != manifest.Name || b.Source.System != "vendor-db" {
			t.Fatalf("part envelope = %+v", b)
		}
		for _, rec := range b.Entities["ict_provider"] {
			codes = append(codes, rec["provider_id_code"].(string))
		}
	}
	if fmt.Sprint(ids) != "[b1#1 b1#2 b1#3]" || fmt.Sprint(sizes) != "[2 2 1]" || fmt.Sprint(codes) != "[P1 P2 P3 P4 P5]" || len(res) != 3 {
		t.Fatalf("ids %v sizes %v codes %v results %d", ids, sizes, codes, len(res))
	}
}

func TestPushSplitsBySize(t *testing.T) {
	f, c := newFake(t, accept)
	c.MaxBodyBytes = 700
	if _, err := c.Push(ctx, batchOf(6, adapter.ModeIncremental)); err != nil {
		t.Fatal(err)
	}
	total := 0
	r := f.got()
	for _, req := range r {
		if len(req.body) > 700 {
			t.Fatalf("request of %d bytes exceeds the 700-byte limit", len(req.body))
		}
		total += decode(t, req.body).RecordCount()
	}
	if len(r) < 2 || total != 6 {
		t.Fatalf("%d requests carrying %d records", len(r), total)
	}
}

func TestPushRefusesToSplitFullBatch(t *testing.T) {
	f, c := newFake(t, accept)
	c.MaxRecords = 2
	_, err := c.Push(ctx, batchOf(3, adapter.ModeFull))
	if !errors.Is(err, push.ErrFullBatchTooLarge) || len(f.got()) != 0 {
		t.Fatalf("err = %v, requests = %d", err, len(f.got()))
	}
}

func TestPushRejectsOversizedRecord(t *testing.T) {
	f, c := newFake(t, accept)
	c.MaxBodyBytes = 150
	if _, err := c.Push(ctx, batchOf(1, adapter.ModeIncremental)); !errors.Is(err, push.ErrRecordTooLarge) || len(f.got()) != 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestPushRetriesTransientFailures(t *testing.T) {
	f, c := newFake(t, func(n int, body []byte) (int, string) {
		if n < 3 {
			return 503, `{"error":{"code":"unavailable","message":"busy"}}`
		}
		return accept(n, body)
	})
	res, err := c.Push(ctx, batchOf(1, adapter.ModeFull))
	if err != nil || len(res) != 1 || len(f.got()) != 3 {
		t.Fatalf("err = %v, results = %+v, requests = %d", err, res, len(f.got()))
	}
	f, c = newFake(t, func(int, []byte) (int, string) { return 503, `busy` })
	c.MaxAttempts = 2
	_, err = c.Push(ctx, batchOf(1, adapter.ModeFull))
	var ae *push.APIError
	if !errors.As(err, &ae) || ae.Status != 503 || ae.Message != "busy" || len(f.got()) != 2 {
		t.Fatalf("exhausted retries: err = %v, requests = %d", err, len(f.got()))
	}
}

func TestPushReturnsAPIErrors(t *testing.T) {
	f, c := newFake(t, func(n int, body []byte) (int, string) {
		if n == 2 {
			return 422, `{"error":{"code":"invalid_batch","message":"the batch envelope is invalid","details":[{"entity":"","index":-1,"field":"schema_version","code":"pattern","message":"bad"}]}}`
		}
		return accept(n, body)
	})
	c.MaxRecords = 2
	res, err := c.Push(ctx, batchOf(5, adapter.ModeIncremental))
	var ae *push.APIError
	if !errors.As(err, &ae) || ae.Status != 422 || ae.Code != "invalid_batch" || len(ae.Details) != 1 || ae.Details[0].Field != "schema_version" {
		t.Fatalf("err = %#v", err)
	}
	if len(res) != 1 || len(f.got()) != 2 {
		t.Fatalf("committed results = %d, requests = %d (want 1 and 2: no retry, stop at the failed part)", len(res), len(f.got()))
	}
}
