// SPDX-License-Identifier: Apache-2.0

package dataset_test

import (
	"bytes"
	"context"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/nexops-one/compliance-engine/demo/dataset"
	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/importer"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var (
	ctx   = context.Background()
	scope = adapter.Scope{TenantID: "demo", WorkspaceID: "sample"}
)

// realLEIs are LEIs of real, well-known legal entities. No demo identifier
// may ever equal one of them.
var realLEIs = []string{
	"HWUPKR0MPOU8FGXBT394", // Apple Inc.
	"7LTWFZYICNSX8D621K86", // Deutsche Bank AG
	"8I5DZWZKVSZI1NUHU748", // JPMorgan Chase & Co.
	"784F5XWPLTWKTBV3E584", // The Goldman Sachs Group, Inc.
	"R0MUWSFPU8MPRO8K5P83", // BNP Paribas
}

func TestNoRealLEIs(t *testing.T) {
	for _, l := range dataset.LEIs() {
		if !strings.HasPrefix(l, "DEMO00") || len(l) != 20 {
			t.Errorf("%s is not a DEMO LEI", l)
		}
		for _, real := range realLEIs {
			if l == real {
				t.Errorf("%s is a real LEI", l)
			}
		}
	}
	valid := 0
	for _, l := range dataset.LEIs() {
		if canonical.ValidLEI(l) {
			valid++
		}
	}
	if valid != len(dataset.LEIs())-1 {
		t.Fatalf("every LEI but the deliberate fault has valid check digits: %d of %d", valid, len(dataset.LEIs()))
	}
}

func workbook(t *testing.T, eng *compliance.Engine) []byte {
	t.Helper()
	data, err := dataset.Workbook(eng.Schema())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDatasetDeterministic(t *testing.T) {
	eng, _ := compliance.New(ctx, compliance.Config{Store: memory.New()})
	if a, b := workbook(t, eng), workbook(t, eng); !bytes.Equal(a, b) {
		t.Fatal("two generations differ")
	}
}

func TestSampleMarking(t *testing.T) {
	eng, _ := compliance.New(ctx, compliance.Config{Store: memory.New()})
	f, err := excelize.OpenReader(bytes.NewReader(workbook(t, eng)))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	a1, _ := f.GetCellValue(importer.ReadmeSheet, "A1")
	props, _ := f.GetDocProps()
	if !strings.HasPrefix(a1, dataset.Marker) || !strings.HasPrefix(props.Title, "SAMPLE DATA") || props.Created != "2026-10-01T00:00:00Z" {
		t.Fatalf("README A1 %q, title %q, created %q", a1, props.Title, props.Created)
	}
}

func TestDeliberateFaults(t *testing.T) {
	eng, err := compliance.New(ctx, compliance.Config{Store: memory.New()})
	if err != nil {
		t.Fatal(err)
	}
	res, err := eng.Import(ctx, scope, importer.Input{Name: "sample-register.xlsx", Data: workbook(t, eng)}, true)
	if err != nil {
		t.Fatal(err)
	}
	r := res.Result
	var got []string
	for _, e := range r.Errors {
		got = append(got, e.SourceRecordRef+" "+e.Field+" "+e.Code)
	}
	var warns []string
	for _, w := range r.Warnings {
		warns = append(warns, w.SourceRecordRef+" "+w.Field+" "+w.Code)
	}
	sort.Strings(got)
	var want, wantWarn []string
	for _, f := range dataset.Faults {
		line := f.Entity + ":" + strconv.Itoa(f.Row) + " " + f.Field + " " + f.Code
		if f.Rejects {
			want = append(want, line)
		} else {
			wantWarn = append(wantWarn, line)
		}
	}
	sort.Strings(want)
	if r.RejectedRecords != 2 || strings.Join(got, "|") != strings.Join(want, "|") || strings.Join(warns, "|") != strings.Join(wantWarn, "|") {
		t.Fatalf("rejected %d\nerrors   %q\nwant     %q\nwarnings %q\nwant     %q", r.RejectedRecords, got, want, warns, wantWarn)
	}
	gaps := map[string]int{}
	for _, g := range res.Completeness.Gaps {
		gaps[g.Entity+"."+g.Field]++
	}
	if gaps["arrangement_service_line.data_at_rest_country"] != 3 || gaps["arrangement_service_line.data_processing_country"] != 3 ||
		gaps["service_assessment.exit_plan_exists"] != 3 || len(gaps) != 3 {
		t.Fatalf("export-required gaps = %v", gaps)
	}
	if n := len(res.Completeness.References.Dangling) + len(res.Completeness.References.Cycles); n != 0 {
		t.Fatalf("references = %+v", res.Completeness.References)
	}
}

func TestDemoStatuses(t *testing.T) {
	eng, err := compliance.New(ctx, compliance.Config{Store: memory.New()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Import(ctx, scope, importer.Input{Name: "sample-register.xlsx", Data: workbook(t, eng)}, false); err != nil {
		t.Fatal(err)
	}
	res, err := eng.Status(ctx, scope, nil)
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]engine.Status{}
	for _, fw := range res.Frameworks {
		if fw.Catalog.Catalog == "dora" {
			for _, c := range fw.Controls {
				status[c.ControlID] = c.ComputedStatus
			}
		}
	}
	for id, want := range map[string]engine.Status{
		"dora-roi-provider-identification": engine.StatusMonitoring,
		"dora-roi-data-location":           engine.StatusNotAssessed,
		"dora-exit-plans":                  engine.StatusNotAssessed,
		"dora-incident-readiness":          engine.StatusNotAssessed,
		"dora-supply-chain-visibility":     engine.StatusMonitoring,
	} {
		if status[id] != want {
			t.Errorf("%s = %s, want %s", id, status[id], want)
		}
	}
	t.Logf("DORA: %+v", status)
}
