// SPDX-License-Identifier: Apache-2.0

package extension_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var scope = adapter.Scope{TenantID: "t", WorkspaceID: "w"}

func TestAllowOpen(t *testing.T) {
	ents := extension.AllowOpen{}
	for _, f := range []extension.Feature{extension.FeatureRegisterIngest, extension.FeatureRegisterImport, extension.FeatureEvaluationRun, extension.FeatureCatalogBasic, extension.FeatureEvidenceManage, extension.FeatureWorkflowBasic} {
		if d := ents.Allowed(context.Background(), scope, f); !d.Allowed || !extension.IsOpen(f) {
			t.Errorf("%s should be open and allowed: %+v", f, d)
		}
	}
	for _, f := range []extension.Feature{extension.FeatureCatalogMaintained, extension.FeatureReportProfileA, extension.FeatureAdapterManaged, extension.FeatureAuthSSO, extension.FeatureWorkflowAdvanced, extension.FeatureTenancyMulti} {
		if d := ents.Allowed(context.Background(), scope, f); d.Allowed || d.Reason != extension.ReasonNotLicensed || extension.IsOpen(f) {
			t.Errorf("%s should be commercial and denied as not_licensed: %+v", f, d)
		}
	}
}

type fixed struct{ d extension.Decision }

func (f fixed) Allowed(context.Context, adapter.Scope, extension.Feature) extension.Decision {
	return f.d
}

func TestRequire(t *testing.T) {
	grace := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	d, err := extension.Require(context.Background(), fixed{extension.Decision{Allowed: true, Reason: extension.ReasonExpired, GraceUntil: &grace}}, scope, extension.FeatureReportProfileA)
	if err != nil || !d.Allowed || d.GraceUntil == nil {
		t.Fatalf("grace period must allow and expose GraceUntil: %+v, %v", d, err)
	}
	_, err = extension.Require(context.Background(), fixed{extension.Decision{Reason: extension.ReasonAddonDisabled}}, scope, extension.FeatureRegisterIngest)
	var ne *extension.NotEntitledError
	if !errors.As(err, &ne) || ne.Feature != extension.FeatureRegisterIngest || ne.Reason != extension.ReasonAddonDisabled {
		t.Fatalf("err = %v", err)
	}
	if ne.Error() != "feature register.ingest is not entitled: addon_disabled" {
		t.Fatalf("message = %q", ne.Error())
	}
}

func TestPrincipalContext(t *testing.T) {
	if _, ok := extension.PrincipalFrom(context.Background()); ok {
		t.Fatal("an empty context carries no principal")
	}
	p := extension.Principal{Scope: scope, Actor: "user:u1", Kind: extension.ActorUser, Roles: []access.Role{access.RoleOwner}}
	got, ok := extension.PrincipalFrom(extension.WithPrincipal(context.Background(), p))
	if !ok || got.Actor != "user:u1" || got.Kind != extension.ActorUser || len(got.Roles) != 1 {
		t.Fatalf("got %+v, %v", got, ok)
	}
}
