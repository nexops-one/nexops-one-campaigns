// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// ReportFileMeta describes one stored rendering of a report.
type ReportFileMeta struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

// ReportFile is a stored rendering with its content.
type ReportFile struct {
	ReportFileMeta
	Data []byte `json:"-"`
}

// Report is an immutable generated report. Facts are kept byte for byte:
// FactsHash is computed over them. Inputs hold what reproducing the report
// needs besides the stored snapshot and evaluation; the engine defines them.
type Report struct {
	ID            string           `json:"id"`
	Scope         adapter.Scope    `json:"scope"`
	Profile       string           `json:"profile"`
	CreatedAt     time.Time        `json:"created_at"`
	CreatedBy     string           `json:"created_by"`
	CreatedByKind string           `json:"created_by_kind"`
	EvaluationID  string           `json:"evaluation_id"`
	SnapshotID    string           `json:"snapshot_id"`
	Catalogs      []string         `json:"catalogs"`
	AsOf          time.Time        `json:"as_of"`
	Sample        bool             `json:"sample"`
	Complete      bool             `json:"complete"`
	FactsHash     string           `json:"facts_hash"`
	Files         []ReportFileMeta `json:"files"`
	Inputs        json.RawMessage  `json:"inputs,omitempty"`
	Facts         json.RawMessage  `json:"facts,omitempty"`
	Validation    json.RawMessage  `json:"validation,omitempty"`
}

// ReportRef identifies a stored report for retention.
type ReportRef struct {
	ID           string    `json:"id"`
	EvaluationID string    `json:"evaluation_id"`
	SnapshotID   string    `json:"snapshot_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// ReportStore stores immutable reports and their renderings.
type ReportStore interface {
	// SaveReport stores a report, its files and events in one transaction; it
	// fails with ErrExists when the ID is taken. r.Files must describe files.
	SaveReport(ctx context.Context, r Report, files []ReportFile, events ...AuditEvent) error
	// Report returns a report with its facts, validation and inputs.
	Report(ctx context.Context, scope adapter.Scope, id string) (Report, error)
	// Reports lists a workspace's reports, newest first (then by ID), without
	// facts, validation or inputs.
	Reports(ctx context.Context, scope adapter.Scope) ([]Report, error)
	ReportFile(ctx context.Context, scope adapter.Scope, id, name string) (ReportFile, error)
	// ReportRefs lists a workspace's reports ordered by ID.
	ReportRefs(ctx context.Context, scope adapter.Scope) ([]ReportRef, error)
}

// CloneReport deep-copies a report.
func CloneReport(r Report) Report {
	r.Catalogs = append([]string{}, r.Catalogs...)
	r.Files = append([]ReportFileMeta{}, r.Files...)
	r.Inputs = cloneRaw(r.Inputs)
	r.Facts = cloneRaw(r.Facts)
	r.Validation = cloneRaw(r.Validation)
	return r
}

func cloneRaw(b json.RawMessage) json.RawMessage {
	if b == nil {
		return nil
	}
	return append(json.RawMessage{}, b...)
}
