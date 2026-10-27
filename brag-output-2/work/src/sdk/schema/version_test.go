// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func TestParseVersion(t *testing.T) {
	v, err := schema.ParseVersion("v1.2.3")
	if err != nil || v != (schema.Version{Major: 1, Minor: 2, Patch: 3}) || v.String() != "1.2.3" {
		t.Fatalf("got %v %v", v, err)
	}
	for _, bad := range []string{"", "1.2", "1.2.x", "1.-2.3"} {
		if _, err := schema.ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) should fail", bad)
		}
	}
}

func TestSatisfies(t *testing.T) {
	cases := []struct {
		version, constraint string
		want                bool
	}{
		{"0.1.0", "^0.1", true},
		{"0.1.9", "^0.1", true},
		{"0.2.0", "^0.1", false},
		{"1.3.0", "^1.2", true},
		{"1.1.9", "^1.2", false},
		{"2.0.0", "^1.2", false},
		{"1.2.3", "1.2.3", true},
		{"1.2.4", "1.2.3", false},
	}
	for _, c := range cases {
		v, _ := schema.ParseVersion(c.version)
		got, err := v.Satisfies(c.constraint)
		if err != nil || got != c.want {
			t.Errorf("%s satisfies %s = %v, %v; want %v", c.version, c.constraint, got, err, c.want)
		}
	}
	v, _ := schema.ParseVersion("1.0.0")
	if _, err := v.Satisfies("^abc"); err == nil {
		t.Error("invalid constraint should fail")
	}
}
