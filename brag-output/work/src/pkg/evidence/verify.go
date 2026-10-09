// SPDX-License-Identifier: Apache-2.0

package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ErrCannotVerifyHere means the engine may not read the object; the customer
// verifies it with 'compliance-engine evidence verify' and submits the result.
var ErrCannotVerifyHere = errors.New("the engine cannot read this evidence object; verify it locally with 'compliance-engine evidence verify'")

// DefaultMaxBytes bounds the size of an object the engine hashes.
const DefaultMaxBytes = 1 << 30

// Verifier computes checksums of objects the engine is allowed to read: files
// under Root and HTTPS objects on hosts in AllowHosts. The zero value can read
// nothing, so a default deployment makes no outbound calls.
type Verifier struct {
	Root       string
	AllowHosts []string
	Client     *http.Client
	MaxBytes   int64
}

// Checksum returns "sha256:<hex>" of the object at uri.
func (v Verifier) Checksum(ctx context.Context, uri string) (string, error) {
	if err := ValidateURI(uri); err != nil {
		return "", err
	}
	u, _ := url.Parse(uri)
	switch u.Scheme {
	case "file":
		return v.file(u.Path)
	case "https":
		return v.https(ctx, u)
	}
	return "", ErrCannotVerifyHere
}

func (v Verifier) limit() int64 {
	if v.MaxBytes > 0 {
		return v.MaxBytes
	}
	return DefaultMaxBytes
}

func hashReader(r io.Reader, max int64) (string, error) {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, max+1))
	if err != nil {
		return "", err
	}
	if n > max {
		return "", fmt.Errorf("evidence object exceeds %d bytes", max)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// file hashes a file only when its real path (symlinks resolved) is inside Root.
func (v Verifier) file(p string) (string, error) {
	if v.Root == "" {
		return "", ErrCannotVerifyHere
	}
	root, err := filepath.EvalSymlinks(v.Root)
	if err != nil {
		return "", fmt.Errorf("evidence root: %w", err)
	}
	if root, err = filepath.Abs(root); err != nil {
		return "", err
	}
	// file:///C:/x on Windows parses to /C:/x.
	if len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	real, err := filepath.EvalSymlinks(filepath.FromSlash(p))
	if err != nil {
		return "", fmt.Errorf("%w (%v)", ErrCannotVerifyHere, err)
	}
	if real, err = filepath.Abs(real); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", ErrCannotVerifyHere
	}
	f, err := os.Open(real)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return hashReader(f, v.limit())
}

func (v Verifier) allowed(host string) bool {
	for _, h := range v.AllowHosts {
		if strings.EqualFold(strings.TrimSpace(h), host) {
			return true
		}
	}
	return false
}

func (v Verifier) https(ctx context.Context, u *url.URL) (string, error) {
	if !v.allowed(u.Host) {
		return "", ErrCannotVerifyHere
	}
	client := v.Client
	if client == nil {
		client = http.DefaultClient
	}
	c := *client
	c.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if !v.allowed(req.URL.Host) || req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to %s is not allowed", req.URL.Host)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	res, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching evidence: HTTP %d", res.StatusCode)
	}
	return hashReader(res.Body, v.limit())
}
