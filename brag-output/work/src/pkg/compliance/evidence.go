// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/evidence"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

var (
	// ErrInvalidEvidence wraps every refused evidence input.
	ErrInvalidEvidence = evidence.ErrInvalid
	// ErrUnknownControl means no loaded catalog version has the control.
	ErrUnknownControl = errors.New("unknown catalog control")
	// ErrAlreadyRevoked means the evidence item was revoked before.
	ErrAlreadyRevoked = errors.New("evidence was already revoked")
	// ErrReasonRequired means the action needs a reason.
	ErrReasonRequired = errors.New("a reason is required")
	// ErrNoChecksum means the evidence has no checksum to compare an
	// engine-side check with; an attested check supplies one.
	ErrNoChecksum = errors.New("the evidence has no recorded checksum: run an attested check (compliance-engine evidence verify --file) to record one")
)

// EvidenceInput describes an evidence reference to attach.
type EvidenceInput struct {
	Title       string             `json:"title"`
	Kind        string             `json:"kind"`
	Source      string             `json:"source"`
	URI         string             `json:"uri"`
	Checksum    string             `json:"checksum"`
	CollectedAt *time.Time         `json:"collected_at,omitempty"`
	ValidUntil  *time.Time         `json:"valid_until,omitempty"`
	Retention   store.Retention    `json:"retention"`
	Links       []store.ControlRef `json:"links,omitempty"`
}

// checkControl verifies that some loaded catalog version has the control.
func (e *Engine) checkControl(ref store.ControlRef) error {
	for _, r := range e.catalogs.Refs() {
		if r.Catalog != ref.Catalog {
			continue
		}
		c, _ := e.catalogs.Get(r)
		for _, ctl := range c.Controls {
			if ctl.ID == ref.ControlID {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: %s/%s", ErrUnknownControl, ref.Catalog, ref.ControlID)
}

// uriHost keeps the scheme and host of a URI for audit details.
func uriHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// AddEvidence attaches a reference to an evidence object kept in the
// customer's environment. The object itself is never copied.
func (e *Engine) AddEvidence(ctx context.Context, scope Scope, in EvidenceInput) (store.Evidence, error) {
	if err := scope.Validate(); err != nil {
		return store.Evidence{}, err
	}
	if _, err := e.require(ctx, scope, extension.FeatureEvidenceManage); err != nil {
		return store.Evidence{}, err
	}
	return e.addEvidence(ctx, scope, in, false)
}

// addEvidence validates and stores evidence; managed items carry an engine-set URI.
func (e *Engine) addEvidence(ctx context.Context, scope Scope, in EvidenceInput, managed bool) (store.Evidence, error) {
	in.Title, in.Source, in.URI = strings.TrimSpace(in.Title), strings.TrimSpace(in.Source), strings.TrimSpace(in.URI)
	switch {
	case in.Title == "":
		return store.Evidence{}, fmt.Errorf("%w: title is required", ErrInvalidEvidence)
	case in.Source == "":
		return store.Evidence{}, fmt.Errorf("%w: source is required", ErrInvalidEvidence)
	case in.Retention.MinDays < 0:
		return store.Evidence{}, fmt.Errorf("%w: retention.min_days must not be negative", ErrInvalidEvidence)
	}
	uriCheck := evidence.ValidateURI(in.URI)
	if managed {
		uriCheck = nil
	}
	integrity, checksumCheck := store.IntegrityUnverified, evidence.ValidateChecksum(in.Checksum)
	if in.Checksum == "" && !managed && e.workflow.AllowEvidenceWithoutChecksum {
		integrity, checksumCheck = store.IntegrityNoChecksum, nil
	}
	for _, check := range []error{evidence.ValidateKind(in.Kind), uriCheck, checksumCheck} {
		if check != nil {
			return store.Evidence{}, check
		}
	}
	links := []store.ControlRef{}
	for _, l := range in.Links {
		if err := e.checkControl(l); err != nil {
			return store.Evidence{}, err
		}
		dup := false
		for _, x := range links {
			dup = dup || x == l
		}
		if !dup {
			links = append(links, l)
		}
	}
	now := e.now()
	collected := now
	if in.CollectedAt != nil {
		collected = in.CollectedAt.UTC()
	}
	actor, _ := e.actor(ctx)
	ev := store.Evidence{
		ID: e.newID("evd"), Scope: scope, Title: in.Title, Kind: in.Kind, Source: in.Source, URI: in.URI, Checksum: in.Checksum,
		CollectedAt: collected, Collector: actor, ValidUntil: in.ValidUntil, Retention: in.Retention,
		Integrity: integrity, Checks: []store.EvidenceCheck{}, Links: links, CreatedAt: now,
	}
	audit := e.auditEvent(ctx, scope, "evidence.create", "evidence", ev.ID, map[string]any{
		"title": ev.Title, "kind": ev.Kind, "source": ev.Source, "location": uriHost(ev.URI), "checksum": ev.Checksum, "links": links,
	})
	return e.store.PutEvidence(ctx, ev, 0, audit)
}

// Evidence returns one evidence item and logs the access.
func (e *Engine) Evidence(ctx context.Context, scope Scope, id string) (store.Evidence, error) {
	if err := scope.Validate(); err != nil {
		return store.Evidence{}, err
	}
	ev, err := e.store.Evidence(ctx, scope, id)
	if err != nil {
		return store.Evidence{}, err
	}
	if err := e.store.AppendAudit(ctx, e.auditEvent(ctx, scope, "evidence.access", "evidence", id, map[string]any{"ids": []string{id}})); err != nil {
		return store.Evidence{}, err
	}
	return ev, nil
}

// ListEvidence lists evidence (optionally of one control) and logs the access.
func (e *Engine) ListEvidence(ctx context.Context, scope Scope, q store.EvidenceQuery) ([]store.Evidence, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	evs, err := e.store.ListEvidence(ctx, scope, q)
	if err != nil || len(evs) == 0 {
		return evs, err
	}
	ids := make([]string, len(evs))
	for i, x := range evs {
		ids[i] = x.ID
	}
	if err := e.store.AppendAudit(ctx, e.auditEvent(ctx, scope, "evidence.access", "evidence", "", map[string]any{"ids": ids})); err != nil {
		return nil, err
	}
	return evs, nil
}

// updateEvidence applies change to the stored item and saves it with its audit event.
func (e *Engine) updateEvidence(ctx context.Context, scope Scope, id, action string, change func(*store.Evidence) (map[string]any, error)) (store.Evidence, error) {
	if err := scope.Validate(); err != nil {
		return store.Evidence{}, err
	}
	if _, err := e.require(ctx, scope, extension.FeatureEvidenceManage); err != nil {
		return store.Evidence{}, err
	}
	ev, err := e.store.Evidence(ctx, scope, id)
	if err != nil {
		return store.Evidence{}, err
	}
	version := ev.Version
	details, err := change(&ev)
	if err != nil {
		return store.Evidence{}, err
	}
	if details == nil {
		return ev, nil // nothing changed
	}
	return e.store.PutEvidence(ctx, ev, version, e.auditEvent(ctx, scope, action, "evidence", id, details))
}

// LinkEvidence links an evidence item to a control.
func (e *Engine) LinkEvidence(ctx context.Context, scope Scope, id string, ref store.ControlRef) (store.Evidence, error) {
	if err := e.checkControl(ref); err != nil {
		return store.Evidence{}, err
	}
	return e.updateEvidence(ctx, scope, id, "evidence.link", func(ev *store.Evidence) (map[string]any, error) {
		if ev.LinkedTo(ref) {
			return nil, nil
		}
		ev.Links = append(ev.Links, ref)
		return map[string]any{"catalog": ref.Catalog, "control_id": ref.ControlID}, nil
	})
}

// UnlinkEvidence removes a link between an evidence item and a control.
func (e *Engine) UnlinkEvidence(ctx context.Context, scope Scope, id string, ref store.ControlRef) (store.Evidence, error) {
	return e.updateEvidence(ctx, scope, id, "evidence.unlink", func(ev *store.Evidence) (map[string]any, error) {
		if !ev.LinkedTo(ref) {
			return nil, fmt.Errorf("%w: evidence %s is not linked to %s/%s", store.ErrNotFound, id, ref.Catalog, ref.ControlID)
		}
		kept := []store.ControlRef{}
		for _, l := range ev.Links {
			if l != ref {
				kept = append(kept, l)
			}
		}
		ev.Links = kept
		return map[string]any{"catalog": ref.Catalog, "control_id": ref.ControlID}, nil
	})
}

// RevokeEvidence revokes an evidence item: it stops supporting approvals
// and stays auditable.
func (e *Engine) RevokeEvidence(ctx context.Context, scope Scope, id, reason string) (store.Evidence, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return store.Evidence{}, ErrReasonRequired
	}
	actor, _ := e.actor(ctx)
	return e.updateEvidence(ctx, scope, id, "evidence.revoke", func(ev *store.Evidence) (map[string]any, error) {
		if ev.RevokedAt != nil {
			return nil, fmt.Errorf("%w: %s", ErrAlreadyRevoked, id)
		}
		now := e.now()
		ev.RevokedAt, ev.RevokedBy, ev.RevokeReason = &now, actor, reason
		return map[string]any{"reason_given": true}, nil
	})
}

// VerifyEvidence hashes the object when the engine may read it (a file under
// the evidence root, or an allowlisted HTTPS host) and records the check.
func (e *Engine) VerifyEvidence(ctx context.Context, scope Scope, id string) (store.Evidence, error) {
	if err := scope.Validate(); err != nil {
		return store.Evidence{}, err
	}
	ev, err := e.store.Evidence(ctx, scope, id)
	if err != nil {
		return store.Evidence{}, err
	}
	if ev.Checksum == "" {
		return store.Evidence{}, fmt.Errorf("%w: %s", ErrNoChecksum, id)
	}
	observed, isManaged, err := e.managedChecksum(ctx, scope, ev.URI)
	if err != nil {
		return store.Evidence{}, err
	}
	method := "engine_managed"
	if !isManaged {
		if observed, err = e.workflow.Verifier.Checksum(ctx, ev.URI); err != nil {
			return store.Evidence{}, err
		}
		method = "engine_file"
		if strings.HasPrefix(ev.URI, "https:") {
			method = "engine_https"
		}
	}
	return e.recordCheck(ctx, scope, id, method, observed)
}

// AttestEvidenceCheck records a checksum computed by the customer. Evidence
// recorded without a checksum adopts the observed one (method attested_adopt).
func (e *Engine) AttestEvidenceCheck(ctx context.Context, scope Scope, id, observed string) (store.Evidence, error) {
	if err := evidence.ValidateChecksum(observed); err != nil {
		return store.Evidence{}, err
	}
	return e.recordCheck(ctx, scope, id, "attested", observed)
}

func (e *Engine) recordCheck(ctx context.Context, scope Scope, id, method, observed string) (store.Evidence, error) {
	actor, _ := e.actor(ctx)
	return e.updateEvidence(ctx, scope, id, "evidence.check", func(ev *store.Evidence) (map[string]any, error) {
		if ev.Checksum == "" {
			if method != "attested" {
				return nil, fmt.Errorf("%w: %s", ErrNoChecksum, id)
			}
			ev.Checksum, method = observed, "attested_adopt"
		}
		match := observed == ev.Checksum
		ev.Checks = append(ev.Checks, store.EvidenceCheck{At: e.now(), Actor: actor, Method: method, Observed: observed, Match: match})
		ev.Integrity = store.IntegrityFailed
		if match {
			ev.Integrity = store.IntegrityVerified
		}
		return map[string]any{"method": method, "match": match, "observed": observed}, nil
	})
}

func checksumBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
