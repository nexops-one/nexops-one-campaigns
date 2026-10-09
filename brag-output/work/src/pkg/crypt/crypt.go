// SPDX-License-Identifier: Apache-2.0

// Package crypt provides envelope encryption for data at rest: an operator
// key-encryption key (KEK) wraps per-tenant data keys, which seal individual
// values with AES-256-GCM. Each sealed value is bound to its place (tenant,
// workspace, kind, field) by additional authenticated data, so a value copied
// elsewhere does not open.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

var (
	// ErrOpen means a sealed value could not be decrypted: wrong key, wrong
	// place, tampering, or deleted keys.
	ErrOpen = errors.New("cannot decrypt a sealed value")
	// ErrKEKMismatch means the tenant keys were wrapped by another KEK.
	ErrKEKMismatch = errors.New("tenant keys are wrapped by a different key-encryption key")
)

const sealPrefix = "v1:"

// KEK is the operator-held key-encryption key.
type KEK struct {
	ID  string
	key [32]byte
}

// NewKEK builds a KEK from 32 raw bytes.
func NewKEK(raw []byte) (KEK, error) {
	if len(raw) != 32 {
		return KEK{}, fmt.Errorf("key-encryption key must be 32 bytes, got %d", len(raw))
	}
	var k KEK
	copy(k.key[:], raw)
	sum := sha256.Sum256(raw)
	k.ID = hex.EncodeToString(sum[:8])
	return k, nil
}

// GenerateKEK returns a new random KEK, base64 encoded as stored in key files.
func GenerateKEK() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b[:]), nil
}

// LoadKEK reads a base64 key file. On POSIX systems a file that others can
// access is refused, and so is a file its group can access, unless that
// group is the engine process's own and has read access only: Kubernetes
// mounts Secrets owned by root, readable through the pod's fsGroup.
func LoadKEK(path string) (KEK, error) {
	info, err := os.Stat(path)
	if err != nil {
		return KEK{}, fmt.Errorf("key-encryption key file: %w", err)
	}
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && (perm&0o007 != 0 || perm&0o070 != 0 && (perm&0o030 != 0 || !ownGroup(info))) {
		return KEK{}, fmt.Errorf("key-encryption key file %s is accessible by others, or by a group other than the engine's own (mode %v); "+
			"chmod 600 it, or 440/640 with the engine's group (Kubernetes fsGroup)", path, perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return KEK{}, fmt.Errorf("key-encryption key file: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return KEK{}, fmt.Errorf("key-encryption key file %s is not base64: %w", path, err)
	}
	return NewKEK(raw)
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(key []byte, plain []byte, aad string) ([]byte, error) {
	aead, err := gcm(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, plain, []byte(aad))...), nil
}

func open(key []byte, data []byte, aad string) ([]byte, error) {
	aead, err := gcm(key)
	if err != nil {
		return nil, err
	}
	if len(data) < aead.NonceSize() {
		return nil, ErrOpen
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte(aad))
	if err != nil {
		return nil, ErrOpen
	}
	return plain, nil
}

// Wrap encrypts a data key under the KEK.
func (k KEK) Wrap(plain []byte) ([]byte, error) { return seal(k.key[:], plain, "kek:"+k.ID) }

// Unwrap decrypts a data key wrapped by this KEK.
func (k KEK) Unwrap(w []byte) ([]byte, error) { return open(k.key[:], w, "kek:"+k.ID) }

// DataKey is one version of a tenant's keys: Enc seals values, Mac hashes content.
type DataKey struct {
	Version int
	Enc     [32]byte
	Mac     [32]byte
}

// NewDataKey returns random keys for version v.
func NewDataKey(v int) (DataKey, error) {
	d := DataKey{Version: v}
	if _, err := rand.Read(d.Enc[:]); err != nil {
		return d, err
	}
	_, err := rand.Read(d.Mac[:])
	return d, err
}

// Seal encrypts plain for the place named by aad: "v1:<version>:<base64 nonce+ciphertext>".
func (d DataKey) Seal(plain []byte, aad string) (string, error) {
	ct, err := seal(d.Enc[:], plain, aad)
	if err != nil {
		return "", err
	}
	return sealPrefix + strconv.Itoa(d.Version) + ":" + base64.RawStdEncoding.EncodeToString(ct), nil
}

// IsSealed reports whether s looks like a sealed value.
func IsSealed(s string) bool {
	if !strings.HasPrefix(s, sealPrefix) {
		return false
	}
	ver, rest, ok := strings.Cut(s[len(sealPrefix):], ":")
	if _, err := strconv.Atoi(ver); err != nil || !ok || rest == "" {
		return false
	}
	return true
}

// Open decrypts a sealed value, looking its key version up with keys.
func Open(keys func(version int) (DataKey, error), sealed, aad string) ([]byte, error) {
	if !IsSealed(sealed) {
		return nil, ErrOpen
	}
	ver, rest, _ := strings.Cut(sealed[len(sealPrefix):], ":")
	v, _ := strconv.Atoi(ver)
	d, err := keys(v)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOpen, err)
	}
	ct, err := base64.RawStdEncoding.DecodeString(rest)
	if err != nil {
		return nil, ErrOpen
	}
	return open(d.Enc[:], ct, aad)
}

// HMAC returns "hmac-sha256:<hex>" of data under the tenant's hashing key.
func (d DataKey) HMAC(data []byte) string {
	m := hmac.New(sha256.New, d.Mac[:])
	m.Write(data)
	return "hmac-sha256:" + hex.EncodeToString(m.Sum(nil))
}
