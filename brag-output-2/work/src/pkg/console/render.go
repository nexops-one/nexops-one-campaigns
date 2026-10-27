// SPDX-License-Identifier: Apache-2.0

package console

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

const (
	sessionCookie = "ce_session"
	// signInCookie carries the double-submit CSRF token of the sign-in form,
	// which has no session yet.
	signInCookie = "ce_signin"
)

// csrfToken derives a session's form token; nothing extra is stored.
func csrfToken(sessionValue string) string {
	sum := sha256.Sum256([]byte("csrf:" + sessionValue))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: Prefix, MaxAge: int(maxAge.Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: Prefix, MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
}

// Disclaimer is the console's non-certification statement.
const Disclaimer = "Not legal advice, not certification. The console shows what the compliance engine evaluated from the data supplied, at the time shown. Wording pending legal review."

// view is what every page template receives.
type view struct {
	Title      string
	Section    string
	SignedIn   bool
	Email      string
	Tenant     string
	Workspace  string
	Workspaces []store.Member
	Roles      []access.Role
	Sample     bool
	Banner     string
	CSRF       string
	Problem    *httpapi.Problem
	OK         string
	Version    string
	Disclaimer string
	Data       any
	// Refresh, when set, continues to this console page (after an SSO sign-in).
	Refresh string

	entitled func(extension.Feature) bool
}

// Can reports whether the viewer's roles grant a permission.
func (v view) Can(p string) bool { return access.Allows(v.Roles, access.Permission(p)) }

// Entitled reports whether the workspace may use a feature.
func (v view) Entitled(f string) bool { return v.entitled != nil && v.entitled(extension.Feature(f)) }

// okMessages are the confirmations a redirect may name with ?ok=.
var okMessages = map[string]string{
	"signed_out":  "You are signed out.",
	"imported":    "The import was committed.",
	"rolled_back": "The import was rolled back.",
	"assigned":    "The assessment was updated.",
	"acted":       "The workflow action was recorded.",
	"evidence":    "The evidence was saved.",
	"verified":    "The evidence was checked.",
	"revoked":     "The evidence was revoked.",
	"generated":   "The report was generated.",
	"member":      "The member was saved.",
	"removed":     "The member was removed.",
	"token":       "The token was revoked.",
	"settings":    "The settings were saved.",
	"workspace":   "You switched workspace.",
}

var funcs = template.FuncMap{
	"date": func(t any) string {
		switch v := t.(type) {
		case time.Time:
			if v.IsZero() {
				return ""
			}
			return v.UTC().Format("2006-01-02 15:04 UTC")
		case *time.Time:
			if v == nil {
				return ""
			}
			return v.UTC().Format("2006-01-02 15:04 UTC")
		}
		return fmt.Sprint(t)
	},
	"day": func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.UTC().Format("2006-01-02")
	},
	"pct": func(v float64) string { return fmt.Sprintf("%.0f%%", v) },
	"join": func(sep string, xs any) string {
		switch v := xs.(type) {
		case []string:
			return strings.Join(v, sep)
		case []access.Role:
			return access.Join(v, sep)
		}
		return fmt.Sprint(xs)
	},
	"status": func(s any) string { return strings.ReplaceAll(fmt.Sprint(s), "_", " ") },
	"slug":   func(s any) string { return strings.ReplaceAll(fmt.Sprint(s), "_", "-") },
	"add":    func(a, b int) int { return a + b },
	"get": func(m map[string]any, key string) string {
		if v, ok := m[key]; ok && v != nil {
			return fmt.Sprint(v)
		}
		return ""
	},
	"seq": func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	},
	"has": func(xs []access.Role, r any) bool { return access.Contains(xs, access.Role(fmt.Sprint(r))) },
}

// parsePages parses every page template together with the layout.
func parsePages() map[string]*template.Template {
	layout := template.Must(template.New("layout.html").Funcs(funcs).ParseFS(assets, "templates/layout.html"))
	names, _ := fs.Glob(assets, "templates/*.html")
	pages := map[string]*template.Template{}
	for _, n := range names {
		name := strings.TrimSuffix(path.Base(n), ".html")
		if name == "layout" {
			continue
		}
		pages[name] = template.Must(template.Must(layout.Clone()).ParseFS(assets, n))
	}
	return pages
}

// render writes a page. A problem carried by rc (a failed action being
// re-rendered) is shown and sets the status.
func (c *Console) render(w http.ResponseWriter, r *http.Request, rc *reqCtx, name, title string, status int, data any) {
	v := view{Title: title, Section: name, Version: c.opts.Version, Disclaimer: Disclaimer, Data: data,
		Tenant: c.opts.Tenant, OK: okMessages[r.URL.Query().Get("ok")]}
	if p, ok := data.(*httpapi.Problem); ok {
		v.Problem, v.Data = p, nil
	}
	if name == "continue" {
		v.Refresh, _ = data.(string)
	}
	if rc != nil && rc.problem != nil {
		v.Problem = rc.problem
		status = rc.problem.Status
		v.OK = ""
	}
	if rc != nil && rc.value != "" {
		v.SignedIn, v.Email, v.Workspace, v.Workspaces, v.Roles = true, rc.email, rc.p.Scope.WorkspaceID, rc.workspaces, rc.p.Roles
		v.Tenant, v.Sample, v.CSRF = rc.p.Scope.TenantID, rc.sample, csrfToken(rc.value)
		scope, ctx := rc.p.Scope, r.Context()
		v.entitled = func(f extension.Feature) bool { return c.eng.Entitled(ctx, scope, f).Allowed }
	}
	if v.Sample {
		v.Banner = SampleBanner
	}
	if form, ok := data.(interface{ csrf() string }); ok && v.CSRF == "" {
		v.CSRF = form.csrf()
	}
	t := c.pages[name]
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", v); err != nil {
		c.opts.Logger.Error("console template failed", "page", name, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
