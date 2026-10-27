// SPDX-License-Identifier: Apache-2.0

// Package audit seals and verifies the hash chain of audit events. Each
// event's hash covers its content and the previous event's hash, so an edit,
// deletion or reordering anywhere in a chain is detected by Verify.
package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/store"
)

// canonical is the hashed form of an event; field order is part of the format.
type canonical struct {
	Seq         int64           `json:"seq"`
	TenantID    string          `json:"tenant_id"`
	WorkspaceID string          `json:"workspace_id"`
	At          string          `json:"at"`
	Actor       string          `json:"actor"`
	ActorKind   string          `json:"actor_kind"`
	Action      string          `json:"action"`
	TargetType  string          `json:"target_type"`
	TargetID    string          `json:"target_id"`
	Details     json.RawMessage `json:"details"`
}

// Hash computes an event's chain hash from the previous event's hash.
func Hash(prevHash string, e store.AuditEvent) string {
	data, err := json.Marshal(canonical{
		Seq: e.Seq, TenantID: e.Scope.TenantID, WorkspaceID: e.Scope.WorkspaceID,
		At: e.At.UTC().Format(time.RFC3339Nano), Actor: e.Actor, ActorKind: e.ActorKind, Action: e.Action,
		TargetType: e.TargetType, TargetID: e.TargetID, Details: e.Details,
	})
	if err != nil {
		panic(fmt.Sprintf("audit: encode event: %v", err)) // Details is validated JSON
	}
	sum := sha256.Sum256(append([]byte(prevHash+"\n"), data...))
	return hex.EncodeToString(sum[:])
}

// Seal normalizes events and chains them after the event (prevSeq, prevHash)
// of the same scope; prevSeq 0 and prevHash "" start a chain. Times are
// truncated to microseconds (the PostgreSQL precision) and details compacted,
// so the stored form hashes to the same value.
func Seal(prevSeq int64, prevHash string, events []store.AuditEvent) ([]store.AuditEvent, error) {
	out := make([]store.AuditEvent, len(events))
	for i, e := range events {
		if i > 0 && e.Scope != events[0].Scope {
			return nil, errors.New("audit: events of different scopes cannot be sealed together")
		}
		if strings.TrimSpace(e.Scope.TenantID) == "" {
			return nil, errors.New("audit: event has no tenant")
		}
		if strings.TrimSpace(e.Action) == "" || strings.TrimSpace(e.Actor) == "" {
			return nil, fmt.Errorf("audit: event %q by %q: action and actor are required", e.Action, e.Actor)
		}
		details := e.Details
		if len(bytes.TrimSpace(details)) == 0 {
			details = json.RawMessage(`{}`)
		}
		var buf bytes.Buffer
		if err := json.Compact(&buf, details); err != nil {
			return nil, fmt.Errorf("audit: event %s: details are not valid JSON: %w", e.Action, err)
		}
		e.Details = json.RawMessage(buf.Bytes())
		e.At = e.At.UTC().Truncate(time.Microsecond)
		prevSeq++
		e.Seq, e.PrevHash = prevSeq, prevHash
		e.Hash = Hash(prevHash, e)
		prevHash = e.Hash
		out[i] = e
	}
	return out, nil
}

// Result is the outcome of verifying a chain.
type Result struct {
	OK       bool   `json:"ok"`
	Checked  int    `json:"checked"`
	BrokenAt int64  `json:"broken_at,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Reasons reported by Verify.
const (
	ReasonSequenceGap      = "sequence_gap"
	ReasonPrevHashMismatch = "prev_hash_mismatch"
	ReasonHashMismatch     = "hash_mismatch"
)

// Verify checks a complete chain (or a suffix of one: the first event's
// PrevHash is then trusted) and reports the first broken event.
func Verify(events []store.AuditEvent) Result {
	for i, e := range events {
		if i > 0 {
			prev := events[i-1]
			if e.Seq != prev.Seq+1 {
				return Result{Checked: i, BrokenAt: e.Seq, Reason: ReasonSequenceGap}
			}
			if e.PrevHash != prev.Hash {
				return Result{Checked: i, BrokenAt: e.Seq, Reason: ReasonPrevHashMismatch}
			}
		} else if e.Seq == 1 && e.PrevHash != "" {
			return Result{Checked: 0, BrokenAt: e.Seq, Reason: ReasonPrevHashMismatch}
		}
		if Hash(e.PrevHash, e) != e.Hash {
			return Result{Checked: i, BrokenAt: e.Seq, Reason: ReasonHashMismatch}
		}
	}
	return Result{OK: true, Checked: len(events)}
}
