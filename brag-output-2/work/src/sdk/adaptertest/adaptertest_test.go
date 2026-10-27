// SPDX-License-Identifier: Apache-2.0

package adaptertest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/adaptertest"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

var ctx = context.Background()

func testManifest() adapter.Manifest {
	return adapter.Manifest{
		Name: "fake", Version: "1.0.0", SchemaVersion: "0.1.0",
		Supplies: map[string][]string{
			"ict_provider":   {"provider_id_code", "legal_name", "hq_country", "total_annual_cost"},
			"cloud_resource": {"resource_ref", "region", "country", "provider_id_code"},
		},
		Modes: []adapter.Mode{adapter.ModeFull, adapter.ModeIncremental},
	}
}

var derived = map[string][]string{"cloud_resource": {"country"}}

func provider(code, name, country string) adapter.Record {
	rec := adapter.Record{"provider_id_code": code, "legal_name": name, "hq_country": country}
	adapter.SetSourceRef(rec, "prov/"+code)
	return rec
}

func resource(ref, region, country string) adapter.Record {
	rec := adapter.Record{"resource_ref": ref, "region": region, "country": country}
	adapter.MarkDerived(rec, adapter.DerivedField{Field: "country", Method: "region_lookup", SourceRef: region})
	return rec
}

func batch(mode adapter.Mode, recs map[string][]adapter.Record) adapter.Batch {
	b := adapter.NewBuilder(testManifest(), "fake-system").Mode(mode)
	for _, e := range []string{"ict_provider", "cloud_resource", "function"} {
		for _, r := range recs[e] {
			b.Add(e, r)
		}
	}
	return b.Build()
}

func good(mode adapter.Mode) adapter.Batch {
	return batch(mode, map[string][]adapter.Record{
		"ict_provider":   {provider("P1", "One Ltd", "IE")},
		"cloud_resource": {resource("r-1", "eu-west-1", "IE")},
	})
}

type fake struct {
	m     adapter.Manifest
	pulls int
	last  adapter.SyncRequest
	pull  func(n int, req adapter.SyncRequest) (adapter.Batch, error)
}

func (f *fake) Manifest() adapter.Manifest { return f.m }

func (f *fake) Pull(_ context.Context, req adapter.SyncRequest) (adapter.Batch, error) {
	f.pulls++
	f.last = req
	return f.pull(f.pulls, req)
}

func goodAdapter() *fake {
	return &fake{m: testManifest(), pull: func(_ int, req adapter.SyncRequest) (adapter.Batch, error) { return good(req.Mode), nil }}
}

func registry(t *testing.T) *schema.Registry {
	t.Helper()
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// count returns the number of failures (not warnings) per check.
func count(fs []adaptertest.Finding) map[string]int {
	out := map[string]int{}
	for _, f := range adaptertest.Failures(fs) {
		out[f.Check]++
	}
	return out
}

func find(fs []adaptertest.Finding, check, entity string, index int, field string) *adaptertest.Finding {
	for i, f := range fs {
		if f.Check == check && f.Entity == entity && f.Index == index && f.Field == field {
			return &fs[i]
		}
	}
	return nil
}

func text(t *testing.T, r adaptertest.Report) string {
	t.Helper()
	var b strings.Builder
	if err := adaptertest.WriteText(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestConformingAdapterPasses(t *testing.T) {
	a := goodAdapter()
	r := adaptertest.Evaluate(ctx, a, adaptertest.Fixture{Name: "full sync", Request: adapter.SyncRequest{Mode: adapter.ModeFull}, Derived: derived})
	if r.Failed() {
		t.Fatalf("conforming adapter failed:\n%s", text(t, r))
	}
	if a.pulls != 2 || a.last.Scope != adaptertest.DefaultScope || a.last.Mode != adapter.ModeFull {
		t.Fatalf("pulls = %d, last request = %+v", a.pulls, a.last)
	}
	if r.Adapter != "fake" || len(r.Suites) != 2 || r.Suites[0].Name != "manifest" || r.Suites[1].Name != "full sync" {
		t.Fatalf("report = %+v", r)
	}
}

func TestDefaultFixturesCoverEveryMode(t *testing.T) {
	a := goodAdapter()
	r := adaptertest.Evaluate(ctx, a)
	var names []string
	for _, s := range r.Suites {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "manifest,full,incremental" || a.pulls != 4 {
		t.Fatalf("suites = %v, pulls = %d", names, a.pulls)
	}
}

func TestValidateBatchFindings(t *testing.T) {
	withCurrency := provider("P3", "Three SA", "FR")
	withCurrency["currency"] = "EUR"
	b := batch(adapter.ModeFull, map[string][]adapter.Record{
		"ict_provider": {
			provider("P1", "One Ltd", "IE"),
			provider("P1", "One again", "IE"),
			provider("P2", "Two Inc", "Ireland"),
			withCurrency,
		},
		"cloud_resource": {{"resource_ref": "r-2", "region": "eu-west-1", "country": "IE"}},
		"function":       {{"function_id": "F-1"}},
	})
	b.Source.Adapter = "other"
	fs := adaptertest.ValidateBatch(registry(t), testManifest(), b, derived)
	got := count(fs)
	for check, want := range map[string]int{
		adaptertest.CheckIdentity: 1, adaptertest.CheckOutside: 2, adaptertest.CheckDerived: 1,
		adaptertest.CheckManifestMatch: 1, adaptertest.CheckEnvelope: 0, adaptertest.CheckFullScope: 0,
	} {
		if got[check] != want {
			t.Errorf("%s = %d, want %d\n%v", check, got[check], want, fs)
		}
	}
	if f := find(fs, adaptertest.CheckIdentity, "ict_provider", 1, ""); f == nil || !strings.Contains(f.Message, "record 0") || f.SourceRecordRef != "prov/P1" {
		t.Errorf("identity finding = %+v", f)
	}
	if find(fs, adaptertest.CheckRecords, "ict_provider", 2, "hq_country") == nil {
		t.Errorf("missing L1 finding for hq_country: %v", fs)
	}
	if find(fs, adaptertest.CheckOutside, "ict_provider", 3, "currency") == nil || find(fs, adaptertest.CheckOutside, "function", -1, "") == nil {
		t.Errorf("missing outside_manifest findings: %v", fs)
	}
	if find(fs, adaptertest.CheckDerived, "cloud_resource", 0, "country") == nil {
		t.Errorf("missing derived_fields finding: %v", fs)
	}
	if find(fs, adaptertest.CheckManifestMatch, "", -1, "source.adapter") == nil {
		t.Errorf("missing manifest_match finding: %v", fs)
	}
}

func TestFullBatchWithoutASuppliedEntityWarns(t *testing.T) {
	only := map[string][]adapter.Record{"ict_provider": {provider("P1", "One Ltd", "IE")}}
	fs := adaptertest.ValidateBatch(registry(t), testManifest(), batch(adapter.ModeFull, only), derived)
	if len(adaptertest.Failures(fs)) != 0 || len(fs) != 1 || !fs[0].Warning || fs[0].Check != adaptertest.CheckFullScope || fs[0].Entity != "cloud_resource" {
		t.Fatalf("full batch findings = %v", fs)
	}
	if fs := adaptertest.ValidateBatch(registry(t), testManifest(), batch(adapter.ModeIncremental, only), derived); len(fs) != 0 {
		t.Fatalf("incremental batch findings = %v", fs)
	}
}

func TestValidateSyncRejectsSplitFullSync(t *testing.T) {
	fs := adaptertest.ValidateSync([]adapter.Batch{good(adapter.ModeFull), good(adapter.ModeFull)})
	if len(fs) != 1 || fs[0].Check != adaptertest.CheckFullScope || fs[0].Warning {
		t.Fatalf("split full sync = %v", fs)
	}
	if fs := adaptertest.ValidateSync([]adapter.Batch{good(adapter.ModeIncremental), good(adapter.ModeIncremental)}); len(fs) != 0 {
		t.Fatalf("incremental parts = %v", fs)
	}
}

func TestCompareRuns(t *testing.T) {
	reg := registry(t)
	first := good(adapter.ModeFull)
	first.Entities["ict_provider"][0]["total_annual_cost"] = 12
	renamed := provider("P1", "One Limited", "IE")
	renamed["total_annual_cost"] = json.Number("12")
	second := batch(adapter.ModeFull, map[string][]adapter.Record{"ict_provider": {renamed}})
	fs := adaptertest.CompareRuns(reg, testManifest(), []adapter.Batch{first}, []adapter.Batch{second})
	if len(fs) != 2 {
		t.Fatalf("findings = %v", fs)
	}
	if f := find(fs, adaptertest.CheckIdempotency, "ict_provider", 0, ""); f == nil || !strings.Contains(f.Message, "legal_name") || strings.Contains(f.Message, "total_annual_cost") {
		t.Errorf("update finding = %+v", f)
	}
	if f := find(fs, adaptertest.CheckIdempotency, "cloud_resource", 0, ""); f == nil || !strings.Contains(f.Message, "deleted") || f.Batch != "run 1" {
		t.Errorf("delete finding = %+v", f)
	}
	same := batch(adapter.ModeIncremental, map[string][]adapter.Record{"ict_provider": {provider("P1", "One Ltd", "IE")}})
	if fs := adaptertest.CompareRuns(reg, testManifest(), []adapter.Batch{good(adapter.ModeIncremental)}, []adapter.Batch{same}); len(fs) != 0 {
		t.Fatalf("an incremental batch may omit records: %v", fs)
	}
}

func TestEvaluateReportsPullProblems(t *testing.T) {
	drifting := &fake{m: testManifest(), pull: func(n int, req adapter.SyncRequest) (adapter.Batch, error) {
		b := good(req.Mode)
		b.Entities["ict_provider"][0]["legal_name"] = fmt.Sprintf("One Ltd (sync %d)", n)
		return b, nil
	}}
	r := adaptertest.Evaluate(ctx, drifting, adaptertest.Fixture{Name: "inc", Request: adapter.SyncRequest{Mode: adapter.ModeIncremental}, Derived: derived})
	if f := find(r.Suites[1].Findings, adaptertest.CheckIdempotency, "ict_provider", 0, ""); f == nil || !strings.Contains(f.Message, "legal_name") {
		t.Fatalf("non-determinism not reported:\n%s", text(t, r))
	}
	broken := &fake{m: testManifest(), pull: func(int, adapter.SyncRequest) (adapter.Batch, error) {
		return adapter.Batch{}, errors.New("source unreachable")
	}}
	r = adaptertest.Evaluate(ctx, broken, adaptertest.Fixture{Name: "full", Request: adapter.SyncRequest{Mode: adapter.ModeFull}})
	if !r.Failed() || !strings.Contains(text(t, r), "source unreachable") {
		t.Fatalf("pull error not reported:\n%s", text(t, r))
	}
	wrongMode := &fake{m: testManifest(), pull: func(int, adapter.SyncRequest) (adapter.Batch, error) { return good(adapter.ModeFull), nil }}
	r = adaptertest.Evaluate(ctx, wrongMode, adaptertest.Fixture{Name: "inc", Request: adapter.SyncRequest{Mode: adapter.ModeIncremental}, Derived: derived})
	if count(r.Suites[1].Findings)[adaptertest.CheckPull] != 1 {
		t.Fatalf("mode mismatch not reported:\n%s", text(t, r))
	}
}

func TestValidateManifest(t *testing.T) {
	reg := registry(t)
	if fs := adaptertest.ValidateManifest(reg, testManifest()); len(fs) != 0 {
		t.Fatalf("valid manifest: %v", fs)
	}
	bad := adapter.Manifest{Name: "my adapter", Version: "1", SchemaVersion: "0.1.0",
		Supplies: map[string][]string{"ict_provider": {"legal_name"}}, Modes: []adapter.Mode{"sometimes"}}
	fs := adaptertest.ValidateManifest(reg, bad)
	if find(fs, adaptertest.CheckManifest, "", -1, "name") == nil || find(fs, adaptertest.CheckManifest, "", -1, "modes.0") == nil {
		t.Fatalf("schema findings missing: %v", fs)
	}
	var identity bool
	for _, f := range fs {
		identity = identity || strings.Contains(f.Message, "identity field provider_id_code")
	}
	if !identity {
		t.Fatalf("registry finding missing: %v", fs)
	}
}

func TestReports(t *testing.T) {
	a := &fake{m: testManifest(), pull: func(_ int, req adapter.SyncRequest) (adapter.Batch, error) {
		b := good(req.Mode)
		b.Entities["ict_provider"][0]["hq_country"] = "Ireland"
		return b, nil
	}}
	r := adaptertest.Evaluate(ctx, a, adaptertest.Fixture{Name: "full sync", Request: adapter.SyncRequest{Mode: adapter.ModeFull}, Derived: derived})
	out := text(t, r)
	for _, want := range []string{"conformance: adapter fake", "suite full sync", "  PASS identity", "  FAIL records", "ict_provider[0] (prov/P1) hq_country", "result: FAIL (1 failure(s), 0 warning(s))"} {
		if !strings.Contains(out, want) {
			t.Errorf("text report lacks %q:\n%s", want, out)
		}
	}
	var buf bytes.Buffer
	if err := adaptertest.WriteJUnit(&buf, r); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Tests    int `xml:"tests,attr"`
		Failures int `xml:"failures,attr"`
		Suites   []struct {
			Name  string `xml:"name,attr"`
			Cases []struct {
				Name      string    `xml:"name,attr"`
				Classname string    `xml:"classname,attr"`
				Failure   *struct{} `xml:"failure"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("junit: %v\n%s", err, buf.String())
	}
	if doc.Tests != 1+len(adaptertest.BatchChecks) || doc.Failures != 1 || len(doc.Suites) != 2 {
		t.Fatalf("junit totals = %+v", doc)
	}
	c := doc.Suites[1].Cases[3]
	if c.Name != adaptertest.CheckRecords || c.Classname != "conformance.full_sync" || c.Failure == nil {
		t.Fatalf("records case = %+v", c)
	}
}

func TestRunPassesForConformingAdapter(t *testing.T) {
	adaptertest.Run(t, goodAdapter(), adaptertest.Fixture{Name: "full", Request: adapter.SyncRequest{Mode: adapter.ModeFull}, Derived: derived})
}
