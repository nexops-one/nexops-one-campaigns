// SPDX-License-Identifier: Apache-2.0

package audit_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/audit"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var scope = adapter.Scope{TenantID: "acme", WorkspaceID: "ws-1"}

func events(n int) []store.AuditEvent {
	at := time.Date(2026, 9, 30, 10, 0, 0, 123456789, time.UTC)
	out := make([]store.AuditEvent, n)
	for i := range out {
		out[i] = store.AuditEvent{
			Scope: scope, At: at.Add(time.Duration(i) * time.Second), Actor: "user:u1", ActorKind: "user",
			Action: "ingestion.commit", TargetType: "ingestion", TargetID: "ing-" + string(rune('a'+i)),
			Details: json.RawMessage(`{ "created": 1 }`),
		}
	}
	return out
}

func sealed(t *testing.T, n int) []store.AuditEvent {
	t.Helper()
	out, err := audit.Seal(0, "", events(n))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSealChains(t *testing.T) {
	evs := sealed(t, 3)
	if evs[0].Seq != 1 || evs[0].PrevHash != "" || evs[1].PrevHash != evs[0].Hash || evs[2].PrevHash != evs[1].Hash {
		t.Fatalf("chain: %+v", evs)
	}
	if string(evs[0].Details) != `{"created":1}` || evs[0].At.Nanosecond() != 123456000 {
		t.Fatalf("normalization: %s %v", evs[0].Details, evs[0].At)
	}
	// Golden value: changing it breaks every stored chain.
	if evs[0].Hash != audit.Hash("", evs[0]) || len(evs[0].Hash) != 64 {
		t.Fatalf("hash %q", evs[0].Hash)
	}
	const golden = "07aea65b0a60a5a0bde2171338dffa1ae75a5858c91af2ba4dcda739cb72f56d"
	if evs[0].Hash != golden {
		t.Logf("first hash %s (update the golden value only with a chain-format migration)", evs[0].Hash)
		t.Fail()
	}
	more, err := audit.Seal(evs[2].Seq, evs[2].Hash, events(1))
	if err != nil || more[0].Seq != 4 || more[0].PrevHash != evs[2].Hash {
		t.Fatalf("continuation: %+v %v", more, err)
	}
	if r := audit.Verify(append(evs, more...)); !r.OK || r.Checked != 4 {
		t.Fatalf("verify: %+v", r)
	}
}

func TestSealRejects(t *testing.T) {
	mixed := events(2)
	mixed[1].Scope.WorkspaceID = "ws-2"
	if _, err := audit.Seal(0, "", mixed); err == nil {
		t.Fatal("mixed scopes must fail")
	}
	for name, mutate := range map[string]func(*store.AuditEvent){
		"no action":   func(e *store.AuditEvent) { e.Action = "" },
		"no actor":    func(e *store.AuditEvent) { e.Actor = " " },
		"no tenant":   func(e *store.AuditEvent) { e.Scope.TenantID = "" },
		"bad details": func(e *store.AuditEvent) { e.Details = json.RawMessage(`{`) },
	} {
		evs := events(1)
		mutate(&evs[0])
		if _, err := audit.Seal(0, "", evs); err == nil {
			t.Errorf("%s: must fail", name)
		}
	}
	evs := events(1)
	evs[0].Details = nil
	out, err := audit.Seal(0, "", evs)
	if err != nil || string(out[0].Details) != "{}" {
		t.Fatalf("empty details: %s %v", out[0].Details, err)
	}
	tenantLevel := events(1)
	tenantLevel[0].Scope.WorkspaceID = ""
	if _, err := audit.Seal(0, "", tenantLevel); err != nil {
		t.Fatalf("tenant-level chain: %v", err)
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	cases := map[string]struct {
		mutate func([]store.AuditEvent) []store.AuditEvent
		at     int64
		reason string
	}{
		"edited details": {func(e []store.AuditEvent) []store.AuditEvent {
			e[1].Details = json.RawMessage(`{"created":2}`)
			return e
		}, 2, audit.ReasonHashMismatch},
		"edited actor": {func(e []store.AuditEvent) []store.AuditEvent { e[2].Actor = "user:u2"; return e }, 3, audit.ReasonHashMismatch},
		"deleted event": {func(e []store.AuditEvent) []store.AuditEvent {
			return append(e[:1:1], e[2:]...)
		}, 3, audit.ReasonSequenceGap},
		"swapped events": {func(e []store.AuditEvent) []store.AuditEvent {
			e[1], e[2] = e[2], e[1]
			return e
		}, 3, audit.ReasonSequenceGap},
		"relinked":       {func(e []store.AuditEvent) []store.AuditEvent { e[3].PrevHash = e[1].Hash; return e }, 4, audit.ReasonPrevHashMismatch},
		"forged genesis": {func(e []store.AuditEvent) []store.AuditEvent { e[0].PrevHash = "x"; return e }, 1, audit.ReasonPrevHashMismatch},
	}
	for name, c := range cases {
		r := audit.Verify(c.mutate(sealed(t, 4)))
		if r.OK || r.BrokenAt != c.at || r.Reason != c.reason {
			t.Errorf("%s: %+v, want broken at %d (%s)", name, r, c.at, c.reason)
		}
	}
	if r := audit.Verify(nil); !r.OK || r.Checked != 0 {
		t.Fatalf("empty chain: %+v", r)
	}
}
