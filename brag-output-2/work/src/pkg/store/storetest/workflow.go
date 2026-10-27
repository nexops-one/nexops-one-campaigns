// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/store"
)

func evidenceDoc(id string, links ...store.ControlRef) store.Evidence {
	return store.Evidence{
		ID: id, Scope: scopeA, Title: "Exit plan " + id, Kind: "document", Source: "dms", URI: "https://dms.example/" + id,
		Checksum: "sha256:00", CollectedAt: at, Collector: "user:u1", Retention: store.Retention{MinDays: 3650, Basis: "DORA"},
		Integrity: store.IntegrityUnverified, Checks: []store.EvidenceCheck{}, Links: links, CreatedAt: at,
	}
}

func assessment(control string) store.Assessment {
	return store.Assessment{Scope: scopeA, Catalog: "dora", ControlID: control, Owner: "user:u1", Stage: store.StageNone, UpdatedAt: at}
}

func runWorkflow(t *testing.T, newStore func(t *testing.T) store.Store) {
	dora1 := store.ControlRef{Catalog: "dora", ControlID: "c1"}
	dora2 := store.ControlRef{Catalog: "dora", ControlID: "c2"}

	t.Run("evidence documents", func(t *testing.T) {
		s := newStore(t)
		e1, err := s.PutEvidence(ctx, evidenceDoc("ev-1", dora1), 0, Event(scopeA, "evidence.create"))
		if err != nil || e1.Version != 1 {
			t.Fatalf("create = %+v, %v", e1, err)
		}
		later := evidenceDoc("ev-2", dora1, dora2)
		later.CreatedAt = at.Add(time.Minute)
		if _, err := s.PutEvidence(ctx, later, 0, Event(scopeA, "evidence.create")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PutEvidence(ctx, evidenceDoc("ev-1"), 0, Event(scopeA, "evidence.create")); !errors.Is(err, store.ErrExists) {
			t.Fatalf("duplicate id = %v", err)
		}
		got, err := s.Evidence(ctx, scopeA, "ev-1")
		if err != nil || !reflect.DeepEqual(got, e1) {
			t.Fatalf("read = %+v, %v\nwant %+v", got, err, e1)
		}
		if _, err := s.Evidence(ctx, scopeB, "ev-1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("evidence must be scoped: %v", err)
		}
		all, _ := s.ListEvidence(ctx, scopeA, store.EvidenceQuery{})
		byC2, _ := s.ListEvidence(ctx, scopeA, store.EvidenceQuery{Catalog: "dora", ControlID: "c2"})
		byCat, _ := s.ListEvidence(ctx, scopeA, store.EvidenceQuery{Catalog: "gdpr"})
		if len(all) != 2 || all[0].ID != "ev-1" || len(byC2) != 1 || byC2[0].ID != "ev-2" || len(byCat) != 0 {
			t.Fatalf("lists = %d %d %d", len(all), len(byC2), len(byCat))
		}

		// Update with the right version; a stale version conflicts and changes nothing.
		e1.Links = []store.ControlRef{dora2}
		e1.Integrity = store.IntegrityVerified
		e1.Checks = append(e1.Checks, store.EvidenceCheck{At: at, Actor: "user:u1", Method: "attested", Observed: "sha256:x", Match: true})
		e2, err := s.PutEvidence(ctx, e1, 1, Event(scopeA, "evidence.link"))
		if err != nil || e2.Version != 2 {
			t.Fatalf("update = %+v, %v", e2, err)
		}
		if _, err := s.PutEvidence(ctx, e1, 1, Event(scopeA, "evidence.link")); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("stale version = %v", err)
		}
		if byC2, _ := s.ListEvidence(ctx, scopeA, store.EvidenceQuery{Catalog: "dora", ControlID: "c2"}); len(byC2) != 2 {
			t.Fatalf("links must follow updates: %d", len(byC2))
		}
		if byC1, _ := s.ListEvidence(ctx, scopeA, store.EvidenceQuery{Catalog: "dora", ControlID: "c1"}); len(byC1) != 1 {
			t.Fatalf("removed links must be removed: %d", len(byC1))
		}
		if got, _ := s.Evidence(ctx, scopeA, "ev-1"); got.Integrity != store.IntegrityVerified || len(got.Checks) != 1 {
			t.Fatalf("stored update = %+v", got)
		}
		if _, err := s.PutEvidence(ctx, evidenceDoc("ev-3"), 0, Invalid(scopeA)); err == nil {
			t.Fatal("an invalid audit event must abort the change")
		}
		if _, err := s.Evidence(ctx, scopeA, "ev-3"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("the aborted evidence must not exist")
		}
		if got := ChainOf(t, s, scopeA); !reflect.DeepEqual(got, []string{"evidence.create", "evidence.create", "evidence.link"}) {
			t.Fatalf("chain = %v", got)
		}
	})

	t.Run("assessments and history", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Assessment(ctx, scopeA, "dora", "c1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("missing = %v", err)
		}
		tr := &store.Transition{Catalog: "dora", ControlID: "c1", At: at, Actor: "user:u1", ActorKind: "user", Action: "submit",
			FromStage: store.StageNone, ToStage: store.StageSubmitted}
		a := assessment("c1")
		a.Stage = store.StageSubmitted
		saved, err := s.SaveAssessment(ctx, a, 0, tr, Event(scopeA, "assessment.submit"))
		if err != nil || saved.Version != 1 {
			t.Fatalf("create = %+v, %v", saved, err)
		}
		if _, err := s.SaveAssessment(ctx, a, 0, nil, Event(scopeA, "assessment.assign")); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("creating twice = %v", err)
		}
		due := at.Add(24 * time.Hour)
		saved.Stage, saved.DueAt = store.StageApproved, &due
		saved.Approval = &store.Approval{Actor: "user:u2", At: at, EvaluationID: "eval-1", SnapshotID: "rev-1", ExpiresAt: at.AddDate(1, 0, 0)}
		tr2 := *tr
		tr2.Action, tr2.FromStage, tr2.ToStage = "approve", store.StageSubmitted, store.StageApproved
		saved2, err := s.SaveAssessment(ctx, saved, 1, &tr2, Event(scopeA, "assessment.approve"))
		if err != nil || saved2.Version != 2 {
			t.Fatalf("update = %+v, %v", saved2, err)
		}
		if _, err := s.SaveAssessment(ctx, saved, 1, &tr2, Event(scopeA, "assessment.approve")); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("stale = %v", err)
		}
		other := assessment("c2")
		if _, err := s.SaveAssessment(ctx, other, 0, &store.Transition{Catalog: "dora", ControlID: "c2", At: at, Actor: "u", Action: "assign"}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Assessment(ctx, scopeA, "dora", "c1")
		if err != nil || !reflect.DeepEqual(got, saved2) {
			t.Fatalf("read = %+v, %v\nwant %+v", got, err, saved2)
		}
		list, _ := s.Assessments(ctx, scopeA, "")
		if len(list) != 2 || list[0].ControlID != "c1" {
			t.Fatalf("list = %+v", list)
		}
		if l, _ := s.Assessments(ctx, scopeA, "gdpr"); len(l) != 0 {
			t.Fatal("catalog filter")
		}
		h, err := s.History(ctx, scopeA, "dora", "c1")
		if err != nil || len(h) != 2 || h[0].Seq != 1 || h[1].Seq != 2 || h[1].Action != "approve" || h[1].ToStage != store.StageApproved {
			t.Fatalf("history = %+v, %v", h, err)
		}
		if h2, _ := s.History(ctx, scopeA, "dora", "c2"); len(h2) != 1 || h2[0].Seq != 3 {
			t.Fatalf("sequence numbers are per workspace: %+v", h2)
		}
		if _, err := s.SaveAssessment(ctx, assessment("c9"), 0, nil, Invalid(scopeA)); err == nil {
			t.Fatal("an invalid audit event must abort the save")
		}
		if _, err := s.Assessment(ctx, scopeA, "dora", "c9"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("the aborted assessment must not exist")
		}
		if _, err := s.Assessment(ctx, scopeB, "dora", "c1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("assessments must be scoped")
		}
	})
}
