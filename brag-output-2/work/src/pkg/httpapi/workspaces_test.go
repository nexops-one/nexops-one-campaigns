// SPDX-License-Identifier: Apache-2.0

package httpapi_test

import (
	"context"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
)

type limitOne struct{ extension.AllowOpen }

func (limitOne) WorkspaceLimit(context.Context) int { return 1 }

func TestWorkspaceLimitAndSuspensionCodes(t *testing.T) {
	st := memory.New()
	var eng *compliance.Engine
	api := newAPI(t, setup{engine: func(c *compliance.Config) {
		c.Store, c.Entitlements = st, limitOne{}
	}})
	if r := call(t, api, "PUT", "/api/v1/adapters/csv-import/manifest", tokenA, sampleManifest(t)); r.Status != 200 {
		t.Fatalf("first workspace = %d %v", r.Status, r.Body)
	}
	if r := call(t, api, "PUT", "/api/v1/adapters/csv-import/manifest", tokenB, sampleManifest(t)); r.Status != 403 || errorCode(r) != "workspace_limit_reached" {
		t.Fatalf("over the limit = %d %v", r.Status, r.Body)
	}
	var err error
	if eng, err = compliance.New(ctx, compliance.Config{Store: st}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.SuspendWorkspace(ctx, scopeA, "audit"); err != nil {
		t.Fatal(err)
	}
	if r := call(t, api, "PUT", "/api/v1/adapters/csv-import/manifest", tokenA, sampleManifest(t)); r.Status != 403 || errorCode(r) != "workspace_suspended" {
		t.Fatalf("suspended = %d %v", r.Status, r.Body)
	}
	if r := call(t, api, "GET", "/api/v1/about", tokenA, nil); r.Status != 200 {
		t.Fatalf("reads stay allowed = %d", r.Status)
	}
}
