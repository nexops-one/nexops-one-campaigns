// SPDX-License-Identifier: Apache-2.0

package canonical_test

import (
	"reflect"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func latestSchema(t *testing.T) *schema.Schema {
	t.Helper()
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	return reg.Latest()
}

func TestValidLEI(t *testing.T) {
	if !canonical.ValidLEI("SAMPLEFE000000000021") {
		t.Error("SAMPLEFE000000000021 has valid check digits")
	}
	for _, bad := range []string{"SAMPLEFE000000000001", "samplefe000000000021", "SAMPLEFE00000000002", "SAMPLEFE0000000000AB", ""} {
		if canonical.ValidLEI(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestLEICheckDigits(t *testing.T) {
	got, err := canonical.LEICheckDigits("SAMPLETP0000000000")
	if err != nil || got != "87" {
		t.Fatalf("got %q, %v", got, err)
	}
	if !canonical.ValidLEI("SAMPLETP0000000000" + got) {
		t.Fatal("generated LEI must validate")
	}
	if _, err := canonical.LEICheckDigits("SHORT"); err == nil {
		t.Fatal("short prefix must fail")
	}
	if _, err := canonical.LEICheckDigits("sampletp0000000000"); err == nil {
		t.Fatal("lowercase prefix must fail")
	}
}

func TestCountryAndCurrency(t *testing.T) {
	for _, c := range []string{"LU", "IE", "US", "GB", "DE"} {
		if !canonical.ValidCountry(c) {
			t.Errorf("%s should be valid", c)
		}
	}
	for _, c := range []string{"UK", "XX", "lu", "EUR", ""} {
		if canonical.ValidCountry(c) {
			t.Errorf("%s should be invalid", c)
		}
	}
	for _, c := range []string{"EUR", "USD", "CHF", "GBP", "JPY"} {
		if !canonical.ValidCurrency(c) {
			t.Errorf("%s should be valid", c)
		}
	}
	for _, c := range []string{"EUX", "eur", "US", "XXX"} {
		if canonical.ValidCurrency(c) {
			t.Errorf("%s should be invalid", c)
		}
	}
}

func TestCheckIdentifiers(t *testing.T) {
	s := latestSchema(t)
	line, _ := s.Entity("arrangement_service_line")
	rec := adapter.Record{
		"financial_entity_lei": "SAMPLEFE000000000001",
		"country_of_provision": "UK",
		"data_at_rest_country": "IE",
		"provider_id_code":     "not-an-lei-field",
	}
	var got [][2]string
	for _, e := range canonical.CheckIdentifiers(line, rec) {
		got = append(got, [2]string{e.Field, e.Code})
	}
	want := [][2]string{{"country_of_provision", "iso_country"}, {"financial_entity_lei", "lei_check_digits"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	arr, _ := s.Entity("contractual_arrangement")
	errs := canonical.CheckIdentifiers(arr, adapter.Record{"currency": "EUX"})
	if len(errs) != 1 || errs[0].Code != "iso_currency" || errs[0].Entity != "contractual_arrangement" {
		t.Fatalf("currency errors = %+v", errs)
	}
	if errs := canonical.CheckIdentifiers(arr, adapter.Record{"currency": "EUR"}); errs != nil {
		t.Fatalf("EUR should pass: %+v", errs)
	}
}
