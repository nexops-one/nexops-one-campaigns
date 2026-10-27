// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/importer"
	"github.com/nexops-one/compliance-engine/pkg/ingest"
)

// ImportResult is the validation report of one file import.
type ImportResult struct {
	File         string                  `json:"file"`
	Format       importer.Format         `json:"format"`
	Adapter      string                  `json:"adapter"`
	Rows         int                     `json:"rows"`
	Result       ingest.Result           `json:"result"`
	Completeness *canonical.Completeness `json:"completeness,omitempty"`
}

// Import reads a CSV or XLSX file, registers its importer adapter's manifest,
// and ingests the rows (or, with dryRun, reports what ingesting them would do,
// including the completeness of the would-be snapshot). Rows that fail
// validation are reported with their entity:row location, never dropped.
func (e *Engine) Import(ctx context.Context, scope Scope, in importer.Input, dryRun bool) (ImportResult, error) {
	if err := scope.Validate(); err != nil {
		return ImportResult{}, err
	}
	if _, err := e.require(ctx, scope, extension.FeatureRegisterImport); err != nil {
		return ImportResult{}, err
	}
	p, err := importer.Parse(e.schemas.Latest(), in)
	if err != nil {
		return ImportResult{}, err
	}
	if err := e.RegisterManifest(ctx, scope, p.Manifest); err != nil {
		return ImportResult{}, err
	}
	res := ImportResult{File: in.Name, Format: p.Format, Adapter: p.Manifest.Name, Rows: p.Rows}
	if dryRun {
		dr, err := e.DryRun(ctx, scope, p.Batch)
		if err != nil {
			return ImportResult{}, err
		}
		res.Result, res.Completeness = dr.Result, &dr.Completeness
		return res, nil
	}
	if res.Result, err = e.Ingest(ctx, scope, p.Batch); err != nil {
		return ImportResult{}, err
	}
	return res, nil
}
