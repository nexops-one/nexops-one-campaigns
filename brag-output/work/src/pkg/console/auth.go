// SPDX-License-Identifier: Apache-2.0

package console

import (
	"errors"
	"net/http"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/identity"
)

type signInForm struct {
	Tenant      string
	AskTenant   bool
	Email       string
	Next        string
	token       string
	TooMany     bool
	RetrySecond string
	Providers   []signInButton
}

func (f signInForm) csrf() string { return f.token }

// safeNext keeps a post-sign-in redirect inside the console.
func safeNext(next string) string {
	if strings.HasPrefix(next, Prefix+"/") && !strings.HasPrefix(next, "//") && !strings.Contains(next, "\\") && !strings.Contains(next, "://") {
		return next
	}
	return ""
}

func (c *Console) signInPage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	if ck, err := r.Cookie(sessionCookie); err == nil {
		if _, _, err := c.ids.SessionPrincipal(r.Context(), ck.Value); err == nil {
			http.Redirect(w, r, Prefix+"/", http.StatusSeeOther)
			return nil
		}
	}
	return c.renderSignIn(w, r, rc, http.StatusOK, signInForm{Next: safeNext(r.URL.Query().Get("next"))})
}

func (c *Console) renderSignIn(w http.ResponseWriter, r *http.Request, rc *reqCtx, status int, f signInForm) error {
	f.token = randomToken()
	f.Providers = c.buttons(f.Next)
	f.AskTenant = c.opts.Tenant == ""
	if !f.AskTenant {
		f.Tenant = c.opts.Tenant
	}
	setCookie(w, signInCookie, f.token, 0)
	c.render(w, r, rc, "signin", "Sign in", status, f)
	return nil
}

func (c *Console) signIn(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	if err := r.ParseForm(); err != nil {
		return err
	}
	f := signInForm{Tenant: strings.TrimSpace(r.PostFormValue("tenant")), Email: strings.TrimSpace(r.PostFormValue("email")),
		Next: safeNext(r.PostFormValue("next"))}
	ck, err := r.Cookie(signInCookie)
	if err != nil || !validCSRF(r.PostFormValue("csrf"), ck.Value) {
		rc.problem = &httpapi.Problem{Status: http.StatusForbidden, Code: "invalid_csrf",
			Message: "The sign-in form has expired. Please sign in again."}
		return c.renderSignIn(w, r, rc, http.StatusForbidden, f)
	}
	if wait, blocked := c.opts.Failures.Blocked(r); blocked {
		w.Header().Set("Retry-After", httpapi.RetryAfter(wait))
		rc.problem = &httpapi.Problem{Status: http.StatusTooManyRequests, Code: "rate_limited",
			Message: "Too many failed sign-ins from your address. Wait a minute and try again."}
		return c.renderSignIn(w, r, rc, http.StatusTooManyRequests, f)
	}
	tenant := c.opts.Tenant
	if tenant == "" {
		tenant = f.Tenant
	}
	in, err := c.ids.SignIn(r.Context(), tenant, f.Email, r.PostFormValue("password"))
	if err != nil {
		if !errors.Is(err, identity.ErrSignInFailed) && !errors.Is(err, identity.ErrNoWorkspace) {
			return err
		}
		c.opts.Failures.Fail(r)
		c.opts.Logger.Info("console sign-in failed", "tenant", tenant, "email", f.Email, "reason", err.Error())
		rc.problem = &httpapi.Problem{Status: http.StatusUnauthorized, Code: "unauthorized", Message: err.Error()}
		return c.renderSignIn(w, r, rc, http.StatusUnauthorized, f)
	}
	clearCookie(w, signInCookie)
	setCookie(w, sessionCookie, in.Value, c.ids.SessionMaxAge())
	next := f.Next
	switch {
	case next != "":
	case len(in.Workspaces) > 1:
		next = Prefix + "/workspace"
	default:
		next = Prefix + "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
	return nil
}

func (c *Console) signOut(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	if err := c.ids.SignOut(r.Context(), rc.value); err != nil {
		return err
	}
	clearCookie(w, sessionCookie)
	http.Redirect(w, r, Prefix+"/signin?ok=signed_out", http.StatusSeeOther)
	return nil
}

func (c *Console) workspacePage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	c.render(w, r, rc, "workspace", "Workspace", http.StatusOK, nil)
	return nil
}

func (c *Console) switchWorkspace(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	if err := c.ids.SwitchWorkspace(r.Context(), rc.value, r.PostFormValue("workspace")); err != nil {
		return err
	}
	http.Redirect(w, r, Prefix+"/?ok=workspace", http.StatusSeeOther)
	return nil
}
