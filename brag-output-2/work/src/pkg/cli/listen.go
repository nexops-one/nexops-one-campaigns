// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/adaptertest"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// listenChecks are the checks of the listen mode. Idempotency needs two syncs
// of the same input and is checked in file mode (--repeat).
var listenChecks = []string{adaptertest.CheckPull, adaptertest.CheckEnvelope, adaptertest.CheckManifestMatch, adaptertest.CheckRecords,
	adaptertest.CheckIdentity, adaptertest.CheckOutside, adaptertest.CheckDerived, adaptertest.CheckFullScope}

// conformanceServer is a local, unauthenticated stand-in for an engine. It
// accepts manifest registrations and batch pushes on the engine's paths,
// checks them, and commits nothing. The session is treated as one sync.
type conformanceServer struct {
	reg     *schema.Registry
	derived map[string][]string
	handler http.Handler

	mu               sync.Mutex
	manifests        map[string]adapter.Manifest
	manifestCalls    int
	manifestFindings []adaptertest.Finding
	pushes           int
	batches          []adapter.Batch
	batchFindings    []adaptertest.Finding
}

func newConformanceServer(reg *schema.Registry, derived map[string][]string) *conformanceServer {
	cs := &conformanceServer{reg: reg, derived: derived, manifests: map[string]adapter.Manifest{}}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/adapters/{name}/manifest", cs.putManifest)
	mux.HandleFunc("POST /api/v1/ingestions", cs.postIngestion)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		listenError(w, http.StatusNotFound, "not_found", "the conformance endpoint implements only PUT /api/v1/adapters/{name}/manifest and POST /api/v1/ingestions")
	})
	cs.handler = mux
	return cs
}

func (cs *conformanceServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	cs.handler.ServeHTTP(w, r)
}

func listenReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func listenError(w http.ResponseWriter, status int, code, msg string) {
	listenReply(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func (cs *conformanceServer) putManifest(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var m adapter.Manifest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	status, code := http.StatusOK, ""
	var fs []adaptertest.Finding
	if err := dec.Decode(&m); err != nil {
		status, code = http.StatusBadRequest, "invalid_json"
		fs = []adaptertest.Finding{{Check: adaptertest.CheckManifest, Index: -1, Message: "manifest body: " + err.Error()}}
	} else if m.Name != name {
		status, code = http.StatusUnprocessableEntity, "name_mismatch"
		fs = []adaptertest.Finding{{Check: adaptertest.CheckManifest, Index: -1, Field: "name",
			Message: fmt.Sprintf("the manifest name %q does not match the adapter name %q in the path", m.Name, name)}}
	} else if fs = adaptertest.ValidateManifest(cs.reg, m); len(fs) > 0 {
		status, code = http.StatusUnprocessableEntity, "invalid_manifest"
	}
	cs.mu.Lock()
	cs.manifestCalls++
	cs.manifestFindings = append(cs.manifestFindings, adaptertest.Label(fs, "manifest "+name)...)
	if len(fs) == 0 {
		cs.manifests[m.Name] = m
	}
	cs.mu.Unlock()
	if status != http.StatusOK {
		listenError(w, status, code, fs[0].Message)
		return
	}
	listenReply(w, http.StatusOK, m)
}

func (cs *conformanceServer) add(b *adapter.Batch, fs ...adaptertest.Finding) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if b != nil {
		cs.batches = append(cs.batches, *b)
	}
	cs.batchFindings = append(cs.batchFindings, fs...)
}

func (cs *conformanceServer) postIngestion(w http.ResponseWriter, r *http.Request) {
	cs.mu.Lock()
	cs.pushes++
	label := fmt.Sprintf("push %d", cs.pushes)
	cs.mu.Unlock()
	b, err := adapter.DecodeBatch(r.Body)
	if err != nil {
		cs.add(nil, adaptertest.Finding{Check: adaptertest.CheckEnvelope, Batch: label, Index: -1, Message: err.Error()})
		listenError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	cs.mu.Lock()
	m, ok := cs.manifests[b.Source.Adapter]
	cs.mu.Unlock()
	if !ok {
		f := adaptertest.Finding{Check: adaptertest.CheckManifestMatch, Batch: label, Index: -1, Field: "source.adapter",
			Message: fmt.Sprintf("adapter %q pushed a batch without a registered valid manifest", b.Source.Adapter)}
		cs.add(nil, f)
		listenError(w, http.StatusUnprocessableEntity, "unknown_adapter", f.Message)
		return
	}
	fs := adaptertest.Label(adaptertest.ValidateBatch(cs.reg, m, b, cs.derived), label)
	cs.add(&b, fs...)
	rejected := map[string]bool{}
	errs := []schema.FieldError{}
	for _, f := range fs {
		if !f.Warning && f.Index >= 0 && (f.Check == adaptertest.CheckRecords || f.Check == adaptertest.CheckIdentity) {
			rejected[fmt.Sprintf("%s/%d", f.Entity, f.Index)] = true
			errs = append(errs, schema.FieldError{Entity: f.Entity, Index: f.Index, SourceRecordRef: f.SourceRecordRef, Field: f.Field, Code: f.Check, Message: f.Message})
		}
	}
	listenReply(w, http.StatusCreated, map[string]any{
		"ingestion_id": "conformance-" + strings.ReplaceAll(label, " ", "-"), "batch_id": b.BatchID(),
		"schema_version": b.SchemaVersion, "source": b.Source, "mode": b.Mode(), "dry_run": false,
		"accepted": b.RecordCount() - len(rejected), "rejected_records": len(rejected),
		"errors": errs, "warnings": []schema.FieldError{},
		"created": 0, "updated": 0, "deleted": 0, "unchanged": 0, "no_changes": true, "snapshot_id": "conformance",
	})
}

func (cs *conformanceServer) report() adaptertest.Report {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	names := make([]string, 0, len(cs.manifests))
	for n := range cs.manifests {
		names = append(names, n)
	}
	sort.Strings(names)
	adapterName := strings.Join(names, ", ")
	if adapterName == "" {
		adapterName = "(no valid manifest registered)"
	}
	mf := append([]adaptertest.Finding(nil), cs.manifestFindings...)
	if cs.manifestCalls == 0 {
		mf = append(mf, adaptertest.Finding{Check: adaptertest.CheckManifest, Index: -1, Message: "no manifest was registered (PUT /api/v1/adapters/{name}/manifest)"})
	}
	bf := append([]adaptertest.Finding(nil), cs.batchFindings...)
	if cs.pushes == 0 {
		bf = append(bf, adaptertest.Finding{Check: adaptertest.CheckPull, Index: -1, Message: "no batch was pushed (POST /api/v1/ingestions)"})
	}
	bf = append(bf, adaptertest.ValidateSync(cs.batches)...)
	adaptertest.SortFindings(bf)
	return adaptertest.Report{Adapter: adapterName, Suites: []adaptertest.Suite{
		{Name: "manifest", Checks: []string{adaptertest.CheckManifest}, Findings: mf},
		{Name: "pushes", Checks: listenChecks, Findings: bf},
	}}
}

// serveConformance serves cs on ln until ctx is done, then reports.
func serveConformance(ctx context.Context, ln net.Listener, cs *conformanceServer, env Env, junitPath string) int {
	srv := &http.Server{Handler: cs, ReadHeaderTimeout: 10 * time.Second}
	fmt.Fprintf(env.Stdout, "conformance endpoint listening on http://%s: register the manifest, push the batches of one sync, then stop with Ctrl+C\n", ln.Addr())
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
	case err := <-errc:
		return fail(env, "%v", err)
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	return finish(env, cs.report(), junitPath)
}
