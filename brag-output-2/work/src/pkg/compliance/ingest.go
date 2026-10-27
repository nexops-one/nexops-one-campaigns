// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"fmt"
	"sort"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/ingest"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// RegisterManifest registers or replaces an adapter's capability manifest.
func (e *Engine) RegisterManifest(ctx context.Context, scope Scope, m adapter.Manifest) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if _, err := e.require(ctx, scope, extension.FeatureRegisterIngest); err != nil {
		return err
	}
	if err := m.Validate(e.schemas); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}
	return e.store.PutManifest(ctx, scope, m)
}

// Ingest validates a batch and commits its accepted records as a new revision.
// Identical content creates no revision. Invalid records are reported in the
// result, not returned as errors.
func (e *Engine) Ingest(ctx context.Context, scope Scope, b adapter.Batch) (ingest.Result, error) {
	res, _, err := e.ingest(ctx, scope, b, false)
	return res, err
}

// DryRunResult is a would-be ingestion plus the completeness of the would-be snapshot.
type DryRunResult struct {
	Result       ingest.Result          `json:"result"`
	Completeness canonical.Completeness `json:"completeness"`
}

// DryRun runs ingestion without committing anything.
func (e *Engine) DryRun(ctx context.Context, scope Scope, b adapter.Batch) (DryRunResult, error) {
	res, next, err := e.ingest(ctx, scope, b, true)
	if err != nil {
		return DryRunResult{}, err
	}
	c, err := e.completeness(ctx, scope, next)
	if err != nil {
		return DryRunResult{}, err
	}
	return DryRunResult{Result: res, Completeness: c}, nil
}

// Sync registers the adapter's manifest, pulls a batch for scope and ingests
// it. The adapter never sees the store; the engine binds the scope.
func (e *Engine) Sync(ctx context.Context, scope Scope, a adapter.Adapter, mode adapter.Mode) (ingest.Result, error) {
	m := a.Manifest()
	if err := e.RegisterManifest(ctx, scope, m); err != nil {
		return ingest.Result{}, err
	}
	b, err := a.Pull(ctx, adapter.SyncRequest{Scope: scope, Mode: mode})
	if err != nil {
		return ingest.Result{}, fmt.Errorf("adapter %s: pull: %w", m.Name, err)
	}
	if b.Source.Adapter != m.Name {
		return ingest.Result{}, fmt.Errorf("adapter %s emitted a batch for adapter %q", m.Name, b.Source.Adapter)
	}
	if b.Mode() != mode {
		return ingest.Result{}, fmt.Errorf("adapter %s returned a %s batch for a %s sync", m.Name, b.Mode(), mode)
	}
	return e.Ingest(ctx, scope, b)
}

// ingest validates and plans a batch; unless dryRun it commits the plan. It
// returns the records of the resulting (or would-be) snapshot.
func (e *Engine) ingest(ctx context.Context, scope Scope, b adapter.Batch, dryRun bool) (ingest.Result, []store.RecordVersion, error) {
	fail := func(err error) (ingest.Result, []store.RecordVersion, error) { return ingest.Result{}, nil, err }
	if err := scope.Validate(); err != nil {
		return fail(err)
	}
	// A dry run writes nothing: it needs the entitlement, not a writable workspace.
	if dryRun {
		if _, err := extension.Require(ctx, e.ents, scope, extension.FeatureRegisterIngest); err != nil {
			return fail(err)
		}
	} else if _, err := e.require(ctx, scope, extension.FeatureRegisterIngest); err != nil {
		return fail(err)
	}
	if n := b.RecordCount(); n > e.limits.MaxRecordsPerBatch {
		return fail(fmt.Errorf("%w: %d records, limit %d", ErrTooLarge, n, e.limits.MaxRecordsPerBatch))
	}
	s, err := e.schemas.Resolve(b.SchemaVersion)
	if err != nil {
		return fail(err)
	}
	if errs := b.ValidateEnvelope(s); len(errs) > 0 {
		return fail(&BatchError{Errors: errs})
	}
	m, err := e.manifest(ctx, scope, b.Source.Adapter)
	if err != nil {
		return fail(err)
	}
	if !m.SupportsMode(b.Mode()) {
		return fail(fmt.Errorf("%w: %s does not declare %s", ErrModeNotSupported, m.Name, b.Mode()))
	}
	cur, err := e.currentRevision(ctx, scope)
	if err != nil {
		return fail(err)
	}
	current, err := e.store.Records(ctx, scope, cur)
	if err != nil {
		return fail(err)
	}
	var hash func(adapter.Record) string
	if e.hasher != nil {
		if hash, err = e.hasher(ctx, scope); err != nil {
			return fail(err)
		}
	}
	now := e.now()
	res, changes := ingest.Plan(b, ingest.Options{Schema: s, Manifest: m, Policy: e.policy, Current: current, IngestionID: e.newID("ing"), Now: now, Hash: hash})
	next := applyChanges(current, changes)
	if dryRun {
		res.DryRun = true
		return res, next, nil
	}
	ing := store.Ingestion{ID: res.IngestionID, Scope: scope, BatchID: res.BatchID, Source: b.Source, Mode: b.Mode(), RevisionBefore: cur, CreatedAt: now}
	if res.NoChanges {
		res.SnapshotID = store.SnapshotID(cur)
		ing.Result = mustJSON(res)
		if err := e.store.SaveIngestion(ctx, ing); err != nil {
			return fail(err)
		}
		return res, next, nil
	}
	res.SnapshotID = store.SnapshotID(cur + 1)
	ing.Result = mustJSON(res)
	commit := store.Commit{Scope: scope, ExpectedRevision: cur, Ingestion: ing, Changes: changes, Kind: store.KindIngestion, At: now}
	commit.Audit = []store.AuditEvent{e.auditEvent(ctx, scope, "ingestion.commit", "ingestion", ing.ID, map[string]any{
		"adapter": b.Source.Adapter, "adapter_version": b.Source.AdapterVersion, "mode": b.Mode(), "batch_id": res.BatchID,
		"snapshot_id": res.SnapshotID, "created": res.Created, "updated": res.Updated, "deleted": res.Deleted,
		"rejected_records": res.RejectedRecords,
	})}
	if _, err := e.store.Commit(ctx, commit); err != nil {
		return fail(err)
	}
	return res, next, nil
}

func (e *Engine) manifest(ctx context.Context, scope Scope, name string) (adapter.Manifest, error) {
	ms, err := e.store.Manifests(ctx, scope)
	if err != nil {
		return adapter.Manifest{}, err
	}
	for _, m := range ms {
		if m.Name == name {
			return m, nil
		}
	}
	return adapter.Manifest{}, fmt.Errorf("%w: %q", ErrUnknownAdapter, name)
}

func versionKey(v store.RecordVersion) string { return v.Entity + "\x00" + v.Key }

// applyChanges returns current with changes applied, sorted by entity then key.
func applyChanges(current []store.RecordVersion, changes []store.Change) []store.RecordVersion {
	byKey := map[string]store.RecordVersion{}
	for _, v := range current {
		byKey[versionKey(v)] = v
	}
	for _, c := range changes {
		k := c.Entity + "\x00" + c.Key
		if c.Op == store.OpDelete {
			delete(byKey, k)
		} else {
			byKey[k] = *c.Version
		}
	}
	out := make([]store.RecordVersion, 0, len(byKey))
	for _, v := range byKey {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return versionKey(out[i]) < versionKey(out[j]) })
	return out
}
