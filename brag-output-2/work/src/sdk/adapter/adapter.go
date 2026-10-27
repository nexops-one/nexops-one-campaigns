// SPDX-License-Identifier: Apache-2.0

// Package adapter is the adapter SDK: the contract between a data source and
// the compliance engine. An adapter declares a Manifest and produces Batches of
// canonical records. It never receives a store or engine handle.
package adapter

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// Scope identifies the tenant and workspace data belongs to. The engine binds
// it; adapters never choose it.
type Scope struct {
	TenantID    string `json:"tenant_id"`
	WorkspaceID string `json:"workspace_id"`
}

// Validate checks both identifiers are present.
func (s Scope) Validate() error {
	if strings.TrimSpace(s.TenantID) == "" || strings.TrimSpace(s.WorkspaceID) == "" {
		return errors.New("scope: tenant_id and workspace_id are required")
	}
	return nil
}

// Mode is the synchronization mode of a batch.
type Mode string

const (
	// ModeFull: the batch is the complete set of records the adapter supplies;
	// its own records absent from the batch are deleted.
	ModeFull Mode = "full"
	// ModeIncremental: records are upserted; nothing is deleted.
	ModeIncremental Mode = "incremental"
)

// Manifest is an adapter's capability declaration.
type Manifest struct {
	Name          string              `json:"name"`
	Version       string              `json:"version"`
	SchemaVersion string              `json:"schema_version"`
	Supplies      map[string][]string `json:"supplies"` // entity -> fields the adapter can provide
	Modes         []Mode              `json:"modes"`
}

// Validate checks the manifest against the registry. An adapter that supplies
// an entity must supply all of its identity fields.
func (m Manifest) Validate(reg *schema.Registry) error {
	var errs []error
	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, errors.New("manifest: name is required"))
	}
	if strings.TrimSpace(m.Version) == "" {
		errs = append(errs, errors.New("manifest: version is required"))
	}
	s, err := reg.Resolve(m.SchemaVersion)
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("manifest: %w", err))...)
	}
	for entity, fields := range m.Supplies {
		e, ok := s.Entity(entity)
		if !ok {
			errs = append(errs, fmt.Errorf("manifest: unknown entity %q", entity))
			continue
		}
		for _, f := range fields {
			if _, ok := e.Fields[f]; !ok {
				errs = append(errs, fmt.Errorf("manifest: unknown field %s.%s", entity, f))
			}
		}
		for _, k := range e.IdentityKey {
			if !slices.Contains(fields, k) {
				errs = append(errs, fmt.Errorf("manifest: %s supplies the entity but not its identity field %s", entity, k))
			}
		}
	}
	if len(m.Modes) == 0 {
		errs = append(errs, errors.New("manifest: at least one mode is required"))
	}
	for _, mode := range m.Modes {
		if mode != ModeFull && mode != ModeIncremental {
			errs = append(errs, fmt.Errorf("manifest: unknown mode %q", mode))
		}
	}
	return errors.Join(errs...)
}

// SuppliesField reports whether the manifest declares entity.field.
func (m Manifest) SuppliesField(entity, field string) bool {
	return slices.Contains(m.Supplies[entity], field)
}

// SupportsMode reports whether the manifest declares mode.
func (m Manifest) SupportsMode(mode Mode) bool { return slices.Contains(m.Modes, mode) }

// SyncRequest asks an adapter for a batch.
type SyncRequest struct {
	Scope Scope
	Mode  Mode
	Since *time.Time // incremental hint; adapters may ignore it
}

// Adapter maps an external source to canonical batches.
type Adapter interface {
	Manifest() Manifest
	Pull(ctx context.Context, req SyncRequest) (Batch, error)
}
