// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"fmt"

	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// Snapshot is one immutable workspace state.
type Snapshot struct {
	ID       string                `json:"id"`
	Revision store.Revision        `json:"revision"`
	Records  []store.RecordVersion `json:"records"`
}

// Snapshot returns a snapshot by ID ("" is the current one, "rev-0" is empty).
func (e *Engine) Snapshot(ctx context.Context, scope Scope, id string) (Snapshot, error) {
	if err := scope.Validate(); err != nil {
		return Snapshot{}, err
	}
	n, err := e.resolveSnapshot(ctx, scope, id)
	if err != nil {
		return Snapshot{}, err
	}
	rev := store.Revision{Scope: scope, SnapshotID: store.SnapshotID(0)}
	if n > 0 {
		if rev, err = e.store.Revision(ctx, scope, n); err != nil {
			return Snapshot{}, err
		}
	}
	recs, err := e.store.Records(ctx, scope, n)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{ID: store.SnapshotID(n), Revision: rev, Records: recs}, nil
}

func (e *Engine) resolveSnapshot(ctx context.Context, scope Scope, id string) (int64, error) {
	if id == "" {
		return e.currentRevision(ctx, scope)
	}
	return store.ParseSnapshotID(id)
}

// Snapshots lists every revision of the workspace, oldest first.
func (e *Engine) Snapshots(ctx context.Context, scope Scope) ([]store.Revision, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return e.store.Revisions(ctx, scope)
}

// Ingestion returns the stored record of an ingestion or rollback. Its Result
// holds the JSON-encoded ingest.Result (or RollbackResult).
func (e *Engine) Ingestion(ctx context.Context, scope Scope, id string) (store.Ingestion, error) {
	if err := scope.Validate(); err != nil {
		return store.Ingestion{}, err
	}
	return e.store.Ingestion(ctx, scope, id)
}

// RollbackResult describes a rollback commit.
type RollbackResult struct {
	IngestionID string `json:"ingestion_id"`
	RolledBack  string `json:"rolled_back"`
	SnapshotID  string `json:"snapshot_id"`
	Created     int    `json:"created"`
	Updated     int    `json:"updated"`
	Deleted     int    `json:"deleted"`
}

// RollbackSource identifies rollback commits in the provenance log.
var RollbackSource = adapter.Source{System: "compliance-engine", Adapter: "rollback", AdapterVersion: "1"}

// Rollback creates a new revision whose content equals the revision before
// ingestionID. Ingestions committed after it are reverted as well. History
// is never rewritten.
func (e *Engine) Rollback(ctx context.Context, scope Scope, ingestionID string) (RollbackResult, error) {
	if err := scope.Validate(); err != nil {
		return RollbackResult{}, err
	}
	if _, err := e.require(ctx, scope, extension.FeatureRegisterIngest); err != nil {
		return RollbackResult{}, err
	}
	ing, err := e.store.Ingestion(ctx, scope, ingestionID)
	if err != nil {
		return RollbackResult{}, err
	}
	if ing.RevisionAfter == 0 {
		return RollbackResult{}, fmt.Errorf("%w: %s", ErrNothingToRollBack, ingestionID)
	}
	if ing.RolledBackBy != "" {
		return RollbackResult{}, fmt.Errorf("%w: %s by %s", ErrAlreadyRolledBack, ingestionID, ing.RolledBackBy)
	}
	cur, err := e.currentRevision(ctx, scope)
	if err != nil {
		return RollbackResult{}, err
	}
	current, err := e.store.Records(ctx, scope, cur)
	if err != nil {
		return RollbackResult{}, err
	}
	target, err := e.store.Records(ctx, scope, ing.RevisionBefore)
	if err != nil {
		return RollbackResult{}, err
	}
	reverted, err := e.revertedIngestions(ctx, scope, ing.RevisionBefore)
	if err != nil {
		return RollbackResult{}, err
	}
	changes := diffVersions(current, target)
	res := RollbackResult{IngestionID: e.newID("rbk"), RolledBack: ingestionID, SnapshotID: store.SnapshotID(cur + 1)}
	for _, c := range changes {
		switch c.Op {
		case store.OpCreate:
			res.Created++
		case store.OpUpdate:
			res.Updated++
		case store.OpDelete:
			res.Deleted++
		}
	}
	now := e.now()
	commit := store.Commit{
		Scope: scope, ExpectedRevision: cur, Changes: changes, Kind: store.KindRollback, RollsBack: reverted, At: now,
		Ingestion: store.Ingestion{ID: res.IngestionID, Scope: scope, Source: RollbackSource, RevisionBefore: cur, Result: mustJSON(res), CreatedAt: now},
		Audit: []store.AuditEvent{e.auditEvent(ctx, scope, "ingestion.rollback", "ingestion", ingestionID, map[string]any{
			"rollback_ingestion_id": res.IngestionID, "snapshot_id": res.SnapshotID, "reverted": reverted,
			"created": res.Created, "updated": res.Updated, "deleted": res.Deleted,
		})},
	}
	if _, err := e.store.Commit(ctx, commit); err != nil {
		return RollbackResult{}, err
	}
	return res, nil
}

// revertedIngestions lists every ingestion committed after revision `after`
// that is not yet rolled back: restoring `after` reverts all of them, so all
// of them are marked, and none can later be rolled back to resurrect data.
func (e *Engine) revertedIngestions(ctx context.Context, scope Scope, after int64) ([]string, error) {
	revs, err := e.store.Revisions(ctx, scope)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, r := range revs {
		if r.Number <= after {
			continue
		}
		ing, err := e.store.Ingestion(ctx, scope, r.IngestionID)
		if err != nil {
			return nil, err
		}
		if ing.RolledBackBy == "" {
			ids = append(ids, r.IngestionID)
		}
	}
	return ids, nil
}

// diffVersions returns the changes that turn current into target.
func diffVersions(current, target []store.RecordVersion) []store.Change {
	cur := map[string]store.RecordVersion{}
	for _, v := range current {
		cur[versionKey(v)] = v
	}
	tgt := map[string]bool{}
	var changes []store.Change
	for _, v := range target {
		tgt[versionKey(v)] = true
		prev, exists := cur[versionKey(v)]
		switch {
		case exists && prev.Hash == v.Hash:
		case exists:
			changes = append(changes, store.Change{Op: store.OpUpdate, Entity: v.Entity, Key: v.Key, Version: &v, PreviousHash: prev.Hash})
		default:
			changes = append(changes, store.Change{Op: store.OpCreate, Entity: v.Entity, Key: v.Key, Version: &v})
		}
	}
	for _, v := range current {
		if !tgt[versionKey(v)] {
			changes = append(changes, store.Change{Op: store.OpDelete, Entity: v.Entity, Key: v.Key, PreviousHash: v.Hash})
		}
	}
	return changes
}

// Provenance returns the change history of one record.
func (e *Engine) Provenance(ctx context.Context, scope Scope, entity, key string) ([]store.ProvenanceEntry, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return e.store.Provenance(ctx, scope, entity, key)
}
