// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
)

// Evaluate evaluates every control of every catalog against the snapshot.
func Evaluate(in Input) Result {
	res := Result{SnapshotID: in.SnapshotID, SchemaVersion: in.Index.Schema().Version, Frameworks: []FrameworkResult{}}
	var overall Tally
	for _, c := range in.Catalogs {
		fr := FrameworkResult{Catalog: c.Ref(), Framework: c.Framework, Controls: []ControlResult{}}
		for _, ctl := range c.Controls {
			fr.Controls = append(fr.Controls, evaluateControl(in, ctl))
		}
		fr.Tally = TallyOf(fr.Controls, c.Scoring)
		overall = overall.add(fr.Tally)
		res.Frameworks = append(res.Frameworks, fr)
	}
	res.Overall = overall.finish()
	return res
}

func evaluateControl(in Input, ctl catalog.Control) ControlResult {
	r := ControlResult{
		ControlID: ctl.ID, Title: ctl.Title,
		Explanation: Explanation{Rule: cloneRule(ctl.Rule), Inputs: append([]string(nil), ctl.Requires...), RecordsExamined: []string{}},
	}
	if ctl.Rule.Kind == catalog.KindManual {
		r.Status = StatusNotAssessed
		r.Blockers = []Blocker{{Reason: ReasonManual}}
		return r
	}
	entity := ctl.Rule.Entity
	fields := make([]string, 0, len(ctl.Requires))
	for _, p := range ctl.Requires {
		fields = append(fields, strings.TrimPrefix(p, entity+"."))
	}
	var blockers []Blocker
	var selected []canonical.SnapshotRecord
	for _, rec := range in.Index.Records(entity) {
		view := canonical.NewView(rec.Data)
		match, filterBlockers := matchesFilter(in, ctl, rec, view)
		blockers = append(blockers, filterBlockers...)
		if !match {
			continue
		}
		selected = append(selected, rec)
		r.Explanation.RecordsExamined = append(r.Explanation.RecordsExamined, rec.Key)
		for _, f := range fields {
			if b, blocked := gate(in, ctl, rec, view, f); blocked {
				blockers = append(blockers, b)
			}
		}
	}
	switch {
	case len(blockers) > 0:
		sort.Slice(blockers, func(i, j int) bool { return blockerLess(blockers[i], blockers[j]) })
		r.Status, r.Blockers = StatusNotAssessed, blockers
	case len(selected) == 0:
		r.Status, r.Blockers = StatusNotAssessed, []Blocker{{Reason: ReasonNoRecords, Entity: entity}}
	default:
		ok, failing, compared := applyRule(ctl.Rule, selected)
		r.Explanation.Failing = failing
		switch {
		case compared == 0:
			r.Status, r.Blockers = StatusNotAssessed, []Blocker{{Reason: ReasonNoRecords, Entity: entity}}
		case ok:
			r.Status = StatusMonitoring
		default:
			r.Status, r.Attention = StatusInReview, AttentionRuleFailed
		}
	}
	return r
}

// cloneRule copies the rule's slices so results never alias the catalog.
func cloneRule(r catalog.Rule) catalog.Rule {
	r.Fields = append([]string(nil), r.Fields...)
	r.Values = append([]any(nil), r.Values...)
	filter := make([]catalog.Condition, len(r.Filter))
	for i, c := range r.Filter {
		c.In = append([]any(nil), c.In...)
		filter[i] = c
	}
	if r.Filter != nil {
		r.Filter = filter
	}
	return r
}

// gate decides whether one required field of one record blocks assessment.
func gate(in Input, ctl catalog.Control, rec canonical.SnapshotRecord, view canonical.View, field string) (Blocker, bool) {
	b := Blocker{Entity: rec.Entity, Key: rec.Key, Field: field}
	switch view.State(field) {
	case canonical.StateMissing:
		b.Reason = ReasonMissing
		if in.Supplied != nil && !in.Supplied(rec.Entity, field) {
			b.Reason = ReasonNotSupplied
		}
		return b, true
	case canonical.StateNotApplicable:
		return b, false
	case canonical.StateDerived:
		if !ctl.AcceptDerived {
			b.Reason = ReasonDerived
			return b, true
		}
	}
	if in.References.IsDangling(rec.Entity, rec.Key, field) {
		b.Reason = ReasonDangling
		return b, true
	}
	return b, false
}

// matchesFilter applies the rule filter. A record whose filter field is
// missing cannot be classified and blocks the control.
func matchesFilter(in Input, ctl catalog.Control, rec canonical.SnapshotRecord, view canonical.View) (bool, []Blocker) {
	var blockers []Blocker
	match := true
	for _, c := range ctl.Rule.Filter {
		switch view.State(c.Field) {
		case canonical.StateMissing:
			if b, blocked := gate(in, ctl, rec, view, c.Field); blocked {
				blockers = append(blockers, b)
			}
			match = false
		case canonical.StateNotApplicable:
			match = false
		case canonical.StateDerived:
			if !ctl.AcceptDerived {
				// an unconfirmed value can neither select nor exclude the record
				if b, blocked := gate(in, ctl, rec, view, c.Field); blocked {
					blockers = append(blockers, b)
				}
				match = false
				continue
			}
			fallthrough
		default:
			want := c.In
			if c.Equals != nil {
				want = []any{c.Equals}
			}
			if !containsValue(want, rec.Data[c.Field]) {
				match = false
			}
		}
	}
	return match, blockers
}

// applyRule evaluates the rule over records that passed the gate. compared
// is the number of records the rule actually judged; zero means nothing was
// assessed, which must never count as a pass.
func applyRule(rule catalog.Rule, selected []canonical.SnapshotRecord) (ok bool, failing []string, compared int) {
	switch rule.Kind {
	case catalog.KindRecordsExist:
		min := rule.Min
		if min < 1 {
			min = 1
		}
		return len(selected) >= min, nil, len(selected)
	case catalog.KindFieldEquals, catalog.KindFieldIn:
		want := rule.Values
		if rule.Kind == catalog.KindFieldEquals {
			want = []any{rule.Value}
		}
		for _, rec := range selected {
			if canonical.NewView(rec.Data).State(rule.Field) == canonical.StateNotApplicable {
				continue // missing values were blocked by the gate
			}
			compared++
			if !containsValue(want, rec.Data[rule.Field]) {
				failing = append(failing, rec.Key)
			}
		}
		return len(failing) == 0, failing, compared
	default: // fields_complete, references_resolved: decided entirely by the gate
		return true, nil, len(selected)
	}
}

// containsValue compares JSON encodings, so 1, 1.0 and json.Number("1") match.
func containsValue(want []any, v any) bool {
	got := canonicalJSON(v)
	for _, w := range want {
		if canonicalJSON(w) == got {
			return true
		}
	}
	return false
}

func canonicalJSON(v any) string {
	if s, ok := canonical.ScalarString(v); ok {
		if _, isString := v.(string); !isString {
			return s
		}
	}
	data, _ := json.Marshal(v)
	return string(data)
}

func blockerLess(a, b Blocker) bool {
	if a.Entity != b.Entity {
		return a.Entity < b.Entity
	}
	if a.Key != b.Key {
		return a.Key < b.Key
	}
	if a.Field != b.Field {
		return a.Field < b.Field
	}
	return a.Reason < b.Reason
}

// AttentionRuleFailed marks a control whose inputs are complete but whose rule is not satisfied.
const AttentionRuleFailed = "rule_failed"

// TallyOf counts results and computes score and coverage with the catalog's
// scoring. Rejected and expired controls are assessable but never ready.
func TallyOf(results []ControlResult, sc catalog.Scoring) Tally {
	counts := map[Status]bool{}
	for _, s := range sc.CountsAsReady {
		counts[Status(s)] = true
	}
	var t Tally
	for _, r := range results {
		t.InScope++
		switch r.Status {
		case StatusReady:
			t.Ready++
		case StatusMonitoring:
			t.Monitoring++
		case StatusInReview:
			t.InReview++
		case StatusRejected:
			t.Rejected++
		case StatusExpired:
			t.Expired++
		default:
			t.NotAssessed++
		}
		if r.Status != StatusNotAssessed {
			t.Assessable++
		}
		if counts[r.Status] {
			t.ScoreNumerator++
		}
	}
	t.Assumptions = sc.Assumptions
	return t.finish()
}

func (t Tally) add(o Tally) Tally {
	return Tally{
		InScope: t.InScope + o.InScope, Assessable: t.Assessable + o.Assessable,
		Ready: t.Ready + o.Ready, Monitoring: t.Monitoring + o.Monitoring,
		InReview: t.InReview + o.InReview, NotAssessed: t.NotAssessed + o.NotAssessed,
		Rejected: t.Rejected + o.Rejected, Expired: t.Expired + o.Expired,
		ScoreNumerator: t.ScoreNumerator + o.ScoreNumerator,
	}
}

func (t Tally) finish() Tally {
	t.ScoreDefined, t.ScorePct, t.CoveragePct = false, 0, 0
	if t.Assessable > 0 {
		t.ScoreDefined = true
		t.ScorePct = t.ScoreNumerator * 100 / t.Assessable
	}
	if t.InScope > 0 {
		t.CoveragePct = t.Assessable * 100 / t.InScope
	}
	return t
}

// SumTallies adds per-framework tallies into an overall tally.
func SumTallies(ts ...Tally) Tally {
	var sum Tally
	for _, t := range ts {
		sum = sum.add(t)
	}
	return sum.finish()
}
