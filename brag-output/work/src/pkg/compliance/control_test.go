// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"errors"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestControlStatus(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	res, err := eng.Status(ctx, scopeA, nil)
	if err != nil {
		t.Fatal(err)
	}
	var want, blocked string
	for _, fw := range res.Frameworks {
		for _, c := range fw.Controls {
			if fw.Catalog.Catalog != "dora" {
				continue
			}
			if want == "" && c.ComputedStatus == engine.StatusMonitoring {
				want = c.ControlID
			}
			if blocked == "" && c.ComputedStatus == engine.StatusNotAssessed {
				blocked = c.ControlID
			}
		}
	}
	cs, err := eng.ControlStatus(ctx, scopeA, "dora", want)
	if err != nil {
		t.Fatal(err)
	}
	if cs.Catalog.Catalog != "dora" || cs.Control.ID != want || cs.Computed.ControlID != want || cs.Effective.ControlID != want ||
		cs.Effective.Status != engine.StatusMonitoring || cs.SnapshotID != "rev-1" || len(cs.Computed.Explanation.RecordsExamined) == 0 {
		t.Fatalf("control status = %+v", cs)
	}
	if cs, _ := eng.ControlStatus(ctx, scopeA, "dora", blocked); len(cs.Computed.Blockers) == 0 {
		t.Fatalf("a not-assessed control explains its blockers: %+v", cs.Computed)
	}
	if _, err := eng.ControlStatus(ctx, scopeA, "dora", "nope"); !errors.Is(err, compliance.ErrUnknownControl) {
		t.Fatalf("unknown control: %v", err)
	}
}

func TestEmails(t *testing.T) {
	st := memory.New()
	eng := newEngine(t, func(c *compliance.Config) { c.Store = st })
	if err := st.CreateUser(ctx, store.User{TenantID: scopeA.TenantID, ID: "u1", Email: "anna@example.com"},
		store.AuditEvent{Scope: adapter.Scope{TenantID: scopeA.TenantID}, Actor: "cli", ActorKind: "system", Action: "user.create"}); err != nil {
		t.Fatal(err)
	}
	got, err := eng.Emails(ctx, scopeA, "user:u1", "user:gone", "token:t1", "system", "")
	if err != nil || len(got) != 1 || got["user:u1"] != "anna@example.com" {
		t.Fatalf("emails = %v, %v", got, err)
	}
}
