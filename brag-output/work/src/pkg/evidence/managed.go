// SPDX-License-Identifier: Apache-2.0

package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/crypt"
)

// ManagedScheme prefixes the URI of evidence whose object the engine stores.
const ManagedScheme = "managed:"

var (
	// ErrManagedNotConfigured means no managed storage directory or KEK is configured.
	ErrManagedNotConfigured = errors.New("managed evidence storage is not configured (COMPLIANCE_EVIDENCE_STORAGE_DIR and COMPLIANCE_ENCRYPTION_KEY_FILE)")
	// ErrNotManaged means the evidence object is not stored by the engine.
	ErrNotManaged = errors.New("the evidence object is not stored by the engine")
)

var hexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Managed stores uploaded evidence objects under Dir/<tenant>/<sha256>.bin,
// each encrypted with the tenant's data key. Objects are content-addressed,
// so the same file uploaded twice is stored once.
type Managed struct {
	Dir      string
	Ring     *crypt.Keyring
	MaxBytes int64
}

func (m *Managed) configured() bool { return m != nil && m.Dir != "" && m.Ring != nil }

func (m *Managed) path(tenant, sum string) (string, error) {
	if !hexPattern.MatchString(sum) || tenant == "" || strings.ContainsAny(tenant, `/\.`) {
		return "", fmt.Errorf("%w: invalid managed object reference", ErrInvalid)
	}
	return filepath.Join(m.Dir, tenant, sum+".bin"), nil
}

func aad(tenant, sum string) string { return tenant + "/managed/" + sum }

// Put stores r and returns its SHA-256 (hex) and size.
func (m *Managed) Put(ctx context.Context, tenant string, r io.Reader) (string, int64, error) {
	if !m.configured() {
		return "", 0, ErrManagedNotConfigured
	}
	max := m.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return "", 0, err
	}
	if int64(len(data)) > max {
		return "", 0, fmt.Errorf("%w: evidence object exceeds %d bytes", ErrInvalid, max)
	}
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	p, err := m.path(tenant, hexSum)
	if err != nil {
		return "", 0, err
	}
	if _, err := os.Stat(p); err == nil {
		return hexSum, int64(len(data)), nil // already stored
	}
	d, err := m.Ring.Active(ctx, tenant)
	if err != nil {
		return "", 0, err
	}
	sealed, err := d.Seal(data, aad(tenant, hexSum))
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", 0, err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(sealed), 0o600); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return "", 0, err
	}
	return hexSum, int64(len(data)), nil
}

// Open returns the decrypted object.
func (m *Managed) Open(ctx context.Context, tenant, sum string) ([]byte, error) {
	if !m.configured() {
		return nil, ErrManagedNotConfigured
	}
	p, err := m.path(tenant, sum)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("managed evidence object: %w", err)
	}
	return crypt.Open(m.Ring.Opener(ctx, tenant), string(data), aad(tenant, sum))
}

// Delete removes an object (missing objects are not an error).
func (m *Managed) Delete(tenant, sum string) error {
	if !m.configured() {
		return nil
	}
	p, err := m.path(tenant, sum)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// DeleteTenant removes every object of a tenant.
func (m *Managed) DeleteTenant(tenant string) error {
	if !m.configured() {
		return nil
	}
	if _, err := m.path(tenant, strings.Repeat("0", 64)); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(m.Dir, tenant))
}

// ManagedSum returns the SHA-256 hex of a managed URI.
func ManagedSum(uri string) (string, bool) {
	sum, ok := strings.CutPrefix(uri, ManagedScheme)
	return sum, ok && hexPattern.MatchString(sum)
}
