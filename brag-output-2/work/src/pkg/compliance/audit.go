// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"

	"github.com/nexops-one/compliance-engine/pkg/audit"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

// LibraryActor is recorded for engine actions whose context carries no principal.
const LibraryActor = "library"

// auditEvent builds an event attributed to the principal carried by ctx (see
// extension.WithPrincipal), or to LibraryActor when there is none.
// actor returns the principal carried by ctx, or LibraryActor.
func (e *Engine) actor(ctx context.Context) (string, string) {
	if p, ok := extension.PrincipalFrom(ctx); ok && p.Actor != "" {
		if p.Kind == "" {
			return p.Actor, string(extension.ActorUser)
		}
		return p.Actor, string(p.Kind)
	}
	return LibraryActor, string(extension.ActorSystem)
}

func (e *Engine) auditEvent(ctx context.Context, scope Scope, action, targetType, targetID string, details any) store.AuditEvent {
	actor, kind := e.actor(ctx)
	return store.AuditEvent{Scope: scope, At: e.now(), Actor: actor, ActorKind: kind, Action: action,
		TargetType: targetType, TargetID: targetID, Details: mustJSON(details)}
}

// AuditEvents pages through the workspace's audit log in ascending order.
func (e *Engine) AuditEvents(ctx context.Context, scope Scope, q store.AuditQuery) ([]store.AuditEvent, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return e.store.AuditEvents(ctx, scope, q)
}

// VerifyAudit recomputes the workspace's whole audit chain.
func (e *Engine) VerifyAudit(ctx context.Context, scope Scope) (audit.Result, error) {
	if err := scope.Validate(); err != nil {
		return audit.Result{}, err
	}
	return VerifyChain(ctx, e.store, scope)
}

// VerifyChain recomputes one audit chain of st, reading it in pages.
func VerifyChain(ctx context.Context, st store.AuditLog, scope Scope) (audit.Result, error) {
	const page = 1000
	var all []store.AuditEvent
	var after int64
	for {
		evs, err := st.AuditEvents(ctx, scope, store.AuditQuery{AfterSeq: after, Limit: page})
		if err != nil {
			return audit.Result{}, err
		}
		all = append(all, evs...)
		if len(evs) < page {
			return audit.Verify(all), nil
		}
		after = evs[len(evs)-1].Seq
	}
}
