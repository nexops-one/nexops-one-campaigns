// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"errors"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

// ControlStatus is the live view of one control: the current snapshot
// against the latest version of the control's catalog. It stores nothing.
type ControlStatus struct {
	Catalog    catalog.Ref          `json:"catalog"`
	Framework  string               `json:"framework"`
	Control    catalog.Control      `json:"control"`
	SnapshotID string               `json:"snapshot_id"`
	Computed   engine.ControlResult `json:"computed"`
	Effective  workflow.Control     `json:"effective"`
}

// ControlStatus computes one control's rule explanation, blockers and
// effective status now.
func (e *Engine) ControlStatus(ctx context.Context, scope Scope, catalogName, controlID string) (ControlStatus, error) {
	if err := scope.Validate(); err != nil {
		return ControlStatus{}, err
	}
	cat, ctl, err := e.controlOf(ctx, scope, catalogName, controlID)
	if err != nil {
		return ControlStatus{}, err
	}
	cats := []*catalog.Catalog{cat}
	computed, err := e.compute(ctx, scope, "", cats)
	if err != nil {
		return ControlStatus{}, err
	}
	eff, err := e.effective(ctx, scope, computed, cats, e.now())
	if err != nil {
		return ControlStatus{}, err
	}
	out := ControlStatus{Catalog: cat.Ref(), Framework: cat.Framework, Control: ctl, SnapshotID: computed.SnapshotID}
	for _, c := range computed.Frameworks[0].Controls {
		if c.ControlID == controlID {
			out.Computed = c
		}
	}
	for _, c := range eff.Frameworks[0].Controls {
		if c.ControlID == controlID {
			out.Effective = c
		}
	}
	return out, nil
}

// Emails maps the user actors among actors ("user:<id>") to their emails.
// Other actors (tokens, system, migrations) and removed users are absent
// from the map, to be shown as recorded.
func (e *Engine) Emails(ctx context.Context, scope Scope, actors ...string) (map[string]string, error) {
	out := map[string]string{}
	for _, actor := range actors {
		id, ok := strings.CutPrefix(actor, "user:")
		if !ok || out[actor] != "" {
			continue
		}
		u, err := e.store.User(ctx, scope.TenantID, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[actor] = u.Email
	}
	return out, nil
}
