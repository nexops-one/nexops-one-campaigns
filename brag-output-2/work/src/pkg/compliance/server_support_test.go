// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestRegisterManifestWrapsValidationErrors(t *testing.T) {
	err := newEngine(t).RegisterManifest(ctx, scopeA, adapter.Manifest{Name: "x"})
	if !errors.Is(err, compliance.ErrInvalidManifest) {
		t.Fatalf("err = %v", err)
	}
}

type graceEnts struct{ until time.Time }

func (g graceEnts) Allowed(context.Context, adapter.Scope, extension.Feature) extension.Decision {
	return extension.Decision{Allowed: true, Reason: extension.ReasonExpired, GraceUntil: &g.until}
}

func TestEntitledExposesDecision(t *testing.T) {
	until := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	eng := newEngine(t, func(c *compliance.Config) { c.Entitlements = graceEnts{until: until} })
	d := eng.Entitled(ctx, scopeA, extension.FeatureRegisterIngest)
	if !d.Allowed || d.GraceUntil == nil || !d.GraceUntil.Equal(until) {
		t.Fatalf("decision = %+v", d)
	}
}

func TestSchemaAccess(t *testing.T) {
	eng := newEngine(t)
	if !reflect.DeepEqual(eng.SchemaVersions(), []string{"0.1.0"}) {
		t.Fatalf("versions = %v", eng.SchemaVersions())
	}
	if _, ok := eng.Schema().Entity("ict_provider"); !ok || eng.Schema().Version != "0.1.0" {
		t.Fatal("Schema must return the latest canonical schema")
	}
}
