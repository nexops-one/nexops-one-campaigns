// SPDX-License-Identifier: Apache-2.0

package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

type section struct{ key string }

func (s section) AboutKey() string { return s.key }
func (s section) About(_ context.Context, sc adapter.Scope) any {
	return map[string]string{"workspace": sc.WorkspaceID}
}

func TestExtraRoutesAndAboutSections(t *testing.T) {
	api := newAPI(t, setup{options: func(o *httpapi.Options) {
		o.Edition = "test-edition"
		o.About = []extension.AboutSection{section{"license"}, section{"version"}}
		o.ExtraRoutes = []httpapi.ExtraRoute{
			{Route: httpapi.Route{Method: "POST", Pattern: "/api/v1/echo", Permission: access.PermDataRead},
				Handle: func(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
					var in struct {
						Say string `json:"say"`
					}
					if err := httpapi.DecodeJSON(r, &in); err != nil {
						return err
					}
					if in.Say == "" {
						return httpapi.NewProblem(http.StatusUnprocessableEntity, "nothing_to_say", "say something")
					}
					httpapi.WriteJSON(w, http.StatusOK, map[string]string{"said": in.Say, "actor": p.Actor})
					return nil
				}},
			{Route: httpapi.Route{Method: "POST", Pattern: "/api/v1/approver-only", Permission: access.PermWorkflowApprove},
				Handle: func(http.ResponseWriter, *http.Request, extension.Principal) error { return nil }},
		}
	}})
	about := call(t, api, "GET", "/api/v1/about", tokenA, nil)
	lic, _ := about.Body["license"].(map[string]any)
	if about.Body["edition"] != "test-edition" || lic["workspace"] != scopeA.WorkspaceID || about.Body["version"] != "test" {
		t.Fatalf("about = %v", about.Body)
	}
	if r := call(t, api, "POST", "/api/v1/echo", tokenA, map[string]string{"say": "hi"}); r.Status != 200 || r.Body["said"] != "hi" {
		t.Fatalf("echo = %d %v", r.Status, r.Body)
	}
	if r := call(t, api, "POST", "/api/v1/echo", tokenA, map[string]string{}); r.Status != 422 || errorCode(r) != "nothing_to_say" {
		t.Fatalf("problem = %d %v", r.Status, r.Body)
	}
	if r := call(t, api, "POST", "/api/v1/echo", tokenA, map[string]any{"bogus": 1}); r.Status != 400 {
		t.Fatalf("unknown field = %d", r.Status)
	}
	if r := call(t, api, "POST", "/api/v1/echo", "", map[string]string{"say": "hi"}); r.Status != 401 {
		t.Fatalf("unauthenticated = %d", r.Status)
	}
	if r := call(t, api, "POST", "/api/v1/approver-only", tokenA, nil); r.Status != 403 {
		t.Fatalf("permission = %d", r.Status)
	}
}

func TestExtraRouteMayNotShadow(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an extra route shadowing a built-in one must be refused")
		}
	}()
	newAPI(t, setup{options: func(o *httpapi.Options) {
		o.ExtraRoutes = []httpapi.ExtraRoute{{Route: httpapi.Route{Method: "GET", Pattern: "/api/v1/about", Permission: access.PermDataRead},
			Handle: func(http.ResponseWriter, *http.Request, extension.Principal) error { return nil }}}
	}})
}
