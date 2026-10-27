// SPDX-License-Identifier: Apache-2.0

package crypt_test

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/crypt"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var ctx = context.Background()

func kek(t *testing.T, b byte) crypt.KEK {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = b
	}
	k, err := crypt.NewKEK(raw)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealAndOpen(t *testing.T) {
	d, err := crypt.NewDataKey(1)
	if err != nil {
		t.Fatal(err)
	}
	keys := func(v int) (crypt.DataKey, error) {
		if v != 1 {
			return crypt.DataKey{}, errors.New("no such version")
		}
		return d, nil
	}
	s, err := d.Seal([]byte("notice period 90 days"), "acme/ws-1/arrangement_service_line/notice_period_entity")
	if err != nil || !crypt.IsSealed(s) || strings.Contains(s, "90 days") || !strings.HasPrefix(s, "v1:1:") {
		t.Fatalf("sealed = %q %v", s, err)
	}
	got, err := crypt.Open(keys, s, "acme/ws-1/arrangement_service_line/notice_period_entity")
	if err != nil || string(got) != "notice period 90 days" {
		t.Fatalf("open = %q %v", got, err)
	}
	if _, err := crypt.Open(keys, s, "acme/ws-2/arrangement_service_line/notice_period_entity"); !errors.Is(err, crypt.ErrOpen) {
		t.Fatalf("a value moved to another place must not open: %v", err)
	}
	tampered := s[:len(s)-2] + "AA"
	if _, err := crypt.Open(keys, tampered, "acme/ws-1/arrangement_service_line/notice_period_entity"); !errors.Is(err, crypt.ErrOpen) {
		t.Fatalf("tampered = %v", err)
	}
	other, _ := crypt.NewDataKey(1)
	if _, err := crypt.Open(func(int) (crypt.DataKey, error) { return other, nil }, s, "acme/ws-1/arrangement_service_line/notice_period_entity"); !errors.Is(err, crypt.ErrOpen) {
		t.Fatalf("wrong key = %v", err)
	}
	for _, not := range []string{"", "plain", "v1:", "v1:x:abc", "v2:1:abc"} {
		if crypt.IsSealed(not) {
			t.Errorf("%q is not sealed", not)
		}
	}
	if d.HMAC([]byte("x")) == other.HMAC([]byte("x")) || !strings.HasPrefix(d.HMAC([]byte("x")), "hmac-sha256:") || d.HMAC([]byte("x")) != d.HMAC([]byte("x")) {
		t.Fatal("HMAC must be deterministic per key and differ between keys")
	}
}

func TestKEKFiles(t *testing.T) {
	enc, err := crypt.GenerateKEK()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(enc)
	if len(raw) != 32 {
		t.Fatalf("generated key length %d", len(raw))
	}
	p := filepath.Join(t.TempDir(), "kek")
	if err := os.WriteFile(p, []byte(enc+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	k, err := crypt.LoadKEK(p)
	if err != nil || len(k.ID) != 16 {
		t.Fatalf("load = %+v %v", k.ID, err)
	}
	w, _ := k.Wrap([]byte("data key"))
	if got, err := k.Unwrap(w); err != nil || string(got) != "data key" {
		t.Fatalf("unwrap = %q %v", got, err)
	}
	if _, err := kek(t, 7).Unwrap(w); err == nil {
		t.Fatal("another KEK must not unwrap")
	}
	if runtime.GOOS != "windows" {
		// A file created by the process has the process's group: readable by
		// that group only is accepted (Kubernetes fsGroup); others or group
		// write never are.
		for mode, ok := range map[os.FileMode]bool{0o600: true, 0o400: true, 0o440: true, 0o640: true, 0o644: false, 0o604: false, 0o660: false, 0o620: false} {
			_ = os.Chmod(p, mode)
			_, err := crypt.LoadKEK(p)
			if ok != (err == nil) {
				t.Errorf("mode %v: err = %v", mode, err)
			}
			if !ok && err != nil && !strings.Contains(err.Error(), "chmod 600") {
				t.Errorf("mode %v: message %q", mode, err)
			}
		}
		_ = os.Chmod(p, 0o600)
	}
	bad := filepath.Join(t.TempDir(), "bad")
	_ = os.WriteFile(bad, []byte("c2hvcnQ="), 0o600)
	if _, err := crypt.LoadKEK(bad); err == nil {
		t.Fatal("a short key must be refused")
	}
}

func TestKeyring(t *testing.T) {
	st := memory.New()
	ring := crypt.NewKeyring(kek(t, 1), st, nil)
	d1, err := ring.Active(ctx, "acme")
	if err != nil || d1.Version != 1 {
		t.Fatalf("active = %+v %v", d1.Version, err)
	}
	again, _ := crypt.NewKeyring(kek(t, 1), st, nil).Active(ctx, "acme")
	if again.Enc != d1.Enc || again.Mac != d1.Mac {
		t.Fatal("a second keyring must unwrap the same keys")
	}
	other, _ := ring.Active(ctx, "globex")
	if other.Enc == d1.Enc {
		t.Fatal("tenants must have distinct keys")
	}
	sealed, _ := d1.Seal([]byte("secret"), "a")
	v2, err := ring.RotateData(ctx, "acme")
	if err != nil || v2 != 2 {
		t.Fatalf("rotate = %d %v", v2, err)
	}
	if d2, _ := ring.Active(ctx, "acme"); d2.Version != 2 || d2.Enc == d1.Enc {
		t.Fatal("rotation must activate a new key")
	}
	if got, err := crypt.Open(ring.Opener(ctx, "acme"), sealed, "a"); err != nil || string(got) != "secret" {
		t.Fatalf("old values stay readable after data key rotation: %q %v", got, err)
	}
	if _, err := crypt.NewKeyring(kek(t, 9), st, nil).Active(ctx, "acme"); !errors.Is(err, crypt.ErrKEKMismatch) {
		t.Fatalf("another KEK = %v", err)
	}

	// KEK rotation: everything still opens under the new KEK only.
	n, err := crypt.RewrapAll(ctx, st, kek(t, 1), kek(t, 2), nil)
	if err != nil || n != 2 {
		t.Fatalf("rewrap = %d %v", n, err)
	}
	fresh := crypt.NewKeyring(kek(t, 2), st, nil)
	if got, err := crypt.Open(fresh.Opener(ctx, "acme"), sealed, "a"); err != nil || string(got) != "secret" {
		t.Fatalf("after KEK rotation = %q %v", got, err)
	}
	if _, err := crypt.NewKeyring(kek(t, 1), st, nil).Active(ctx, "acme"); !errors.Is(err, crypt.ErrKEKMismatch) {
		t.Fatal("the old KEK must no longer unwrap")
	}
	if n, _ := crypt.RewrapAll(ctx, st, kek(t, 1), kek(t, 2), nil); n != 0 {
		t.Fatal("rewrapping twice changes nothing")
	}
	evs, _ := st.AuditEvents(ctx, adapter.Scope{TenantID: "acme"}, store.AuditQuery{})
	actions := []string{}
	for _, e := range evs {
		actions = append(actions, e.Action)
	}
	if strings.Join(actions, ",") != "keys.create,keys.rotate_data,keys.rotate_kek" {
		t.Fatalf("key events = %v", actions)
	}
}

func TestLoadSensitivity(t *testing.T) {
	s, err := crypt.LoadSensitivity("0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Has("arrangement_service_line", "notice_period_entity") || !s.Has("function", "rto") || s.Has("ict_provider", "legal_name") {
		t.Fatalf("sensitivity = %v", s)
	}
	if _, err := crypt.LoadSensitivity("9.9.9"); err == nil {
		t.Fatal("unknown schema version")
	}
}

func TestRotateDataRefusesAForeignKEK(t *testing.T) {
	st := memory.New()
	if _, err := crypt.NewKeyring(kek(t, 1), st, nil).Active(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := crypt.NewKeyring(kek(t, 2), st, nil).RotateData(ctx, "acme"); !errors.Is(err, crypt.ErrKEKMismatch) {
		t.Fatalf("rotating with another KEK = %v", err)
	}
	if k, _ := st.TenantKeys(ctx, "acme"); len(k.Versions) != 1 {
		t.Fatal("a refused rotation must not add a key")
	}
}
