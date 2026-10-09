// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const evidenceSynopsis = "evidence verify [--url U] [--token T] --file PATH <evidence-id>   (or COMPLIANCE_URL, COMPLIANCE_API_TOKEN)"

// runEvidence verifies an evidence object where it lives: the file is hashed
// locally and only the checksum is sent to the engine.
func runEvidence(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 || args[0] != "verify" {
		return usageErr(env, evidenceSynopsis)
	}
	fs := flag.NewFlagSet("evidence verify", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	base := fs.String("url", env.Getenv("COMPLIANCE_URL"), "engine base URL, for example https://compliance.example.com")
	token := fs.String("token", "", "API token with the evidence.write permission (default COMPLIANCE_API_TOKEN)")
	file := fs.String("file", "", "local copy of the evidence object")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 1 || *file == "" {
		return usageErr(env, evidenceSynopsis)
	}
	if *token == "" {
		*token = env.Getenv("COMPLIANCE_API_TOKEN")
	}
	if *base == "" || *token == "" {
		return fail(env, "the engine URL and an API token are required (--url and --token, or COMPLIANCE_URL and COMPLIANCE_API_TOKEN)")
	}
	u, err := url.Parse(strings.TrimRight(*base, "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fail(env, "--url %q is not an http(s) URL", *base)
	}
	f, err := os.Open(*file)
	if err != nil {
		return fail(env, "%v", err)
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		return fail(env, "%v", err)
	}
	checksum := "sha256:" + hex.EncodeToString(h.Sum(nil))
	body, _ := json.Marshal(map[string]string{"checksum": checksum})
	endpoint := u.String() + "/api/v1/evidence/" + url.PathEscape(fs.Arg(0)) + "/checks"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fail(env, "%v", err)
	}
	req.Header.Set("Authorization", "Bearer "+*token)
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: time.Minute}).Do(req)
	if err != nil {
		return fail(env, "%v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return fail(env, "the engine answered HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	var ev struct {
		Checksum  string `json:"checksum"`
		Integrity string `json:"integrity"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return fail(env, "unexpected answer: %v", err)
	}
	fmt.Fprintf(env.Stdout, "local file: %s\nrecorded:   %s\nintegrity:  %s\n", checksum, ev.Checksum, ev.Integrity)
	if ev.Integrity != "verified" {
		return fail(env, "the file does not match the recorded checksum; dependent controls are downgraded")
	}
	return 0
}
