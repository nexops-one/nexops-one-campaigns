// SPDX-License-Identifier: Apache-2.0

package evidence_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/evidence"
)

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

func fileURI(p string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(p)}).String()
}

func TestValidateURI(t *testing.T) {
	for _, ok := range []string{
		"https://dms.example.com/policies/exit-plan.pdf", "s3://evidence-bucket/2026/audit.pdf",
		"file:///srv/evidence/a.pdf", "urn:grc:control-test:4711", "https://dms.example.com/doc?version=3",
	} {
		if err := evidence.ValidateURI(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"", "ftp://x/y", "http://dms.example.com/a", "https://user:pw@dms.example.com/a", "https://u@dms.example.com/a",
		"https://s3.example.com/a?X-Amz-Signature=abc", "https://x.example/a?access_token=t", "https://x.example/a?apiKey=1",
		"https://x.example/a?sig=1", "https:///nohost", "urn:", "file://",
	} {
		if err := evidence.ValidateURI(bad); !errors.Is(err, evidence.ErrInvalid) {
			t.Errorf("%q must be refused, got %v", bad, err)
		}
	}
	if evidence.ValidateChecksum(sum([]byte("x"))) != nil || evidence.ValidateChecksum("sha256:ABC") == nil || evidence.ValidateChecksum("md5:00") == nil {
		t.Fatal("checksum validation")
	}
	if evidence.ValidateKind("document") != nil || evidence.ValidateKind("selfie") == nil {
		t.Fatal("kind validation")
	}
}

func TestVerifierFiles(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "sub", "policy.pdf")
	_ = os.MkdirAll(filepath.Dir(inside), 0o755)
	_ = os.WriteFile(inside, []byte("policy"), 0o644)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(outside, []byte("secret"), 0o644)

	v := evidence.Verifier{Root: root}
	got, err := v.Checksum(context.Background(), fileURI(inside))
	if err != nil || got != sum([]byte("policy")) {
		t.Fatalf("inside = %q %v", got, err)
	}
	if _, err := v.Checksum(context.Background(), fileURI(outside)); !errors.Is(err, evidence.ErrCannotVerifyHere) {
		t.Fatalf("outside the root = %v", err)
	}
	escape := filepath.Join(root, "sub", "..", "..", filepath.Base(filepath.Dir(outside)), "secret.txt")
	if _, err := v.Checksum(context.Background(), fileURI(escape)); !errors.Is(err, evidence.ErrCannotVerifyHere) {
		t.Fatalf("dot-dot escape = %v", err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := v.Checksum(context.Background(), fileURI(link)); !errors.Is(err, evidence.ErrCannotVerifyHere) {
			t.Fatalf("symlink escape = %v", err)
		}
	} else {
		t.Logf("symlink check skipped: %v", err)
	}
	if _, err := (evidence.Verifier{}).Checksum(context.Background(), fileURI(inside)); !errors.Is(err, evidence.ErrCannotVerifyHere) {
		t.Fatalf("no root configured = %v", err)
	}
	if _, err := (evidence.Verifier{Root: root, MaxBytes: 3}).Checksum(context.Background(), fileURI(inside)); err == nil {
		t.Fatal("MaxBytes must be enforced")
	}
	if _, err := v.Checksum(context.Background(), "urn:grc:1"); !errors.Is(err, evidence.ErrCannotVerifyHere) {
		t.Fatalf("urn = %v", err)
	}
}

func TestVerifierBoundaries(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("elsewhere"))
	}))
	defer other.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL+"/x", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("report"))
	}))
	defer srv.Close()
	host := srv.Listener.Addr().String()
	v := evidence.Verifier{AllowHosts: []string{host}, Client: srv.Client()}
	got, err := v.Checksum(context.Background(), srv.URL+"/report.pdf")
	if err != nil || got != sum([]byte("report")) {
		t.Fatalf("allowed host = %q %v", got, err)
	}
	if _, err := v.Checksum(context.Background(), srv.URL+"/redirect"); err == nil {
		t.Fatal("a redirect to a host outside the allowlist must fail")
	}
	before := hits.Load()
	if _, err := (evidence.Verifier{Client: srv.Client()}).Checksum(context.Background(), srv.URL+"/report.pdf"); !errors.Is(err, evidence.ErrCannotVerifyHere) {
		t.Fatalf("empty allowlist = %v", err)
	}
	if hits.Load() != before {
		t.Fatal("a host outside the allowlist must not be contacted")
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("requests = %d (the redirect target must never be fetched)", n)
	}
}
