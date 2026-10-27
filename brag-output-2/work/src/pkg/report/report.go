// SPDX-License-Identifier: Apache-2.0

// Package report holds what every report profile shares (the metadata block,
// the disclaimer, URI redaction and the facts hash) and the open-core
// Profile B, the readiness and evidence report. Profiles are pure: the same
// input yields the same facts and the same files, byte for byte.
package report

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
)

// Disclaimer is the non-certification statement every report carries.
const Disclaimer = "Not legal advice, not certification. This report states what the compliance engine evaluated " +
	"from the data supplied, at the time shown. Wording pending legal review."

// SampleWatermark labels every page of a report made in a sample workspace.
const SampleWatermark = "SAMPLE: not a compliance status"

// JSONFile is the name of the facts rendering every report has.
const JSONFile = "report.json"

// Hash returns "sha256:<hex>" of data.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Marshal encodes facts deterministically: indented, keys in struct order,
// HTML characters kept as they are.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Redact reduces an evidence location to its scheme and host: paths, queries,
// fragments and credentials never reach a report. Opaque references (urn,
// managed) keep their scheme only.
func Redact(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" {
		return "reference"
	}
	scheme := strings.ToLower(u.Scheme)
	if u.Host == "" {
		return scheme
	}
	return scheme + "://" + strings.ToLower(u.Host)
}
