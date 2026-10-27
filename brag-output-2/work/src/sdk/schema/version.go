// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a semantic version MAJOR.MINOR.PATCH.
type Version struct{ Major, Minor, Patch int }

// ParseVersion parses "1.2.3" or "v1.2.3".
func ParseVersion(s string) (Version, error) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("invalid version %q: want MAJOR.MINOR.PATCH", s)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Version{}, fmt.Errorf("invalid version %q: want MAJOR.MINOR.PATCH", s)
		}
		n[i] = v
	}
	return Version{Major: n[0], Minor: n[1], Patch: n[2]}, nil
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Less orders versions by major, then minor, then patch.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// Satisfies reports whether v matches a constraint: an exact "1.2.3", or a
// caret range "^1.2" / "^1.2.3" meaning same major and not lower (for 0.x:
// same minor and not lower).
func (v Version) Satisfies(constraint string) (bool, error) {
	c := strings.TrimSpace(constraint)
	if !strings.HasPrefix(c, "^") {
		want, err := ParseVersion(c)
		if err != nil {
			return false, err
		}
		return v == want, nil
	}
	c = strings.TrimPrefix(c, "^")
	if strings.Count(c, ".") == 1 {
		c += ".0"
	}
	min, err := ParseVersion(c)
	if err != nil {
		return false, fmt.Errorf("invalid constraint %q", constraint)
	}
	if v.Less(min) || v.Major != min.Major {
		return false, nil
	}
	if min.Major == 0 && v.Minor != min.Minor {
		return false, nil
	}
	return true, nil
}
