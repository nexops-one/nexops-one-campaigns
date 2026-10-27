// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// limited is the open-core entitlements with a workspace limit.
type limited struct {
	extension.AllowOpen
	n int
}

func (l limited) WorkspaceLimit(context.Context) int { return l.n }

func systemActions(t *testing.T, st *memory.Store) []string {
	t.Helper()
	evs, err := st.AuditEvents(ctx, compliance.SystemScope, store.AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range evs {
		out = append(out, e.Action)
	}
	return out
}

func TestWorkspaceRegisteredOnFirstWrite(t *testing.T) {
	st := memory.New()
	eng := newEngine(t, func(c *compliance.Config) { c.Store = st })
	if _, err := eng.Status(ctx, scopeA, nil); err != nil {
		t.Fatal(err)
	}
	if ws, _ := eng.Workspaces(ctx, ""); len(ws) != 0 {
		t.Fatalf("a read must not register the workspace: %+v", ws)
	}
	b := sampleBatch(t)
	if _, err := eng.DryRun(ctx, scopeA, b); err == nil || !errors.Is(err, compliance.ErrUnknownAdapter) {
		t.Fatalf("dry run = %v", err)
	}
	if ws, _ := eng.Workspaces(ctx, ""); len(ws) != 0 {
		t.Fatalf("a dry run must not register the workspace: %+v", ws)
	}
	pctx := extension.WithPrincipal(ctx, extension.Principal{Scope: scopeA, Actor: "user:u-1", Kind: extension.ActorUser})
	if err := eng.RegisterManifest(pctx, scopeA, manifestFor(b)); err != nil {
		t.Fatal(err)
	}
	w, err := eng.Workspace(ctx, scopeA)
	if err != nil || w.Status != store.WorkspaceActive || w.CreatedBy != "user:u-1" {
		t.Fatalf("workspace = %+v, %v", w, err)
	}
	if _, err := eng.Ingest(pctx, scopeA, b); err != nil {
		t.Fatal(err)
	}
	if got := systemActions(t, st); len(got) != 1 || got[0] != "workspace.register" {
		t.Fatalf("system chain = %v", got)
	}
	reserved := compliance.Scope{TenantID: "_system", WorkspaceID: "ws"}
	if err := eng.RegisterManifest(ctx, reserved, manifestFor(b)); !errors.Is(err, compliance.ErrReservedTenant) {
		t.Fatalf("reserved tenant = %v", err)
	}
}

func TestWorkspaceLimit(t *testing.T) {
	eng := newEngine(t, func(c *compliance.Config) { c.Entitlements = limited{n: 1} })
	m := manifestFor(sampleBatch(t))
	if err := eng.RegisterManifest(ctx, scopeA, m); err != nil {
		t.Fatal(err)
	}
	if err := eng.RegisterManifest(ctx, scopeB, m); !errors.Is(err, store.ErrWorkspaceLimit) {
		t.Fatalf("second workspace = %v", err)
	}
	if _, err := eng.RegisterWorkspace(ctx, scopeB); !errors.Is(err, store.ErrWorkspaceLimit) {
		t.Fatalf("explicit registration = %v", err)
	}
	if _, err := eng.RegisterWorkspace(ctx, scopeA); !errors.Is(err, compliance.ErrWorkspaceExists) {
		t.Fatalf("existing = %v", err)
	}
	u, err := eng.Usage(ctx)
	if err != nil || u.Limit != 1 || u.Active != 1 || u.ByTenant[scopeA.TenantID] != 1 {
		t.Fatalf("usage = %+v, %v", u, err)
	}
	// Suspending frees the slot.
	if _, err := eng.SuspendWorkspace(ctx, scopeA, "moved"); err != nil {
		t.Fatal(err)
	}
	if err := eng.RegisterManifest(ctx, scopeB, m); err != nil {
		t.Fatalf("after suspension = %v", err)
	}
	if _, err := eng.ResumeWorkspace(ctx, scopeA); !errors.Is(err, store.ErrWorkspaceLimit) {
		t.Fatalf("resume over the limit = %v", err)
	}
}

func TestSuspendedWorkspaceRefusesWritesKeepsReads(t *testing.T) {
	st := memory.New()
	eng := newEngine(t, func(c *compliance.Config) { c.Store = st })
	b := sampleBatch(t)
	if err := eng.RegisterManifest(ctx, scopeA, manifestFor(b)); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Ingest(ctx, scopeA, b); err != nil {
		t.Fatal(err)
	}
	w, err := eng.SuspendWorkspace(ctx, scopeA, " unpaid ")
	if err != nil || w.Status != store.WorkspaceSuspended || w.Reason != "unpaid" || w.StatusBy != compliance.LibraryActor {
		t.Fatalf("suspend = %+v, %v", w, err)
	}
	if _, err := eng.Ingest(ctx, scopeA, b); !errors.Is(err, compliance.ErrWorkspaceSuspended) {
		t.Fatalf("ingest = %v", err)
	}
	if _, err := eng.Evaluate(ctx, scopeA, "", nil); !errors.Is(err, compliance.ErrWorkspaceSuspended) {
		t.Fatalf("evaluate = %v", err)
	}
	if _, err := eng.UpdateSettings(ctx, scopeA, compliance.SettingsInput{}); !errors.Is(err, compliance.ErrWorkspaceSuspended) {
		t.Fatalf("settings = %v", err)
	}
	if err := eng.WorkspaceWritable(ctx, scopeA); !errors.Is(err, compliance.ErrWorkspaceSuspended) {
		t.Fatalf("writable = %v", err)
	}
	if _, err := eng.Snapshot(ctx, scopeA, ""); err != nil {
		t.Fatalf("snapshot read = %v", err)
	}
	if _, err := eng.Status(ctx, scopeA, nil); err != nil {
		t.Fatalf("status read = %v", err)
	}
	if _, err := eng.DryRun(ctx, scopeA, b); err != nil {
		t.Fatalf("a dry run writes nothing and stays allowed: %v", err)
	}
	if _, err := eng.ResumeWorkspace(ctx, scopeA); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Evaluate(ctx, scopeA, "", nil); err != nil {
		t.Fatalf("evaluate after resumption = %v", err)
	}
	got := systemActions(t, st)
	if len(got) != 3 || got[1] != "workspace.suspend" || got[2] != "workspace.resume" {
		t.Fatalf("system chain = %v", got)
	}
}

func TestDeleteWorkspace(t *testing.T) {
	st := memory.New()
	eng := newEngine(t, func(c *compliance.Config) { c.Store = st })
	b := sampleBatch(t)
	for _, sc := range []adapter.Scope{scopeA, scopeB} {
		if err := eng.RegisterManifest(ctx, sc, manifestFor(b)); err != nil {
			t.Fatal(err)
		}
		if _, err := eng.Ingest(ctx, sc, b); err != nil {
			t.Fatal(err)
		}
	}
	held, err := eng.AddEvidence(ctx, scopeA, compliance.EvidenceInput{Kind: "document", Title: "Policy", Source: "manual",
		URI: "https://docs.example/policy.pdf", Checksum: "sha256:" + repeat("a", 64),
		Links: []store.ControlRef{{Catalog: "dora", ControlID: "dora-roi-reporting-entity"}}, Retention: store.Retention{MinDays: 365}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.DeleteWorkspace(ctx, scopeA, false); !errors.Is(err, compliance.ErrEvidenceRetention) {
		t.Fatalf("evidence within retention = %v", err)
	}
	rep, err := eng.DeleteWorkspace(ctx, scopeA, true)
	if err != nil || rep.Rows == 0 || !rep.Overridden {
		t.Fatalf("delete = %+v, %v", rep, err)
	}
	if _, err := eng.Evidence(ctx, scopeA, held.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("evidence remains: %v", err)
	}
	if _, err := eng.Workspace(ctx, scopeA); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("registration remains: %v", err)
	}
	if _, err := eng.Snapshot(ctx, scopeB, ""); err != nil {
		t.Fatalf("the other workspace was touched: %v", err)
	}
	got := systemActions(t, st)
	if got[len(got)-1] != "workspace.delete" {
		t.Fatalf("system chain = %v", got)
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
