// SPDX-License-Identifier: Apache-2.0

package console

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"

	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/identity"
)

// SignInProvider is an additional way to sign in to the console, for example
// single sign-on in the enterprise edition. The console owns the session
// cookie: a provider authenticates the user, opens a session with the
// identity service and returns it.
type SignInProvider interface {
	// ID names the provider in its paths: /console/signin/{id} starts a
	// sign-in and /console/signin/{id}/callback completes it.
	ID() string
	// Label is the text of the provider's button on the sign-in page.
	Label() string
	// Start begins a sign-in, typically by redirecting to an identity
	// provider. next is the console page to show afterwards ("" for the
	// default), already checked to stay inside the console.
	Start(w http.ResponseWriter, r *http.Request, next string) error
	// Finish completes a sign-in and returns the opened session and the page
	// to show. A refusal returns an error, shown on the sign-in page; an
	// identity.ErrSignInFailed is answered 401 without details.
	Finish(w http.ResponseWriter, r *http.Request) (in identity.SignedIn, next string, err error)
}

var providerID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// signInButton is a provider as the sign-in page shows it.
type signInButton struct {
	Path  string
	Label string
}

func (c *Console) buttons(next string) []signInButton {
	var out []signInButton
	for _, p := range c.opts.SignIn {
		path := Prefix + "/signin/" + p.ID()
		if next != "" {
			path += "?next=" + url.QueryEscape(next)
		}
		out = append(out, signInButton{Path: path, Label: p.Label()})
	}
	return out
}

func (c *Console) provider(r *http.Request) (SignInProvider, error) {
	id := r.PathValue("provider")
	for _, p := range c.opts.SignIn {
		if p.ID() == id {
			return p, nil
		}
	}
	return nil, problem(http.StatusNotFound, "not_found", "There is no such sign-in method.")
}

func (c *Console) providerStart(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	p, err := c.provider(r)
	if err != nil {
		return err
	}
	if err := p.Start(w, r, safeNext(r.URL.Query().Get("next"))); err != nil {
		return c.providerFailed(w, r, rc, p, err)
	}
	return nil
}

// providerFinish opens the console session. The identity provider's redirect
// is a cross-site navigation, during which browsers do not send the
// SameSite=Strict session cookie; the console therefore answers with a page
// that continues by a same-site navigation instead of a redirect.
func (c *Console) providerFinish(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	p, err := c.provider(r)
	if err != nil {
		return err
	}
	in, next, err := p.Finish(w, r)
	if err != nil {
		return c.providerFailed(w, r, rc, p, err)
	}
	setCookie(w, sessionCookie, in.Value, c.ids.SessionMaxAge())
	switch next = safeNext(next); {
	case next != "":
	case len(in.Workspaces) > 1:
		next = Prefix + "/workspace"
	default:
		next = Prefix + "/"
	}
	c.render(w, r, rc, "continue", "Signed in", http.StatusOK, next)
	return nil
}

func (c *Console) providerFailed(w http.ResponseWriter, r *http.Request, rc *reqCtx, p SignInProvider, err error) error {
	c.opts.Logger.Info("console sign-in failed", "provider", p.ID(), "reason", err.Error())
	pr, ok := httpapi.Describe(err)
	switch {
	case errors.Is(err, identity.ErrSignInFailed):
		pr = httpapi.Problem{Status: http.StatusUnauthorized, Code: "unauthorized", Message: identity.ErrSignInFailed.Error()}
	case !ok:
		pr = httpapi.Problem{Status: http.StatusUnauthorized, Code: "unauthorized", Message: err.Error()}
	}
	c.opts.Failures.Fail(r)
	rc.problem = &pr
	return c.renderSignIn(w, r, rc, pr.Status, signInForm{})
}
