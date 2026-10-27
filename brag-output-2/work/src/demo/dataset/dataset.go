// SPDX-License-Identifier: Apache-2.0

// Package dataset builds the demo's fictitious register of information:
// "Demo Bank S.A. (fictitious)" with its ICT providers, contractual
// arrangements, service lines, functions and subcontracting chains. Every
// name is invented, every LEI starts with DEMO, and a few faults are made on
// purpose so that the import report and the not-assessed controls have
// something to show. The dataset is built in code and is deterministic.
package dataset

import (
	"fmt"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
)

// Marker starts every demo file.
const Marker = "SAMPLE DATA: fictitious organization"

// Row is one register row: column name to cell text.
type Row map[string]string

// Dataset is the rows of each entity sheet, in sheet order.
type Dataset map[string][]Row

// Fault describes a deliberate fault of the dataset.
type Fault struct {
	Entity, Field, Code string
	Row                 int // spreadsheet row (header is row 1)
	Rejects             bool
}

// Faults are the deliberate import faults: two rejected rows and one warning.
var Faults = []Fault{
	{Entity: "ict_provider", Field: "hq_country", Code: "pattern", Row: 13, Rejects: true},
	{Entity: "service_assessment", Field: "last_audit_date", Code: "format", Row: 3, Rejects: true},
	{Entity: "financial_entity", Field: "lei", Code: "lei_check_digits", Row: 3},
}

// LEI returns a fictitious LEI: DEMO, the reserved 00, a 12-character entity
// part, and valid ISO 17442 check digits.
func LEI(entity string) string {
	prefix := fmt.Sprintf("DEMO00%012s", entity)
	digits, err := canonical.LEICheckDigits(prefix)
	if err != nil {
		panic(err)
	}
	return prefix + digits
}

// badLEI is LEI(entity) with a wrong check digit (the deliberate warning).
func badLEI(entity string) string {
	l := LEI(entity)
	last := l[19]
	if last == '9' {
		last = '0'
	} else {
		last++
	}
	return l[:19] + string(last)
}

var (
	bankLEI       = LEI("BANK00000001")
	subsidiaryLEI = badLEI("PAYM00000002")
)

type provider struct {
	code, name, country, person, parent string
	cost                                string
}

// providers lists twelve fictitious ICT providers; three belong to groups
// whose parent is itself a provider. The last one carries the invalid
// country of a rejected row.
var providers = []provider{
	{"P01", "Cirrolux Hosting S.à r.l. (fictitious)", "LU", "legal_person", "P10", "420000"},
	{"P02", "Quantelle Data Ltd (fictitious)", "IE", "legal_person", "", "310000"},
	{"P03", "Brightwharf Software GmbH (fictitious)", "DE", "legal_person", "", "185000"},
	{"P04", "Northsea Network Services B.V. (fictitious)", "NL", "legal_person", "P11", "96000"},
	{"P05", "Ledgerline Core Systems S.A. (fictitious)", "BE", "legal_person", "", "640000"},
	{"P06", "Harbourlight Security Oy (fictitious)", "FI", "legal_person", "", "72000"},
	{"P07", "Mistral Edge Compute SAS (fictitious)", "FR", "legal_person", "P10", "54000"},
	{"P08", "Aurelian Payments Processing S.p.A. (fictitious)", "IT", "legal_person", "", "230000"},
	{"P09", "Tessaract Analytics AB (fictitious)", "SE", "legal_person", "", "48000"},
	{"P10", "Cirrolux Group Holding S.A. (fictitious)", "LU", "legal_person", "", ""},
	{"P11", "Northsea Telecom Holding N.V. (fictitious)", "NL", "legal_person", "", ""},
	{"P12", "Vellum Archive Services Ltd (fictitious)", "Germany", "legal_person", "", "12000"},
}

func providerLEI(code string) string { return LEI("PROV000000" + code[1:]) }

type serviceType struct{ id, name string }

var serviceTypes = []serviceType{
	{"cloud_iaas", "Cloud infrastructure (IaaS)"},
	{"core_banking_saas", "Core banking software as a service"},
	{"payment_processing", "Payment processing"},
	{"network", "Network and connectivity"},
	{"security_operations", "Security operations and monitoring"},
	{"data_analytics", "Data analytics platform"},
	{"archiving", "Electronic archiving"},
}

type function struct{ id, name, activity, criticality, impact, rto, rpo string }

var functions = []function{
	{"F01", "Customer account management", "deposit_taking", "critical", "high", "240", "15"},
	{"F02", "Payment execution", "payment_services", "critical", "high", "120", "5"},
	{"F03", "Lending and credit decisions", "lending", "important", "medium", "1440", "240"},
	{"F04", "Regulatory reporting", "regulatory_reporting", "important", "medium", "2880", "1440"},
	{"F05", "Treasury and liquidity management", "treasury", "critical", "high", "480", "60"},
	{"F06", "Fraud detection", "payment_services", "critical", "high", "60", "15"},
	{"F07", "Customer onboarding (KYC)", "deposit_taking", "important", "medium", "1440", "240"},
	{"F08", "Document archiving", "support", "not_critical", "low", "4320", "1440"},
}

type arrangement struct {
	ref, typ, cost, overarching string
	provider                    string
}

var arrangements = []arrangement{
	{"CTR-2021-001", "overarching", "180000", "", "P01"},
	{"CTR-2021-002", "subsequent", "240000", "CTR-2021-001", "P01"},
	{"CTR-2022-003", "standalone", "310000", "", "P02"},
	{"CTR-2022-004", "standalone", "185000", "", "P03"},
	{"CTR-2022-005", "standalone", "96000", "", "P04"},
	{"CTR-2023-006", "standalone", "640000", "", "P05"},
	{"CTR-2023-007", "standalone", "72000", "", "P06"},
	{"CTR-2023-008", "standalone", "54000", "", "P07"},
	{"CTR-2023-009", "standalone", "230000", "", "P08"},
	{"CTR-2024-010", "standalone", "48000", "", "P09"},
	{"CTR-2024-011", "standalone", "36000", "", "P02"},
	{"CTR-2024-012", "standalone", "28000", "", "P03"},
	{"CTR-2024-013", "standalone", "12000", "", "P06"},
	{"CTR-2025-014", "standalone", "64000", "", "P05"},
	{"CTR-2025-015", "standalone", "18000", "", "P08"},
}

type serviceLine struct {
	arrangement, provider, function, service string
	start, end                               string
	provision, rest, processing              string // empty rest and processing: the deliberate data-location gaps
	sensitiveness, reliance                  string
}

// serviceLines are twenty ICT service lines. Three lack their data
// locations, so the data-location control is not assessed.
var serviceLines = []serviceLine{
	{"CTR-2021-001", "P01", "F01", "cloud_iaas", "2021-03-01", "2027-02-28", "LU", "LU", "LU", "high", "full"},
	{"CTR-2021-002", "P01", "F05", "cloud_iaas", "2021-09-01", "2027-02-28", "LU", "LU", "DE", "high", "full"},
	{"CTR-2022-003", "P02", "F01", "core_banking_saas", "2022-01-15", "2028-01-14", "IE", "IE", "IE", "high", "full"},
	{"CTR-2022-003", "P02", "F03", "core_banking_saas", "2022-01-15", "2028-01-14", "IE", "IE", "IE", "high", "full"},
	{"CTR-2022-003", "P02", "F07", "core_banking_saas", "2022-01-15", "2028-01-14", "IE", "IE", "NL", "high", "partial"},
	{"CTR-2022-004", "P03", "F04", "data_analytics", "2022-06-01", "2026-05-31", "DE", "DE", "DE", "medium", "partial"},
	{"CTR-2022-005", "P04", "F02", "network", "2022-04-01", "2027-03-31", "NL", "", "", "medium", "full"},
	{"CTR-2022-005", "P04", "F01", "network", "2022-04-01", "2027-03-31", "NL", "NL", "NL", "medium", "full"},
	{"CTR-2023-006", "P05", "F01", "core_banking_saas", "2023-01-01", "2030-12-31", "BE", "BE", "BE", "high", "full"},
	{"CTR-2023-006", "P05", "F02", "core_banking_saas", "2023-01-01", "2030-12-31", "BE", "BE", "BE", "high", "full"},
	{"CTR-2023-006", "P05", "F05", "core_banking_saas", "2023-01-01", "2030-12-31", "BE", "BE", "LU", "high", "full"},
	{"CTR-2023-007", "P06", "F06", "security_operations", "2023-02-01", "2026-01-31", "FI", "FI", "FI", "high", "partial"},
	{"CTR-2023-008", "P07", "F03", "cloud_iaas", "2023-05-01", "2026-04-30", "FR", "", "", "medium", "partial"},
	{"CTR-2023-009", "P08", "F02", "payment_processing", "2023-07-01", "2028-06-30", "IT", "IT", "IT", "high", "full"},
	{"CTR-2023-009", "P08", "F06", "payment_processing", "2023-07-01", "2028-06-30", "IT", "IT", "IT", "high", "full"},
	{"CTR-2024-010", "P09", "F04", "data_analytics", "2024-01-01", "2026-12-31", "SE", "SE", "SE", "low", "low"},
	{"CTR-2024-011", "P02", "F07", "core_banking_saas", "2024-03-01", "2027-02-28", "IE", "", "", "medium", "partial"},
	{"CTR-2024-012", "P03", "F04", "data_analytics", "2024-04-01", "2026-03-31", "DE", "DE", "DE", "low", "low"},
	{"CTR-2024-013", "P06", "F06", "security_operations", "2024-06-01", "2027-05-31", "FI", "FI", "FI", "medium", "partial"},
	{"CTR-2025-014", "P05", "F08", "archiving", "2025-01-01", "2029-12-31", "BE", "BE", "BE", "low", "low"},
}

type chainLink struct{ arrangement, service, provider, rank, recipient string }

// chains are two subcontracting chains: Cirrolux subcontracts edge compute
// to Mistral, and Ledgerline subcontracts hosting to Cirrolux, which in turn
// relies on Northsea's network.
var chains = []chainLink{
	{"CTR-2021-001", "cloud_iaas", "P07", "2", "P01"},
	{"CTR-2023-006", "core_banking_saas", "P01", "2", "P05"},
	{"CTR-2023-006", "core_banking_saas", "P04", "3", "P01"},
}

type assessment struct {
	arrangement, provider, service       string
	substitutability, audit, exit, reint string
	impact, alternatives                 string
}

// assessments of the main ICT services. Three lack the exit plan answer, so
// the exit-plan control is not assessed; one has an invalid audit date and
// is rejected.
var assessments = []assessment{
	{"CTR-2021-001", "P01", "cloud_iaas", "difficult", "2025-06-30", "true", "possible_with_effort", "high", "true"},
	{"CTR-2022-003", "P02", "core_banking_saas", "highly_complex", "2025-13-01", "true", "difficult", "high", "false"},
	{"CTR-2022-004", "P03", "data_analytics", "easy", "2025-03-15", "true", "easy", "medium", "true"},
	{"CTR-2022-005", "P04", "network", "medium", "2025-05-20", "", "possible_with_effort", "medium", "true"},
	{"CTR-2023-006", "P05", "core_banking_saas", "highly_complex", "2025-09-10", "true", "difficult", "high", "false"},
	{"CTR-2023-007", "P06", "security_operations", "medium", "2025-04-01", "", "possible_with_effort", "high", "true"},
	{"CTR-2023-009", "P08", "payment_processing", "difficult", "2025-07-15", "true", "difficult", "high", "true"},
	{"CTR-2024-010", "P09", "data_analytics", "easy", "2025-02-28", "", "easy", "low", "true"},
	{"CTR-2025-014", "P05", "archiving", "easy", "2025-08-01", "true", "easy", "low", "true"},
}

// Build returns the dataset.
func Build() Dataset {
	d := Dataset{}
	d["reporting_entity"] = []Row{{
		"lei": bankLEI, "name": "Demo Bank S.A. (fictitious)", "country": "LU", "entity_type": "credit_institution",
		"competent_authority": "national_competent_authority", "reporting_date": "2026-03-31",
	}}
	d["financial_entity"] = []Row{
		{"lei": bankLEI, "name": "Demo Bank S.A. (fictitious)", "country": "LU", "entity_type": "credit_institution",
			"group_hierarchy": "ultimate_parent", "last_update_date": "2026-03-31", "integration_date": "2020-01-01",
			"currency": "EUR", "total_assets": "18500000000", "_not_applicable": "parent_lei,deletion_date"},
		{"lei": subsidiaryLEI, "name": "Demo Payments S.A. (fictitious)", "country": "LU", "entity_type": "payment_institution",
			"group_hierarchy": "subsidiary", "parent_lei": bankLEI, "last_update_date": "2026-03-31", "integration_date": "2022-07-01",
			"currency": "EUR", "total_assets": "420000000", "_not_applicable": "deletion_date"},
	}
	d["branch"] = []Row{{"branch_id_code": "BR-BE-01", "head_office_lei": bankLEI, "name": "Demo Bank S.A. Brussels branch (fictitious)", "country": "BE"}}
	for _, s := range serviceTypes {
		d["ict_service_type"] = append(d["ict_service_type"], Row{"id": s.id, "name": s.name})
	}
	for _, p := range providers {
		r := Row{"provider_id_code": providerLEI(p.code), "provider_id_type": "lei", "legal_name": p.name, "person_type": p.person,
			"hq_country": p.country, "currency": "EUR", "total_annual_cost": p.cost}
		if p.parent != "" {
			r["parent_id_code"], r["parent_id_type"] = providerLEI(p.parent), "lei"
		} else {
			r["_not_applicable"] = "parent_id_code"
		}
		if p.cost == "" {
			delete(r, "total_annual_cost")
		}
		d["ict_provider"] = append(d["ict_provider"], r)
	}
	for _, f := range functions {
		d["function"] = append(d["function"], Row{"function_id": f.id, "function_name": f.name, "licensed_activity": f.activity,
			"financial_entity_lei": bankLEI, "criticality_assessment": f.criticality, "last_assessment_date": "2025-11-30",
			"rto": f.rto, "rpo": f.rpo, "discontinuing_impact": f.impact})
	}
	for _, a := range arrangements {
		r := Row{"arrangement_ref": a.ref, "arrangement_type": a.typ, "currency": "EUR", "annual_cost": a.cost}
		if a.overarching != "" {
			r["overarching_arrangement_ref"] = a.overarching
		}
		d["contractual_arrangement"] = append(d["contractual_arrangement"], r)
		d["arrangement_party"] = append(d["arrangement_party"],
			Row{"arrangement_ref": a.ref, "role": "recipient_signatory", "party_id_code": bankLEI, "party_id_type": "lei"},
			Row{"arrangement_ref": a.ref, "role": "provider_signatory", "party_id_code": providerLEI(a.provider), "party_id_type": "lei"})
		d["arrangement_user_entity"] = append(d["arrangement_user_entity"],
			Row{"arrangement_ref": a.ref, "financial_entity_lei": bankLEI, "is_branch": "no", "_not_applicable": "branch_id_code"})
	}
	for _, s := range serviceLines {
		d["arrangement_service_line"] = append(d["arrangement_service_line"], Row{
			"arrangement_ref": s.arrangement, "financial_entity_lei": bankLEI, "provider_id_code": providerLEI(s.provider), "provider_id_type": "lei",
			"function_id": s.function, "ict_service_type": s.service, "start_date": s.start, "end_date": s.end,
			"notice_period_entity": "90", "notice_period_provider": "180", "governing_law_country": "LU",
			"country_of_provision": s.provision, "data_stored": "yes", "data_at_rest_country": s.rest, "data_processing_country": s.processing,
			"data_sensitiveness": s.sensitiveness, "reliance_level": s.reliance,
		})
	}
	for _, c := range chains {
		d["supply_chain_link"] = append(d["supply_chain_link"], Row{"arrangement_ref": c.arrangement, "ict_service_type": c.service,
			"provider_id_code": providerLEI(c.provider), "provider_id_type": "lei", "rank": c.rank,
			"recipient_id_code": providerLEI(c.recipient), "recipient_id_type": "lei"})
	}
	for _, a := range assessments {
		r := Row{"arrangement_ref": a.arrangement, "provider_id_code": providerLEI(a.provider), "provider_id_type": "lei",
			"ict_service_type": a.service, "substitutability": a.substitutability, "last_audit_date": a.audit,
			"reintegration_possibility": a.reint, "discontinuing_impact": a.impact, "alternatives_identified": a.alternatives}
		if a.exit != "" {
			r["exit_plan_exists"] = a.exit
		}
		if a.alternatives == "false" {
			r["not_substitutable_reason"] = "proprietary_platform"
		}
		d["service_assessment"] = append(d["service_assessment"], r)
	}
	return d
}

// LEIs lists every LEI-shaped identifier of the dataset.
func LEIs() []string {
	out := []string{bankLEI, subsidiaryLEI}
	for _, p := range providers {
		out = append(out, providerLEI(p.code))
	}
	return out
}
