// SPDX-License-Identifier: Apache-2.0

// Package canonical implements the engine side of the canonical model:
// identifier checks (L1b), record identity and field states, codelists,
// referential analysis (L2) and register completeness (L3).
package canonical

import (
	"fmt"
	"sort"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// IdentifierPolicy decides whether failed L1b checks reject a record or only warn.
type IdentifierPolicy string

const (
	PolicyWarn   IdentifierPolicy = "warn"
	PolicyReject IdentifierPolicy = "reject"
)

// ValidLEI reports whether s is a 20-character LEI whose ISO 17442 check
// digits (ISO 7064 MOD 97-10) are valid.
func ValidLEI(s string) bool {
	if len(s) != 20 || s[18] < '0' || s[18] > '9' || s[19] < '0' || s[19] > '9' {
		return false
	}
	rem, ok := mod97(s)
	return ok && rem == 1
}

// LEICheckDigits returns the two check digits for an 18-character LEI prefix.
func LEICheckDigits(prefix string) (string, error) {
	if len(prefix) != 18 {
		return "", fmt.Errorf("LEI prefix must have 18 characters, got %d", len(prefix))
	}
	rem, ok := mod97(prefix + "00")
	if !ok {
		return "", fmt.Errorf("LEI prefix %q has characters outside A-Z0-9", prefix)
	}
	return fmt.Sprintf("%02d", 98-rem), nil
}

func mod97(s string) (int, bool) {
	rem := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			rem = (rem*10 + int(r-'0')) % 97
		case r >= 'A' && r <= 'Z':
			rem = (rem*100 + int(r-'A') + 10) % 97
		default:
			return 0, false
		}
	}
	return rem, true
}

// ValidCountry reports ISO 3166-1 alpha-2 membership.
func ValidCountry(s string) bool { _, ok := isoCountries[s]; return ok }

// ValidCurrency reports ISO 4217 membership.
func ValidCurrency(s string) bool { _, ok := isoCurrencies[s]; return ok }

// CheckIdentifiers applies L1b checks to the record's LEI, country and currency fields.
func CheckIdentifiers(e *schema.Entity, rec adapter.Record) []schema.FieldError {
	var errs []schema.FieldError
	for _, name := range e.FieldNames {
		v, ok := rec[name].(string)
		if !ok || v == "" {
			continue
		}
		var code, msg string
		switch e.Fields[name].Kind {
		case schema.KindLEI:
			if !ValidLEI(v) {
				code, msg = "lei_check_digits", fmt.Sprintf("%q fails the ISO 17442 check digits", v)
			}
		case schema.KindCountry:
			if !ValidCountry(v) {
				code, msg = "iso_country", fmt.Sprintf("%q is not an ISO 3166-1 alpha-2 country code", v)
			}
		case schema.KindCurrency:
			if !ValidCurrency(v) {
				code, msg = "iso_currency", fmt.Sprintf("%q is not an ISO 4217 currency code", v)
			}
		}
		if code != "" {
			errs = append(errs, schema.FieldError{Entity: e.Name, Field: name, Code: code, Message: msg})
		}
	}
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Field < errs[j].Field })
	return errs
}
