// SPDX-License-Identifier: Apache-2.0

package adaptertest

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// DefaultScope is the scope passed to Pull when a fixture sets none.
var DefaultScope = adapter.Scope{TenantID: "conformance", WorkspaceID: "conformance"}

// Fixture is one conformance scenario. The adapter under test is configured
// with the fixture's input by the caller; the fixture says how to pull and
// what the input implies.
type Fixture struct {
	Name string
	// Request is passed to Pull twice. An empty Scope becomes DefaultScope and
	// an empty Mode the manifest's first mode.
	Request adapter.SyncRequest
	// Derived lists, per entity, the fields this input makes the adapter infer
	// (for example a country deduced from a cloud region). Every emitted value
	// of such a field must be declared in _meta.derived_fields.
	Derived map[string][]string
}

// Suite is one group of checks: the manifest, or one fixture or batch set.
type Suite struct {
	Name     string
	Checks   []string // checks performed, in report order
	Findings []Finding
}

// CheckResult is the outcome of one check within a suite.
type CheckResult struct {
	Check    string
	Failures []Finding
	Warnings []Finding
}

// Results groups the suite's findings by check, in Checks order.
func (s Suite) Results() []CheckResult {
	out := make([]CheckResult, 0, len(s.Checks))
	for _, c := range s.Checks {
		r := CheckResult{Check: c}
		for _, f := range s.Findings {
			switch {
			case f.Check != c:
			case f.Warning:
				r.Warnings = append(r.Warnings, f)
			default:
				r.Failures = append(r.Failures, f)
			}
		}
		out = append(out, r)
	}
	return out
}

// Report is the outcome of a conformance run.
type Report struct {
	Adapter string
	Suites  []Suite
}

// Failed reports whether any check failed (warnings do not fail).
func (r Report) Failed() bool {
	for _, s := range r.Suites {
		if len(Failures(s.Findings)) > 0 {
			return true
		}
	}
	return false
}

// SortFindings orders findings by check (report order), keeping the order of
// findings within a check.
func SortFindings(fs []Finding) {
	order := append([]string{CheckManifest}, BatchChecks...)
	sort.SliceStable(fs, func(i, j int) bool {
		return slices.Index(order, fs[i].Check) < slices.Index(order, fs[j].Check)
	})
}

// Evaluate runs the suite against an in-process adapter: the manifest, then
// each fixture pulled twice (the second pull checks idempotency). Without
// fixtures, one fixture per manifest mode is used.
func Evaluate(ctx context.Context, a adapter.Adapter, fixtures ...Fixture) Report {
	m := a.Manifest()
	r := Report{Adapter: m.Name}
	reg, err := schema.Default()
	if err != nil {
		r.Suites = append(r.Suites, Suite{Name: "manifest", Checks: []string{CheckManifest}, Findings: []Finding{{Check: CheckManifest, Index: -1, Message: err.Error()}}})
		return r
	}
	r.Suites = append(r.Suites, Suite{Name: "manifest", Checks: []string{CheckManifest}, Findings: ValidateManifest(reg, m)})
	if len(fixtures) == 0 {
		for _, mode := range m.Modes {
			fixtures = append(fixtures, Fixture{Name: string(mode), Request: adapter.SyncRequest{Mode: mode}})
		}
	}
	for i, fx := range fixtures {
		r.Suites = append(r.Suites, evaluateFixture(ctx, reg, a, m, i, fx))
	}
	return r
}

func evaluateFixture(ctx context.Context, reg *schema.Registry, a adapter.Adapter, m adapter.Manifest, i int, fx Fixture) Suite {
	req := fx.Request
	if req.Scope == (adapter.Scope{}) {
		req.Scope = DefaultScope
	}
	if req.Mode == "" && len(m.Modes) > 0 {
		req.Mode = m.Modes[0]
	}
	name := fx.Name
	if name == "" {
		name = fmt.Sprintf("fixture %d (%s)", i+1, req.Mode)
	}
	s := Suite{Name: name, Checks: BatchChecks}
	first, err := a.Pull(ctx, req)
	if err != nil {
		s.Findings = []Finding{{Check: CheckPull, Batch: "run 1", Index: -1, Message: "pull failed: " + err.Error()}}
		return s
	}
	if first.Mode() != req.Mode {
		s.Findings = append(s.Findings, Finding{Check: CheckPull, Batch: "run 1", Index: -1, Field: "batch.mode",
			Message: fmt.Sprintf("the adapter was asked for mode %q but returned a %q batch", req.Mode, first.Mode())})
	}
	s.Findings = append(s.Findings, Label(ValidateBatch(reg, m, first, fx.Derived), "run 1")...)
	second, err := a.Pull(ctx, req)
	if err != nil {
		s.Findings = append(s.Findings, Finding{Check: CheckPull, Batch: "run 2", Index: -1, Message: "pull failed: " + err.Error()})
	} else {
		s.Findings = append(s.Findings, CompareRuns(reg, m, []adapter.Batch{first}, []adapter.Batch{second})...)
	}
	SortFindings(s.Findings)
	return s
}

// Run is the Go test harness: it evaluates the adapter and reports each suite
// as a subtest. Failures fail the test; warnings are logged.
func Run(t *testing.T, a adapter.Adapter, fixtures ...Fixture) {
	t.Helper()
	r := Evaluate(t.Context(), a, fixtures...)
	for _, s := range r.Suites {
		t.Run(s.Name, func(t *testing.T) {
			for _, f := range s.Findings {
				if f.Warning {
					t.Log(f.String())
				} else {
					t.Error(f.String())
				}
			}
		})
	}
}
