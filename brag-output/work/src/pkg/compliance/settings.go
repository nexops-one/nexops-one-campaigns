// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

// ErrInvalidSettings wraps every refused settings change.
var ErrInvalidSettings = errors.New("invalid settings")

// SettingsInput changes workspace settings; nil fields are unchanged.
// ReviewOverrides replaces the whole override map when given.
type SettingsInput struct {
	Retention       *store.RetentionPolicy          `json:"retention,omitempty"`
	ReviewOverrides map[string]store.ReviewOverride `json:"review_overrides,omitempty"`
	Sample          *bool                           `json:"sample,omitempty"`
}

// Settings returns the workspace's settings (defaults when never saved).
func (e *Engine) Settings(ctx context.Context, scope Scope) (store.Settings, error) {
	if err := scope.Validate(); err != nil {
		return store.Settings{}, err
	}
	return e.store.Settings(ctx, scope)
}

// UpdateSettings validates and saves a settings change, audited as settings.update.
func (e *Engine) UpdateSettings(ctx context.Context, scope Scope, in SettingsInput) (store.Settings, error) {
	if err := e.WorkspaceWritable(ctx, scope); err != nil {
		return store.Settings{}, err
	}
	cur, err := e.Settings(ctx, scope)
	if err != nil {
		return store.Settings{}, err
	}
	next := cur
	changed := []string{}
	if in.Retention != nil {
		r := *in.Retention
		if r.KeepRevisions < 1 || r.RevisionDays < 0 || r.EvaluationDays < 0 {
			return store.Settings{}, fmt.Errorf("%w: retention needs keep_revisions >= 1 and days >= 0 (0 keeps forever)", ErrInvalidSettings)
		}
		next.Retention = r
		changed = append(changed, "retention")
	}
	if in.ReviewOverrides != nil {
		for key, o := range in.ReviewOverrides {
			catalogName, control, ok := strings.Cut(key, "/")
			if !ok {
				return store.Settings{}, fmt.Errorf("%w: override key %q must be <catalog>/<control>", ErrInvalidSettings, key)
			}
			if _, _, err := e.controlOf(ctx, scope, catalogName, control); err != nil {
				return store.Settings{}, fmt.Errorf("%w: %v", ErrInvalidSettings, err)
			}
			if o.ReviewInterval != "" {
				if _, err := workflow.ParseDuration(o.ReviewInterval); err != nil {
					return store.Settings{}, fmt.Errorf("%w: %s: %v", ErrInvalidSettings, key, err)
				}
			}
		}
		next.ReviewOverrides = in.ReviewOverrides
		changed = append(changed, "review_overrides")
	}
	if in.Sample != nil {
		next.Sample = *in.Sample
		changed = append(changed, "sample")
	}
	if len(changed) == 0 {
		return cur, nil
	}
	sort.Strings(changed)
	next.Scope, next.UpdatedAt = scope, e.now()
	ev := e.auditEvent(ctx, scope, "settings.update", "settings", scope.WorkspaceID, map[string]any{"changed": changed, "retention": next.Retention, "sample": next.Sample, "overrides": len(next.ReviewOverrides)})
	return e.store.PutSettings(ctx, next, cur.Version, ev)
}

// SettingsExtension returns an edition's workspace setting, or nil when it
// is not set.
func (e *Engine) SettingsExtension(ctx context.Context, scope Scope, key string) (json.RawMessage, error) {
	cur, err := e.Settings(ctx, scope)
	if err != nil {
		return nil, err
	}
	return cur.Extensions[key], nil
}

// PutSettingsExtension stores an edition's workspace setting under key, or
// removes it when value is nil. The edition validates value, which must hold
// no secret: the change is audited as settings.update with the value.
func (e *Engine) PutSettingsExtension(ctx context.Context, scope Scope, key string, value json.RawMessage) (store.Settings, error) {
	if key == "" {
		return store.Settings{}, fmt.Errorf("%w: an extension key is required", ErrInvalidSettings)
	}
	if value != nil && !json.Valid(value) {
		return store.Settings{}, fmt.Errorf("%w: %s is not valid JSON", ErrInvalidSettings, key)
	}
	if err := e.WorkspaceWritable(ctx, scope); err != nil {
		return store.Settings{}, err
	}
	cur, err := e.Settings(ctx, scope)
	if err != nil {
		return store.Settings{}, err
	}
	next := cur
	next.Extensions = map[string]json.RawMessage{}
	for k, v := range cur.Extensions {
		next.Extensions[k] = v
	}
	if value == nil {
		delete(next.Extensions, key)
	} else {
		next.Extensions[key] = append(json.RawMessage{}, value...)
	}
	next.Scope, next.UpdatedAt = scope, e.now()
	ev := e.auditEvent(ctx, scope, "settings.update", "settings", scope.WorkspaceID, map[string]any{
		"changed": []string{"extensions"}, "extension": key, "removed": value == nil, "value": value,
	})
	return e.store.PutSettings(ctx, next, cur.Version, ev)
}
