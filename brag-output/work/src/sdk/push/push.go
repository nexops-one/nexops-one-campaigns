// SPDX-License-Identifier: Apache-2.0

// Package push registers an adapter's manifest with a compliance engine and
// submits its batches over the engine's HTTP API (/api/v1). It is how
// out-of-process adapters, in connected mode or on another host, deliver data.
// It uses only the standard library.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

const (
	// DefaultMaxBodyBytes keeps requests well below the engine's default 32 MiB body limit.
	DefaultMaxBodyBytes = 16 << 20
	// DefaultMaxRecords keeps requests well below the engine's default 50,000-record limit.
	DefaultMaxRecords = 10000
	// DefaultMaxAttempts is the number of attempts per request, including the first.
	DefaultMaxAttempts = 4
	// DefaultBackoff is the delay before the first retry; it doubles on each retry.
	DefaultBackoff = 500 * time.Millisecond
)

var (
	// ErrFullBatchTooLarge: a full-mode batch exceeds the per-request limits.
	// Full batches are never split, because each part would delete the records
	// the other parts supplied. Raise the engine's limits (COMPLIANCE_MAX_RECORDS,
	// COMPLIANCE_MAX_BODY_BYTES) and the client's, or sync incrementally.
	ErrFullBatchTooLarge = errors.New("push: full-mode batch exceeds the per-request limits and full batches are never split")
	// ErrRecordTooLarge: one record alone exceeds MaxBodyBytes.
	ErrRecordTooLarge = errors.New("push: a record exceeds the per-request size limit")
)

// Client talks to one engine. Zero-valued fields take the defaults.
type Client struct {
	BaseURL      string // engine base URL, without /api/v1
	Token        string // API bearer token (its scope selects the tenant and workspace)
	HTTPClient   *http.Client
	MaxBodyBytes int
	MaxRecords   int
	MaxAttempts  int
	Backoff      time.Duration
}

// New returns a client for the engine at baseURL.
func New(baseURL, token string) *Client {
	return &Client{BaseURL: baseURL, Token: token}
}

func (c *Client) config() Client {
	cfg := *c
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if cfg.MaxRecords <= 0 {
		cfg.MaxRecords = DefaultMaxRecords
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = DefaultBackoff
	}
	return cfg
}

// IngestionResult is the engine's response to one ingestion request.
type IngestionResult struct {
	IngestionID     string              `json:"ingestion_id"`
	BatchID         string              `json:"batch_id,omitempty"`
	SchemaVersion   string              `json:"schema_version"`
	Mode            adapter.Mode        `json:"mode"`
	Accepted        int                 `json:"accepted"`
	RejectedRecords int                 `json:"rejected_records"`
	Errors          []schema.FieldError `json:"errors"`
	Warnings        []schema.FieldError `json:"warnings"`
	Created         int                 `json:"created"`
	Updated         int                 `json:"updated"`
	Deleted         int                 `json:"deleted"`
	Unchanged       int                 `json:"unchanged"`
	NoChanges       bool                `json:"no_changes"`
	SnapshotID      string              `json:"snapshot_id"`
}

// APIError is a non-2xx response from the engine.
type APIError struct {
	Status  int                 `json:"-"`
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Details []schema.FieldError `json:"details,omitempty"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("push: engine returned %d %s: %s", e.Status, e.Code, e.Message)
}

// RegisterManifest registers (or replaces) the adapter's manifest.
func (c *Client) RegisterManifest(ctx context.Context, m adapter.Manifest) error {
	body, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("push: encode manifest: %w", err)
	}
	return c.do(ctx, http.MethodPut, "/api/v1/adapters/"+url.PathEscape(m.Name)+"/manifest", body, nil)
}

// Push submits a batch and returns one result per request sent. A batch
// within MaxBodyBytes and MaxRecords is one request. A larger incremental
// batch is split in entity-name then record order; each part keeps the source
// and mode, and its batch ID gets a "#<n>" suffix. A full-mode batch is never
// split: it fails with ErrFullBatchTooLarge before anything is sent. If a
// request fails, the results of the requests already committed are returned
// with the error.
func (c *Client) Push(ctx context.Context, b adapter.Batch) ([]IngestionResult, error) {
	cfg := c.config()
	parts, err := split(b, cfg.MaxBodyBytes, cfg.MaxRecords)
	if err != nil {
		return nil, err
	}
	results := make([]IngestionResult, 0, len(parts))
	for _, body := range parts {
		var res IngestionResult
		if err := c.do(ctx, http.MethodPost, "/api/v1/ingestions", body, &res); err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

// wire is a batch whose records are already encoded.
type wire struct {
	SchemaVersion string                       `json:"schema_version"`
	Batch         *adapter.BatchInfo           `json:"batch,omitempty"`
	Source        adapter.Source               `json:"source"`
	Entities      map[string][]json.RawMessage `json:"entities"`
}

func split(b adapter.Batch, maxBytes, maxRecords int) ([][]byte, error) {
	whole, err := json.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("push: encode batch: %w", err)
	}
	n := b.RecordCount()
	if len(whole) <= maxBytes && n <= maxRecords {
		return [][]byte{whole}, nil
	}
	if b.Mode() == adapter.ModeFull {
		return nil, fmt.Errorf("%w: %d records in %d bytes, limits %d records and %d bytes", ErrFullBatchTooLarge, n, len(whole), maxRecords, maxBytes)
	}
	names := make([]string, 0, len(b.Entities))
	for name := range b.Entities {
		names = append(names, name)
	}
	sort.Strings(names)
	info := func(part int) *adapter.BatchInfo {
		if b.Batch == nil {
			return nil
		}
		i := *b.Batch
		if i.BatchID != "" {
			i.BatchID += "#" + strconv.Itoa(part)
		}
		return &i
	}
	// overhead bounds the envelope of any part: every entity key and the longest suffix.
	empty := map[string][]json.RawMessage{}
	for _, name := range names {
		empty[name] = []json.RawMessage{}
	}
	env, err := json.Marshal(wire{SchemaVersion: b.SchemaVersion, Batch: info(n), Source: b.Source, Entities: empty})
	if err != nil {
		return nil, fmt.Errorf("push: encode batch: %w", err)
	}
	overhead := len(env)

	var parts [][]byte
	cur, size, count := map[string][]json.RawMessage{}, overhead, 0
	flush := func() error {
		data, err := json.Marshal(wire{SchemaVersion: b.SchemaVersion, Batch: info(len(parts) + 1), Source: b.Source, Entities: cur})
		if err != nil {
			return fmt.Errorf("push: encode batch: %w", err)
		}
		parts = append(parts, data)
		cur, size, count = map[string][]json.RawMessage{}, overhead, 0
		return nil
	}
	for _, name := range names {
		for i, rec := range b.Entities[name] {
			raw, err := json.Marshal(rec)
			if err != nil {
				return nil, fmt.Errorf("push: encode %s[%d]: %w", name, i, err)
			}
			add := len(raw) + 1 // the record and its separator
			if overhead+add > maxBytes {
				return nil, fmt.Errorf("%w: %s[%d] is %d bytes, limit %d", ErrRecordTooLarge, name, i, len(raw), maxBytes)
			}
			if count > 0 && (size+add > maxBytes || count+1 > maxRecords) {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			cur[name] = append(cur[name], raw)
			size += add
			count++
		}
	}
	if count > 0 {
		if err := flush(); err != nil {
			return nil, err
		}
	}
	return parts, nil
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	cfg := c.config()
	for attempt := 1; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, cfg.BaseURL+path, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("push: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if cfg.Token != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
		}
		var lastErr error
		retry := true
		resp, err := cfg.HTTPClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = fmt.Errorf("push: %s %s: %w", method, path, err)
		} else {
			lastErr = handle(resp, out)
			if lastErr == nil {
				return nil
			}
			retry = retryable(resp.StatusCode)
		}
		if !retry || attempt >= cfg.MaxAttempts {
			return lastErr
		}
		t := time.NewTimer(cfg.Backoff << (attempt - 1))
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

func handle(resp *http.Response, out any) error {
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("push: read response: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("push: decode response: %w", err)
		}
		return nil
	}
	var env struct {
		Error *APIError `json:"error"`
	}
	if json.Unmarshal(data, &env) == nil && env.Error != nil {
		env.Error.Status = resp.StatusCode
		return env.Error
	}
	msg := strings.TrimSpace(string(data))
	if len(msg) > 512 {
		msg = msg[:512]
	}
	return &APIError{Status: resp.StatusCode, Message: msg}
}
