// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

// Evaluation is a persisted evaluation of a snapshot against catalog versions.
type Evaluation struct {
	ID         string        `json:"id"`
	SnapshotID string        `json:"snapshot_id"`
	Catalogs   []catalog.Ref `json:"catalogs"`
	CreatedAt  time.Time     `json:"created_at"`
	Result     engine.Result `json:"result"`
	// Effective combines Result with the workspace's assessments and evidence
	// at CreatedAt; it embeds its inputs so it can be recomputed.
	Effective *workflow.Result `json:"effective,omitempty"`
}

// Evaluate evaluates a snapshot ("" = current) against catalog versions (none
// = latest version of every catalog) and stores the result.
func (e *Engine) Evaluate(ctx context.Context, scope Scope, snapshotID string, refs []catalog.Ref) (Evaluation, error) {
	if err := scope.Validate(); err != nil {
		return Evaluation{}, err
	}
	if _, err := e.require(ctx, scope, extension.FeatureEvaluationRun); err != nil {
		return Evaluation{}, err
	}
	cats, err := e.resolveCatalogs(ctx, scope, refs)
	if err != nil {
		return Evaluation{}, err
	}
	res, err := e.compute(ctx, scope, snapshotID, cats)
	if err != nil {
		return Evaluation{}, err
	}
	now := e.now()
	eff, err := e.effective(ctx, scope, res, cats, now)
	if err != nil {
		return Evaluation{}, err
	}
	ev := Evaluation{ID: e.newID("eval"), SnapshotID: res.SnapshotID, Catalogs: []catalog.Ref{}, CreatedAt: now, Result: res, Effective: &eff}
	names := []string{}
	for _, c := range cats {
		ev.Catalogs = append(ev.Catalogs, c.Ref())
		names = append(names, c.Ref().String())
	}
	stored := store.StoredEvaluation{ID: ev.ID, Scope: scope, SnapshotID: res.SnapshotID, Catalogs: names, Result: mustJSON(ev), CreatedAt: ev.CreatedAt}
	if err := e.store.SaveEvaluation(ctx, stored); err != nil {
		return Evaluation{}, err
	}
	if err := e.persistVoids(ctx, scope, eff); err != nil {
		return Evaluation{}, err
	}
	return ev, nil
}

// Evaluation returns a stored evaluation.
func (e *Engine) Evaluation(ctx context.Context, scope Scope, id string) (Evaluation, error) {
	if err := scope.Validate(); err != nil {
		return Evaluation{}, err
	}
	st, err := e.store.Evaluation(ctx, scope, id)
	if err != nil {
		return Evaluation{}, err
	}
	var ev Evaluation
	if err := json.Unmarshal(st.Result, &ev); err != nil {
		return Evaluation{}, fmt.Errorf("compliance: decode evaluation %s: %w", id, err)
	}
	return ev, nil
}

// Catalog returns one loaded catalog version.
func (e *Engine) Catalog(ref catalog.Ref) (*catalog.Catalog, error) {
	c, ok := e.catalogs.Get(ref)
	if !ok {
		return nil, fmt.Errorf("%w: %s", catalog.ErrUnknownCatalog, ref)
	}
	var clone catalog.Catalog // a deep copy: callers must not be able to change loaded catalogs
	if err := json.Unmarshal(mustJSON(c), &clone); err != nil {
		return nil, fmt.Errorf("compliance: copy catalog %s: %w", ref, err)
	}
	return &clone, nil
}

// CatalogDiff compares two loaded catalog versions.
func (e *Engine) CatalogDiff(a, b catalog.Ref) (catalog.Diff, error) {
	ca, err := e.Catalog(a)
	if err != nil {
		return catalog.Diff{}, err
	}
	cb, err := e.Catalog(b)
	if err != nil {
		return catalog.Diff{}, err
	}
	return catalog.DiffCatalogs(ca, cb), nil
}

// Completeness returns the data-completeness view of a snapshot ("" = current).
func (e *Engine) Completeness(ctx context.Context, scope Scope, snapshotID string) (canonical.Completeness, error) {
	snap, err := e.Snapshot(ctx, scope, snapshotID)
	if err != nil {
		return canonical.Completeness{}, err
	}
	c, err := e.completeness(ctx, scope, snap.Records)
	if err != nil {
		return canonical.Completeness{}, err
	}
	c.SnapshotID = snap.ID
	return c, nil
}

func (e *Engine) completeness(ctx context.Context, scope Scope, recs []store.RecordVersion) (canonical.Completeness, error) {
	sup, err := e.supply(ctx, scope)
	if err != nil {
		return canonical.Completeness{}, err
	}
	return canonical.AnalyzeCompleteness(e.index(recs), e.codelists, sup), nil
}

// supply reports whether any manifest registered in scope declares entity.field.
func (e *Engine) supply(ctx context.Context, scope Scope) (canonical.Supply, error) {
	ms, err := e.store.Manifests(ctx, scope)
	if err != nil {
		return nil, err
	}
	return func(entity, field string) bool {
		for _, m := range ms {
			if m.SuppliesField(entity, field) {
				return true
			}
		}
		return false
	}, nil
}

func (e *Engine) index(recs []store.RecordVersion) *canonical.Index {
	out := make([]canonical.SnapshotRecord, len(recs))
	for i, r := range recs {
		out[i] = canonical.SnapshotRecord{Entity: r.Entity, Key: r.Key, Data: r.Data}
	}
	return canonical.NewIndex(e.schemas.Latest(), out)
}
