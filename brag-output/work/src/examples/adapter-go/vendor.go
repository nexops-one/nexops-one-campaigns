// SPDX-License-Identifier: Apache-2.0

// Command adapter-go is the example adapter of the adapter developer guide
// (docs/adapters.md). It maps the JSON export of a fictional vendor inventory
// system to canonical ict_provider and cloud_resource records. It depends only
// on the SDK module: adapter (contract and batch builder), adaptertest
// (conformance, see vendor_test.go) and push (delivery to an engine).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// Export is the vendor inventory system's JSON export.
type Export struct {
	ExportedAt string   `json:"exported_at"`
	Vendors    []Vendor `json:"vendors"`
}

// Vendor is one supplier in the export.
type Vendor struct {
	ID        string     `json:"id"`
	LEI       string     `json:"lei,omitempty"`
	Name      string     `json:"name"`
	Country   string     `json:"country"`
	Resources []Resource `json:"resources"`
}

// Resource is one cloud resource a vendor runs for the organization.
type Resource struct {
	ID      string `json:"id"`
	Service string `json:"service"`
	Region  string `json:"region"`
}

// regionCountry maps cloud regions to the country hosting them. A region that
// is not listed leaves the country missing: the adapter never guesses.
var regionCountry = map[string]string{
	"eu-west-1":    "IE",
	"eu-west-3":    "FR",
	"eu-central-1": "DE",
}

// VendorAdapter reads the export file on every pull.
type VendorAdapter struct {
	Path string
}

// Manifest declares what the adapter supplies.
func (a *VendorAdapter) Manifest() adapter.Manifest {
	return adapter.Manifest{
		Name: "vendor-inventory", Version: "1.0.0", SchemaVersion: "0.1.0",
		Supplies: map[string][]string{
			"ict_provider":   {"provider_id_code", "provider_id_type", "legal_name", "hq_country"},
			"cloud_resource": {"resource_ref", "provider_id_code", "service_name", "region", "country"},
		},
		Modes: []adapter.Mode{adapter.ModeFull, adapter.ModeIncremental},
	}
}

// set adds a field only when the source has a value: an absent value stays
// missing (never an empty string or a default).
func set(rec adapter.Record, field, value string) {
	if value != "" {
		rec[field] = value
	}
}

// Pull maps the whole export. The export has no change tracking, so an
// incremental pull sends every record too; nothing is deleted in that mode.
func (a *VendorAdapter) Pull(_ context.Context, req adapter.SyncRequest) (adapter.Batch, error) {
	data, err := os.ReadFile(a.Path)
	if err != nil {
		return adapter.Batch{}, err
	}
	var exp Export
	if err := json.Unmarshal(data, &exp); err != nil {
		return adapter.Batch{}, fmt.Errorf("%s: %w", a.Path, err)
	}
	mode := req.Mode
	if mode == "" {
		mode = adapter.ModeFull
	}
	b := adapter.NewBuilder(a.Manifest(), "vendor-inventory-export").Mode(mode).BatchID("vendors@" + exp.ExportedAt)
	for _, v := range exp.Vendors {
		code := "vendor:" + v.ID
		if v.LEI != "" {
			code = v.LEI
		}
		p := adapter.Record{"provider_id_code": code}
		if v.LEI != "" {
			p["provider_id_type"] = "LEI"
		}
		set(p, "legal_name", v.Name)
		set(p, "hq_country", v.Country)
		adapter.SetSourceRef(p, "vendors/"+v.ID)
		b.Add("ict_provider", p)
		for _, r := range v.Resources {
			rec := adapter.Record{"resource_ref": v.ID + "/" + r.ID, "provider_id_code": code}
			set(rec, "service_name", r.Service)
			set(rec, "region", r.Region)
			if c, ok := regionCountry[r.Region]; ok {
				rec["country"] = c
				adapter.MarkDerived(rec, adapter.DerivedField{Field: "country", Method: "region_lookup", SourceRef: r.Region})
			}
			adapter.SetSourceRef(rec, "vendors/"+v.ID+"/resources/"+r.ID)
			b.Add("cloud_resource", rec)
		}
	}
	return b.Build(), nil
}
