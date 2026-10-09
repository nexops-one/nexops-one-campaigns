// SPDX-License-Identifier: Apache-2.0

package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/auth"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

const (
	tokenA = "token-a"
	tokenB = "token-b"
)

var (
	ctx    = context.Background()
	scopeA = adapter.Scope{TenantID: "acme", WorkspaceID: "ws-1"}
	scopeB = adapter.Scope{TenantID: "acme", WorkspaceID: "ws-2"}
)

type setup struct {
	engine  func(*compliance.Config)
	options func(*httpapi.Options)
}

func newAPI(t *testing.T, s setup) http.Handler {
	t.Helper()
	cfg := compliance.Config{Store: memory.New()}
	if s.engine != nil {
		s.engine(&cfg)
	}
	eng, err := compliance.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	opts := httpapi.Options{
		Authenticator: auth.NewStaticTokens([]auth.TokenEntry{
			{Hash: auth.HashToken(tokenA), Scope: scopeA},
			{Hash: auth.HashToken(tokenB), Scope: scopeB},
		}),
		Version: "test",
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if s.options != nil {
		s.options(&opts)
	}
	return httpapi.New(eng, opts)
}

type response struct {
	Status int
	Header http.Header
	Body   map[string]any
	Raw    []byte
}

// call sends a request; body may be nil, []byte (sent as is) or any value (sent as JSON).
func call(t *testing.T, h http.Handler, method, path, token string, body any) response {
	t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := response{Status: rec.Code, Header: rec.Header(), Raw: rec.Body.Bytes()}
	_ = json.Unmarshal(res.Raw, &res.Body)
	return res
}

func errorCode(r response) string {
	e, _ := r.Body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func sampleBatch(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../compliance/testdata/sample-batch.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sampleManifest(t *testing.T) adapter.Manifest {
	t.Helper()
	b, err := adapter.DecodeBatch(bytes.NewReader(sampleBatch(t)))
	if err != nil {
		t.Fatal(err)
	}
	supplies := map[string][]string{}
	for entity, recs := range b.Entities {
		seen := map[string]bool{}
		for _, r := range recs {
			for f := range r {
				if f != schema.MetaField && !seen[f] {
					seen[f] = true
					supplies[entity] = append(supplies[entity], f)
				}
			}
		}
		sort.Strings(supplies[entity])
	}
	return adapter.Manifest{Name: "csv-import", Version: "0.1.0", SchemaVersion: "0.1.0", Supplies: supplies, Modes: []adapter.Mode{adapter.ModeIncremental, adapter.ModeFull}}
}

// loaded returns an API with the sample manifest registered and the sample batch ingested for tokenA.
func loaded(t *testing.T) http.Handler {
	t.Helper()
	h := newAPI(t, setup{})
	if r := call(t, h, "PUT", "/api/v1/adapters/csv-import/manifest", tokenA, sampleManifest(t)); r.Status != 200 {
		t.Fatalf("manifest: %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "POST", "/api/v1/ingestions", tokenA, sampleBatch(t)); r.Status != 201 {
		t.Fatalf("ingestion: %d %s", r.Status, r.Raw)
	}
	return h
}
