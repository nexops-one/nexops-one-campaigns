// SPDX-License-Identifier: Apache-2.0

// Package auth provides the open-core authenticator: static bearer tokens,
// each bound to one tenant workspace and configured only by its SHA-256 hash.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// ErrUnauthenticated is returned for a missing, malformed or unknown token.
var ErrUnauthenticated = errors.New("missing or invalid bearer token")

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// HashToken returns the lowercase hex SHA-256 of a token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GenerateToken returns a new random token.
func GenerateToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "ce_" + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// DefaultStaticRoles are the roles of a static token configured without roles:
// enough to ingest, import, evaluate and administer the workspace, as in the
// first releases.
var DefaultStaticRoles = []access.Role{access.RoleOwner, access.RoleAdmin}

// TokenEntry binds a token hash to one workspace and its roles.
type TokenEntry struct {
	Hash  string
	Scope adapter.Scope
	Roles []access.Role // empty: DefaultStaticRoles
}

// ParseTokens parses "<sha256-hex>:<tenant>:<workspace>[:<role>+<role>]" entries
// separated by commas. Without roles an entry gets DefaultStaticRoles.
func ParseTokens(spec string) ([]TokenEntry, error) {
	var out []TokenEntry
	seen := map[string]bool{}
	for i, raw := range strings.Split(spec, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.Split(raw, ":")
		if len(parts) != 3 && len(parts) != 4 {
			return nil, fmt.Errorf("token entry %d: want <sha256-hex>:<tenant>:<workspace>[:<role>+<role>]", i+1)
		}
		e := TokenEntry{Hash: parts[0], Scope: adapter.Scope{TenantID: parts[1], WorkspaceID: parts[2]}, Roles: access.Normalize(DefaultStaticRoles)}
		if len(parts) == 4 {
			roles, err := access.ParseRoles(parts[3], "+")
			if err != nil {
				return nil, fmt.Errorf("token entry %d: %w", i+1, err)
			}
			e.Roles = roles
		}
		if !hashPattern.MatchString(e.Hash) {
			return nil, fmt.Errorf("token entry %d: hash must be 64 lowercase hex characters (use 'compliance-engine token hash')", i+1)
		}
		if err := e.Scope.Validate(); err != nil {
			return nil, fmt.Errorf("token entry %d: %w", i+1, err)
		}
		if seen[e.Hash] {
			return nil, fmt.Errorf("token entry %d: the same token is configured twice", i+1)
		}
		seen[e.Hash] = true
		out = append(out, e)
	}
	return out, nil
}

// StaticTokens authenticates "Authorization: Bearer <token>" requests.
type StaticTokens struct{ entries []TokenEntry }

// NewStaticTokens returns an authenticator over entries.
func NewStaticTokens(entries []TokenEntry) *StaticTokens {
	return &StaticTokens{entries: append([]TokenEntry(nil), entries...)}
}

var _ extension.Authenticator = (*StaticTokens)(nil)

// Authenticate resolves the request's bearer token to its workspace. Every
// configured hash is compared in constant time.
func (s *StaticTokens) Authenticate(r *http.Request) (extension.Principal, error) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		return extension.Principal{}, ErrUnauthenticated
	}
	sum := sha256.Sum256([]byte(token))
	var match *TokenEntry
	for i := range s.entries {
		want, _ := hex.DecodeString(s.entries[i].Hash)
		if subtle.ConstantTimeCompare(sum[:], want) == 1 && match == nil {
			match = &s.entries[i]
		}
	}
	if match == nil {
		return extension.Principal{}, ErrUnauthenticated
	}
	roles := match.Roles
	if len(roles) == 0 {
		roles = DefaultStaticRoles
	}
	return extension.Principal{Scope: match.Scope, Actor: "token:" + match.Hash[:12], Kind: extension.ActorToken, Roles: access.Normalize(roles)}, nil
}
