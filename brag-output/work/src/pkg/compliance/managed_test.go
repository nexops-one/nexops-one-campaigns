// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/crypt"
	"github.com/nexops-one/compliance-engine/pkg/evidence"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/pkg/store/sealed"
)

func encryptedEnv(t *testing.T, storage string) (*wfEnv, *crypt.Keyring) {
	t.Helper()
	var ring *crypt.Keyring
	w := newWorkflowEnv(t, func(c *compliance.Config) {
		inner := c.Store.(*memory.Store)
		kek, _ := crypt.NewKEK([]byte(strings.Repeat("K", 32)))
		ring = crypt.NewKeyring(kek, inner, c.Clock)
		sens, _ := crypt.LoadSensitivity("0.1.0")
		c.Store = sealed.Wrap(inner, ring, sens)
		c.Hasher = sealed.Hasher(ring)
		if storage != "" {
			c.Workflow.Managed = &evidence.Managed{Dir: storage, Ring: ring}
		}
		c.Retention.AuditDays = 3650
	})
	return w, ring
}

func TestManagedEvidence(t *testing.T) {
	dir := t.TempDir()
	w, _ := encryptedEnv(t, dir)
	content := []byte("CONFIDENTIAL exit plan for provider X")
	in := compliance.EvidenceInput{Title: "Exit plan", Kind: "document", Source: "upload",
		Links: []store.ControlRef{{Catalog: "dora", ControlID: monitored}}}
	ev, err := w.eng.UploadEvidence(w.owner, scopeA, in, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ev.URI, evidence.ManagedScheme) || ev.Checksum != checksumOf(content) || ev.Integrity != store.IntegrityVerified {
		t.Fatalf("uploaded = %+v", ev)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "tenant-a", "*.bin"))
	if len(files) != 1 {
		t.Fatalf("stored objects = %v", files)
	}
	raw, _ := os.ReadFile(files[0])
	if bytes.Contains(raw, []byte("CONFIDENTIAL")) {
		t.Fatal("the stored object must be encrypted")
	}
	got, _, err := w.eng.EvidenceContent(w.approver, scopeA, ev.ID)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("download = %q %v", got, err)
	}
	if v, err := w.eng.VerifyEvidence(w.owner, scopeA, ev.ID); err != nil || v.Integrity != store.IntegrityVerified || v.Checks[len(v.Checks)-1].Method != "engine_managed" {
		t.Fatalf("verify = %+v %v", v, err)
	}
	accessLogged := false
	evs, _ := w.st.AuditEvents(ctx, scopeA, store.AuditQuery{})
	for _, e := range evs {
		accessLogged = accessLogged || (e.Action == "evidence.access" && strings.Contains(string(e.Details), `"content":true`))
	}
	if !accessLogged {
		t.Fatal("downloads must be audited")
	}
	if _, err := w.eng.AddEvidence(w.owner, scopeA, compliance.EvidenceInput{Title: "x", Kind: "document", Source: "s",
		URI: ev.URI, Checksum: ev.Checksum}); !errors.Is(err, compliance.ErrInvalidEvidence) {
		t.Fatalf("clients cannot reference managed objects directly: %v", err)
	}

	// Retention removes the object once the item is gone and nothing else uses it.
	if _, err := w.eng.RevokeEvidence(w.owner, scopeA, ev.ID, "superseded"); err != nil {
		t.Fatal(err)
	}
	if rep, err := w.eng.RunRetention(ctx, scopeA, false); err != nil || rep.Deleted.Evidence != 1 {
		t.Fatalf("retention = %+v %v", rep, err)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "tenant-a", "*.bin")); len(files) != 0 {
		t.Fatal("the managed object must be removed with its last evidence item")
	}

	plain := newWorkflowEnv(t)
	if _, err := plain.eng.UploadEvidence(plain.owner, scopeA, in, bytes.NewReader(content)); !errors.Is(err, evidence.ErrManagedNotConfigured) {
		t.Fatalf("upload without managed storage = %v", err)
	}
}

func TestTenantDeleteShredsKeys(t *testing.T) {
	w, ring := encryptedEnv(t, t.TempDir())
	inner := w.st
	_ = ring
	recsBefore, err := w.eng.Snapshot(ctx, scopeA, "")
	if err != nil || len(recsBefore.Records) == 0 {
		t.Fatal("setup")
	}
	// A sealed value as a backup would hold it.
	raw := innerRecords(t, w)
	var sealedValue string
	for _, r := range raw {
		if v, ok := r.Data["annual_cost"].(string); ok && crypt.IsSealed(v) {
			sealedValue = v
		}
	}
	if sealedValue == "" {
		t.Fatal("no sealed value found")
	}
	w.addEvidence(t, "p.pdf", []byte("p"), monitored) // min_days 3650: blocks deletion
	if _, err := w.eng.DeleteTenant(ctx, "tenant-a", false, inner); !errors.Is(err, compliance.ErrEvidenceRetention) {
		t.Fatalf("deletion within evidence retention = %v", err)
	}
	rep, err := w.eng.DeleteTenant(ctx, "tenant-a", true, inner)
	if err != nil || rep.Rows == 0 || !rep.Overridden {
		t.Fatalf("delete = %+v %v", rep, err)
	}
	if _, err := inner.TenantKeys(ctx, "tenant-a"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("the tenant keys must be deleted")
	}
	kek, _ := crypt.NewKEK([]byte(strings.Repeat("K", 32)))
	fresh := crypt.NewKeyring(kek, inner, nil)
	aad := "tenant-a/ws-1/contractual_arrangement/annual_cost"
	if _, err := crypt.Open(fresh.Opener(ctx, "tenant-a"), sealedValue, aad); !errors.Is(err, crypt.ErrOpen) {
		t.Fatalf("a saved ciphertext must not open after deletion, even with the KEK: %v", err)
	}
	chain, _ := inner.AuditEvents(ctx, compliance.Scope{TenantID: "tenant-a"}, store.AuditQuery{})
	if len(chain) == 0 || chain[len(chain)-1].Action != "keys.delete" || chain[len(chain)-2].Action != "tenant.delete" {
		t.Fatalf("tenant chain = %+v", chain)
	}
	_ = time.Now
}

func innerRecords(t *testing.T, w *wfEnv) []store.RecordVersion {
	t.Helper()
	recs, err := w.st.Records(ctx, scopeA, 1)
	if err != nil {
		t.Fatal(err)
	}
	return recs
}
