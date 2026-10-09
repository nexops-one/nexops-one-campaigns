// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/nexops-one/compliance-engine/catalogs"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// paramProfile echoes its parameters into its facts and blocks unless
// allowed to be incomplete when "block" is set.
type paramProfile struct{ seen *extension.ReportInput }

func (paramProfile) ID() string                 { return "params" }
func (paramProfile) Feature() extension.Feature { return extension.FeatureReportProfileB }
func (paramProfile) Formats() []string          { return nil }
func (paramProfile) Parameters() []extension.ParameterSpec {
	return []extension.ParameterSpec{
		{Name: "reference_date", Label: "Reference date", Pattern: `\d{4}-\d{2}-\d{2}`},
		{Name: "block", Label: "Block"},
	}
}

func (p paramProfile) Generate(_ context.Context, in extension.ReportInput) (extension.ReportOutput, error) {
	*p.seen = in
	if in.Parameters["reference_date"] == "1999-01-01" {
		return extension.ReportOutput{}, errors.Join(extension.ErrInvalidParameter, errors.New("reference_date precedes the register"))
	}
	facts, _ := json.Marshal(map[string]any{"parameters": in.Parameters, "incomplete": in.AllowIncomplete})
	out := extension.ReportOutput{Facts: facts, Complete: true}
	if in.Parameters["block"] == "yes" {
		out.Complete = false
		if !in.AllowIncomplete {
			out.Blocking = []extension.Finding{{Severity: "error", Code: "required_missing", Message: "x"}}
		}
	}
	return out, nil
}

func TestReportParameters(t *testing.T) {
	var seen extension.ReportInput
	eng := newEngine(t, func(c *compliance.Config) {
		c.Extensions.ReportProfiles = []extension.ReportProfile{paramProfile{&seen}}
	})
	loadSample(t, eng)
	profiles, _ := eng.ReportProfiles(ctx, scopeA)
	if len(profiles) != 2 || profiles[0].ID != "params" || len(profiles[0].Parameters) != 2 || len(profiles[1].Parameters) != 0 {
		t.Fatalf("profiles = %+v", profiles)
	}
	for name, params := range map[string]map[string]string{
		"unknown parameter": {"nope": "x"},
		"pattern mismatch":  {"reference_date": "31/12/2026"},
		"refused by data":   {"reference_date": "1999-01-01"},
	} {
		if _, err := eng.GenerateReport(ctx, scopeA, compliance.ReportRequest{Profile: "params", Parameters: params}); !errors.Is(err, extension.ErrInvalidParameter) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := eng.GenerateReport(ctx, scopeA, compliance.ReportRequest{Profile: "profile_b", Parameters: map[string]string{"x": "y"}}); !errors.Is(err, extension.ErrInvalidParameter) {
		t.Fatalf("a profile without parameters accepts none: %v", err)
	}
	params := map[string]string{"reference_date": "2026-12-31", "block": "yes"}
	var inc *compliance.ExportIncompleteError
	if _, err := eng.GenerateReport(ctx, scopeA, compliance.ReportRequest{Profile: "params", Parameters: params}); !errors.As(err, &inc) {
		t.Fatalf("blocked export = %v", err)
	}
	r, err := eng.GenerateReport(ctx, scopeA, compliance.ReportRequest{Profile: "params", Parameters: params, AllowIncomplete: true})
	if err != nil || r.Complete {
		t.Fatalf("incomplete export = %+v, %v", r, err)
	}
	if seen.Parameters["reference_date"] != "2026-12-31" || !seen.AllowIncomplete {
		t.Fatalf("profile input = %+v", seen.Parameters)
	}
	stored, _ := eng.Report(ctx, scopeA, r.ID)
	if !bytes.Contains(stored.Facts, []byte(`"reference_date":"2026-12-31"`)) {
		t.Fatalf("facts = %s", stored.Facts)
	}
	// Regeneration uses the stored parameters and reproduces the report.
	seen = extension.ReportInput{}
	reg, err := eng.RegenerateReport(ctx, scopeA, r.ID)
	if err != nil || !reg.Identical || seen.Parameters["reference_date"] != "2026-12-31" || !seen.AllowIncomplete {
		t.Fatalf("regeneration = %+v, %v (input %+v)", reg, err, seen.Parameters)
	}
}

// maintainedOnly allows the open features, and catalog.maintained for ws-2.
type maintainedOnly struct{}

func (maintainedOnly) Allowed(ctx context.Context, sc adapter.Scope, f extension.Feature) extension.Decision {
	if f == extension.FeatureCatalogMaintained && sc == scopeB {
		return extension.Decision{Allowed: true}
	}
	return extension.AllowOpen{}.Allowed(ctx, sc, f)
}

func TestMaintainedCatalogNeedsEntitlement(t *testing.T) {
	dora, err := catalogs.FS.ReadFile("dora/1.0.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	feed := catalog.FSSource{FS: fstest.MapFS{"dora/1.9.0.yaml": {Data: bytes.Replace(dora, []byte("version: 1.0.0"), []byte("version: 1.9.0"), 1)}},
		Origin: "feed:test", Feature: string(extension.FeatureCatalogMaintained)}
	eng := newEngine(t, func(c *compliance.Config) {
		c.Catalogs, c.Entitlements = catalog.Sources{catalog.Embedded(), feed}, maintainedOnly{}
	})
	b := sampleBatch(t)
	for _, sc := range []adapter.Scope{scopeA, scopeB} {
		if err := eng.RegisterManifest(ctx, sc, manifestFor(b)); err != nil {
			t.Fatal(err)
		}
		if _, err := eng.Ingest(ctx, sc, b); err != nil {
			t.Fatal(err)
		}
	}
	doraOf := func(ev compliance.Evaluation) string {
		for _, r := range ev.Catalogs {
			if r.Catalog == "dora" {
				return r.Version
			}
		}
		return ""
	}
	open, err := eng.Evaluate(ctx, scopeA, "", nil)
	if err != nil || doraOf(open) == "1.9.0" || doraOf(open) == "" {
		t.Fatalf("a workspace without the entitlement must get the latest open dora: %v %v", open.Catalogs, err)
	}
	maint, err := eng.Evaluate(ctx, scopeB, "", nil)
	if err != nil || doraOf(maint) != "1.9.0" {
		t.Fatalf("an entitled workspace gets the maintained version: %v %v", maint.Catalogs, err)
	}
	var ne *extension.NotEntitledError
	if _, err := eng.Evaluate(ctx, scopeA, "", []catalog.Ref{{Catalog: "dora", Version: "1.9.0"}}); !errors.As(err, &ne) || ne.Feature != extension.FeatureCatalogMaintained {
		t.Fatalf("explicit maintained catalog = %v", err)
	}
	if _, err := eng.Status(ctx, scopeA, nil); err != nil {
		t.Fatalf("status = %v", err)
	}
	for _, info := range eng.CatalogInfos(ctx, scopeA) {
		if info.Ref.Version == "1.9.0" && (info.Entitled || info.Origin != "feed:test" || info.Feature != "catalog.maintained") {
			t.Fatalf("info = %+v", info)
		}
		if info.Ref.Catalog == "dora" && info.Ref.Version == "1.0.0" && (!info.Entitled || info.Origin != "embedded") {
			t.Fatalf("info = %+v", info)
		}
	}
	// Reading the maintained catalog is never gated.
	if c, err := eng.Catalog(catalog.Ref{Catalog: "dora", Version: "1.9.0"}); err != nil || c.Origin != "feed:test" {
		t.Fatalf("catalog read = %v", err)
	}
}
