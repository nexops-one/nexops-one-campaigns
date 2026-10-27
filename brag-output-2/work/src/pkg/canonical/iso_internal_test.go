// SPDX-License-Identifier: Apache-2.0

package canonical

import "testing"

func TestISOCountryListIsComplete(t *testing.T) {
	if len(isoCountries) != 249 {
		t.Fatalf("ISO 3166-1 alpha-2 has 249 officially assigned codes, list has %d", len(isoCountries))
	}
}
