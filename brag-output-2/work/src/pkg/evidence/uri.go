// SPDX-License-Identifier: Apache-2.0

// Package evidence validates evidence references and verifies checksums of
// objects the engine is allowed to read. Evidence content stays in the
// customer's environment; the engine keeps references and checksums.
package evidence

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ErrInvalid reports an unacceptable URI or checksum.
var ErrInvalid = errors.New("invalid evidence reference")

// Schemes accepted in evidence URIs.
var Schemes = []string{"https", "s3", "file", "urn"}

// Kinds of evidence.
var Kinds = []string{"document", "attestation", "report", "screenshot", "log_extract", "other"}

var credentialParam = regexp.MustCompile(`(?i)(token|key|secret|sig|password|credential)|^x-amz-`)

// ValidateURI accepts a reference to an object in the customer's environment
// and refuses anything carrying credentials (user information, signed-URL or
// token query parameters), which would otherwise be stored and exported.
func ValidateURI(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || raw == "" {
		return fmt.Errorf("%w: uri %q is not a URI", ErrInvalid, raw)
	}
	ok := false
	for _, s := range Schemes {
		ok = ok || u.Scheme == s
	}
	if !ok {
		return fmt.Errorf("%w: uri scheme %q is not one of %s", ErrInvalid, u.Scheme, strings.Join(Schemes, ", "))
	}
	if u.User != nil {
		return fmt.Errorf("%w: uri must not contain user information or passwords", ErrInvalid)
	}
	for name := range u.Query() {
		if credentialParam.MatchString(name) {
			return fmt.Errorf("%w: uri query parameter %q looks like a credential; reference the object without signed or tokenized parameters", ErrInvalid, name)
		}
	}
	if (u.Scheme == "https" || u.Scheme == "s3") && u.Host == "" {
		return fmt.Errorf("%w: %s uri needs a host or bucket", ErrInvalid, u.Scheme)
	}
	if u.Scheme == "file" && u.Path == "" {
		return fmt.Errorf("%w: file uri needs a path", ErrInvalid)
	}
	if u.Scheme == "urn" && u.Opaque == "" {
		return fmt.Errorf("%w: urn needs a namespace and name", ErrInvalid)
	}
	return nil
}

var checksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ValidateChecksum accepts "sha256:<64 lowercase hex>".
func ValidateChecksum(s string) error {
	if !checksumPattern.MatchString(s) {
		return fmt.Errorf("%w: checksum must be sha256:<64 lowercase hex characters>", ErrInvalid)
	}
	return nil
}

// ValidateKind accepts a known evidence kind.
func ValidateKind(k string) error {
	for _, x := range Kinds {
		if x == k {
			return nil
		}
	}
	return fmt.Errorf("%w: kind %q is not one of %s", ErrInvalid, k, strings.Join(Kinds, ", "))
}
