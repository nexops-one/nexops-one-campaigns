// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"net/http"

	"github.com/nexops-one/compliance-engine/pkg/extension"
)

type chain []extension.Authenticator

// Chain tries each authenticator in order and returns the first principal
// resolved; when none succeeds it returns ErrUnauthenticated.
func Chain(as ...extension.Authenticator) extension.Authenticator {
	out := chain{}
	for _, a := range as {
		if a != nil {
			out = append(out, a)
		}
	}
	return out
}

func (c chain) Authenticate(r *http.Request) (extension.Principal, error) {
	for _, a := range c {
		if p, err := a.Authenticate(r); err == nil {
			return p, nil
		}
	}
	return extension.Principal{}, ErrUnauthenticated
}
