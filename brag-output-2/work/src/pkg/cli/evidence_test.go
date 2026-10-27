// SPDX-License-Identifier: Apache-2.0

package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidenceVerify(t *testing.T) {
	recorded := sha256.Sum256([]byte("the report"))
	want := "sha256:" + hex.EncodeToString(recorded[:])
	var got struct{ Checksum string }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/evidence/evd-1/checks" || r.Header.Get("Authorization") != "Bearer tkn" {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		integrity := "failed"
		if got.Checksum == want {
			integrity = "verified"
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"checksum": want, "integrity": integrity})
	}))
	defer srv.Close()
	dir := t.TempDir()
	good := filepath.Join(dir, "report.pdf")
	bad := filepath.Join(dir, "tampered.pdf")
	_ = os.WriteFile(good, []byte("the report"), 0o644)
	_ = os.WriteFile(bad, []byte("changed"), 0o644)
	vars := map[string]string{"COMPLIANCE_URL": srv.URL, "COMPLIANCE_API_TOKEN": "tkn"}
	code, out, errOut := run(t, vars, "evidence", "verify", "--file", good, "evd-1")
	if code != 0 || got.Checksum != want || !strings.Contains(out, "integrity:  verified") {
		t.Fatalf("match = %d %q %q", code, out, errOut)
	}
	code, out, errOut = run(t, vars, "evidence", "verify", "--file", bad, "evd-1")
	if code != 1 || !strings.Contains(out, "integrity:  failed") || !strings.Contains(errOut, "does not match") {
		t.Fatalf("mismatch = %d %q %q", code, out, errOut)
	}
	if code, _, errOut := run(t, nil, "evidence", "verify", "--file", good, "evd-1"); code != 1 || !strings.Contains(errOut, "API token are required") {
		t.Fatalf("no URL = %d %q", code, errOut)
	}
	if code, _, _ := run(t, vars, "evidence", "verify", "evd-1"); code != 2 {
		t.Fatal("--file is required")
	}
}
