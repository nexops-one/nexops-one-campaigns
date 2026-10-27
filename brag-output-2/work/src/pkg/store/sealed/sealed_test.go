// SPDX-License-Identifier: Apache-2.0

package sealed_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/crypt"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres/pgtest"
	"github.com/nexops-one/compliance-engine/pkg/store/sealed"
	"github.com/nexops-one/compliance-engine/pkg/store/storetest"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var (
	ctx   = context.Background()
	scope = adapter.Scope{TenantID: "acme", WorkspaceID: "ws-1"}
)

func testKEK(t *testing.T) crypt.KEK {
	t.Helper()
	k, err := crypt.NewKEK([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func wrap(t *testing.T, inner store.Store) (*sealed.Store, *crypt.Keyring) {
	t.Helper()
	sens, err := crypt.LoadSensitivity("0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	ring := crypt.NewKeyring(testKEK(t), inner, nil)
	return sealed.Wrap(inner, ring, sens), ring
}

func TestSealedStoreContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		s, _ := wrap(t, memory.New())
		return s
	})
}

func TestSealedPostgresContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		pg := openPG(t)
		s, _ := wrap(t, pg)
		return s
	})
}

func openPG(t *testing.T) *postgres.Store {
	t.Helper()
	url := pgtest.URL(t)
	pg, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pg.Close)
	if _, err := pg.MigrateUp(ctx); err != nil {
		t.Fatal(err)
	}
	pgURLs[pg] = url
	return pg
}

var pgURLs = map[*postgres.Store]string{}

func sampleBatch(t *testing.T) adapter.Batch {
	t.Helper()
	f, err := os.Open("../../compliance/testdata/sample-batch.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, err := adapter.DecodeBatch(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func manifestFor(b adapter.Batch) adapter.Manifest {
	supplies := map[string][]string{}
	for entity, recs := range b.Entities {
		seen := map[string]bool{}
		for _, r := range recs {
			for f := range r {
				if f != "_meta" && !seen[f] {
					seen[f] = true
					supplies[entity] = append(supplies[entity], f)
				}
			}
		}
	}
	return adapter.Manifest{Name: b.Source.Adapter, Version: "1", SchemaVersion: b.SchemaVersion, Supplies: supplies,
		Modes: []adapter.Mode{adapter.ModeFull, adapter.ModeIncremental}}
}

func newEngine(t *testing.T, st store.Store, hasher func(context.Context, adapter.Scope) (func(adapter.Record) string, error)) *compliance.Engine {
	t.Helper()
	n := 0
	eng, err := compliance.New(ctx, compliance.Config{
		Store: st, Hasher: hasher,
		Clock: func() time.Time { return time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC) },
		NewID: func(p string) string { n++; return fmt.Sprintf("%s-%d", p, n) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

// exercise ingests the sample, attaches evidence, and records a rejection
// with a reason and a note: every kind of sensitive value.
func exercise(t *testing.T, eng *compliance.Engine) {
	t.Helper()
	b := sampleBatch(t)
	if err := eng.RegisterManifest(ctx, scope, manifestFor(b)); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Ingest(ctx, scope, b); err != nil {
		t.Fatal(err)
	}
	user := extension.WithPrincipal(ctx, extension.Principal{Scope: scope, Actor: "user:o", Kind: extension.ActorUser, Roles: []access.Role{access.RoleOwner}})
	notes := "SECRET-NOTES-exit costs 2.4M"
	if _, err := eng.Assign(user, scope, "dora", "dora-roi-provider-identification", compliance.AssignInput{Notes: &notes}); err != nil {
		t.Fatal(err)
	}
	ev, err := eng.AddEvidence(user, scope, compliance.EvidenceInput{Title: "t", Kind: "document", Source: "dms",
		URI: "https://dms.example/SECRET-PATH/exit.pdf", Checksum: "sha256:" + strings.Repeat("a", 64),
		Links: []store.ControlRef{{Catalog: "dora", ControlID: "dora-roi-provider-identification"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Act(user, scope, "dora", "dora-roi-provider-identification", workflow.ActionSubmit, compliance.ActInput{Note: "SECRET-NOTE-submit"}); err != nil {
		t.Fatal(err)
	}
	approver := extension.WithPrincipal(ctx, extension.Principal{Scope: scope, Actor: "user:a", Kind: extension.ActorUser, Roles: []access.Role{access.RoleApprover}})
	if _, err := eng.Act(approver, scope, "dora", "dora-roi-provider-identification", workflow.ActionReject, compliance.ActInput{Reason: "SECRET-REASON-reject"}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.RevokeEvidence(user, scope, ev.ID, "SECRET-REASON-revoke"); err != nil {
		t.Fatal(err)
	}
	// Reads through the engine see plaintext.
	got, err := eng.Evidence(user, scope, ev.ID)
	if err != nil || got.URI != "https://dms.example/SECRET-PATH/exit.pdf" || got.RevokeReason != "SECRET-REASON-revoke" {
		t.Fatalf("decrypted evidence = %+v %v", got, err)
	}
	view, err := eng.Assessment(ctx, scope, "dora", "dora-roi-provider-identification")
	if err != nil || view.Assessment.Notes != notes || view.History[len(view.History)-1].Reason != "SECRET-REASON-reject" {
		t.Fatalf("decrypted assessment = %+v %v", view, err)
	}
	snap, err := eng.Snapshot(ctx, scope, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range snap.Records {
		if r.Entity == "contractual_arrangement" && fmt.Sprint(r.Data["annual_cost"]) == "48000" {
			found = true
		}
	}
	if !found {
		t.Fatal("records read through the engine must be decrypted")
	}
}

var secrets = []string{"SECRET-NOTES", "SECRET-PATH", "SECRET-NOTE-submit", "SECRET-REASON-reject", "SECRET-REASON-revoke"}

func TestSealedStoreHidesSensitiveValues(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		inner := memory.New()
		s, ring := wrap(t, inner)
		exercise(t, newEngine(t, s, sealed.Hasher(ring)))
		raw := dumpInner(t, inner)
		for _, sec := range secrets {
			if strings.Contains(raw, sec) {
				t.Errorf("the inner store holds %q in plaintext", sec)
			}
		}
		recs, _ := inner.Records(ctx, scope, 1)
		for _, r := range recs {
			if v, ok := r.Data["annual_cost"]; ok && !crypt.IsSealed(fmt.Sprint(v)) {
				t.Errorf("annual_cost stored in plaintext: %v", v)
			}
			if v, ok := r.Data["rto"]; ok && !crypt.IsSealed(fmt.Sprint(v)) {
				t.Errorf("rto stored in plaintext: %v", v)
			}
			if !strings.HasPrefix(r.Hash, "hmac-sha256:") {
				t.Errorf("content hash %q is not an HMAC", r.Hash)
			}
		}
	})
	t.Run("postgres", func(t *testing.T) {
		pg := openPG(t)
		s, ring := wrap(t, pg)
		exercise(t, newEngine(t, s, sealed.Hasher(ring)))
		conn, err := pgx.Connect(ctx, pgURLs[pg])
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(ctx)
		var dump string
		err = conn.QueryRow(ctx, `SELECT
  (SELECT coalesce(string_agg(data::text, ' '), '') FROM ce_record_versions) || ' ' ||
  (SELECT coalesce(string_agg(doc::text, ' '), '') FROM ce_evidence) || ' ' ||
  (SELECT coalesce(string_agg(doc::text, ' '), '') FROM ce_assessments) || ' ' ||
  (SELECT coalesce(string_agg(doc::text, ' '), '') FROM ce_assessment_history) || ' ' ||
  (SELECT coalesce(string_agg(details, ' '), '') FROM ce_audit_events)`).Scan(&dump)
		if err != nil {
			t.Fatal(err)
		}
		for _, sec := range secrets {
			if strings.Contains(dump, sec) {
				t.Errorf("PostgreSQL holds %q in plaintext", sec)
			}
		}
		if strings.Contains(dump, `"annual_cost": 48000`) || strings.Contains(dump, `"rto": 240`) {
			t.Error("sensitive record fields are stored in plaintext")
		}
	})
}

// dumpInner serializes everything the inner memory store returns.
func dumpInner(t *testing.T, st *memory.Store) string {
	t.Helper()
	var parts []any
	recs, _ := st.Records(ctx, scope, 1)
	evs, _ := st.ListEvidence(ctx, scope, store.EvidenceQuery{})
	as, _ := st.Assessments(ctx, scope, "")
	h, _ := st.History(ctx, scope, "dora", "dora-roi-provider-identification")
	audit, _ := st.AuditEvents(ctx, scope, store.AuditQuery{})
	parts = append(parts, recs, evs, as, h, audit)
	data, _ := json.Marshal(parts)
	return string(data)
}

func TestIdenticalIngestionUnderEncryption(t *testing.T) {
	s, ring := wrap(t, memory.New())
	eng := newEngine(t, s, sealed.Hasher(ring))
	b := sampleBatch(t)
	_ = eng.RegisterManifest(ctx, scope, manifestFor(b))
	if _, err := eng.Ingest(ctx, scope, b); err != nil {
		t.Fatal(err)
	}
	again, err := eng.Ingest(ctx, scope, sampleBatch(t))
	if err != nil || !again.NoChanges {
		t.Fatalf("re-ingesting identical content must change nothing: %+v %v", again, err)
	}
	if _, err := ring.RotateData(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	again, err = eng.Ingest(ctx, scope, sampleBatch(t))
	if err != nil || !again.NoChanges {
		t.Fatalf("rotating the data key must not change content hashes: %+v %v", again, err)
	}
}

func TestEvaluationUnchangedUnderEncryption(t *testing.T) {
	plain := newEngine(t, memory.New(), nil)
	s, ring := wrap(t, memory.New())
	enc := newEngine(t, s, sealed.Hasher(ring))
	var results [2]string
	for i, eng := range []*compliance.Engine{plain, enc} {
		b := sampleBatch(t)
		_ = eng.RegisterManifest(ctx, scope, manifestFor(b))
		if _, err := eng.Ingest(ctx, scope, b); err != nil {
			t.Fatal(err)
		}
		ev, err := eng.Evaluate(ctx, scope, "", []catalog.Ref{{Catalog: "dora", Version: "1.0.0"}})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(ev.Result)
		results[i] = string(data)
		c, err := eng.Completeness(ctx, scope, "")
		if err != nil {
			t.Fatal(err)
		}
		cdata, _ := json.Marshal(c)
		results[i] += string(cdata)
	}
	if results[0] != results[1] {
		t.Fatalf("encryption must not change evaluations or completeness:\n%s\n%s", results[0], results[1])
	}
}

func TestWrongKEKCannotRead(t *testing.T) {
	inner := memory.New()
	s, ring := wrap(t, inner)
	eng := newEngine(t, s, sealed.Hasher(ring))
	b := sampleBatch(t)
	_ = eng.RegisterManifest(ctx, scope, manifestFor(b))
	if _, err := eng.Ingest(ctx, scope, b); err != nil {
		t.Fatal(err)
	}
	other, _ := crypt.NewKEK([]byte(strings.Repeat("x", 32)))
	sens, _ := crypt.LoadSensitivity("0.1.0")
	wrong := sealed.Wrap(inner, crypt.NewKeyring(other, inner, nil), sens)
	if _, err := wrong.Records(ctx, scope, 1); err == nil || !strings.Contains(err.Error(), "different key-encryption key") {
		t.Fatalf("reading with another KEK = %v", err)
	}
}

func TestSealExisting(t *testing.T) {
	pg := openPG(t)
	plainEng := newEngine(t, pg, nil)
	exercise(t, plainEng) // written before encryption was enabled
	s, ring := wrap(t, pg)
	rep, err := s.SealExisting(ctx, pg, "acme")
	if err != nil || rep.Records == 0 || rep.Evidence != 1 || rep.Assessments != 1 {
		t.Fatalf("seal existing = %+v %v", rep, err)
	}
	if again, err := s.SealExisting(ctx, pg, "acme"); err != nil || again.Evidence != 0 || again.Assessments != 0 {
		t.Fatalf("a second run must find nothing left to seal: %+v %v", again, err)
	}
	conn, err := pgx.Connect(ctx, pgURLs[pg])
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var dump string
	if err := conn.QueryRow(ctx, `SELECT (SELECT string_agg(data::text || hash, ' ') FROM ce_record_versions) || (SELECT string_agg(doc::text, ' ') FROM ce_evidence) || (SELECT string_agg(doc::text, ' ') FROM ce_assessments)`).Scan(&dump); err != nil {
		t.Fatal(err)
	}
	for _, sec := range []string{"SECRET-NOTES", "SECRET-PATH", "SECRET-REASON-revoke", `"annual_cost": 48000`} {
		if strings.Contains(dump, sec) {
			t.Errorf("%q is still stored in plaintext", sec)
		}
	}
	var plainHashes, orphanProvenance int
	_ = conn.QueryRow(ctx, `SELECT count(*) FROM ce_record_versions WHERE hash NOT LIKE 'hmac-sha256:%'`).Scan(&plainHashes)
	_ = conn.QueryRow(ctx, `SELECT count(*) FROM ce_provenance p WHERE p.content_hash <> '' AND NOT EXISTS
  (SELECT 1 FROM ce_record_versions v WHERE v.tenant_id = p.tenant_id AND v.workspace_id = p.workspace_id AND v.hash = p.content_hash)`).Scan(&orphanProvenance)
	if plainHashes != 0 || orphanProvenance != 0 {
		t.Fatalf("hashes not rewritten consistently: %d plain, %d provenance hashes without a version", plainHashes, orphanProvenance)
	}
	enc := newEngine(t, s, sealed.Hasher(ring))
	res, err := enc.Ingest(ctx, scope, sampleBatch(t))
	if err != nil || !res.NoChanges {
		t.Fatalf("after sealing, identical content must still be a no-op: %+v %v", res, err)
	}
	if ev, err := enc.ListEvidence(ctx, scope, store.EvidenceQuery{}); err != nil || ev[0].URI != "https://dms.example/SECRET-PATH/exit.pdf" {
		t.Fatalf("sealed evidence must read back: %+v %v", ev, err)
	}
}
