// SPDX-License-Identifier: Apache-2.0

package workflow

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// DefaultReviewInterval applies when neither the control nor its catalog sets one.
const DefaultReviewInterval = "P365D"

// Period is an ISO 8601 period of years, months, weeks and days.
type Period struct{ Years, Months, Weeks, Days int }

var periodPattern = regexp.MustCompile(`^P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)W)?(?:(\d+)D)?$`)

// ParseDuration parses a period such as P1Y, P6M2W or P365D. Time components
// (PT...) are not supported: review intervals are counted in calendar days.
func ParseDuration(s string) (Period, error) {
	m := periodPattern.FindStringSubmatch(s)
	if m == nil || s == "P" {
		return Period{}, fmt.Errorf("invalid review interval %q: want an ISO 8601 period such as P365D or P1Y", s)
	}
	n := make([]int, 4)
	for i := range n {
		if m[i+1] != "" {
			v, err := strconv.Atoi(m[i+1])
			if err != nil {
				return Period{}, fmt.Errorf("invalid review interval %q: %w", s, err)
			}
			n[i] = v
		}
	}
	p := Period{Years: n[0], Months: n[1], Weeks: n[2], Days: n[3]}
	if p == (Period{}) {
		return Period{}, fmt.Errorf("invalid review interval %q: must not be zero", s)
	}
	return p, nil
}

// AddTo returns t plus the period, in calendar terms.
func (p Period) AddTo(t time.Time) time.Time {
	return t.AddDate(p.Years, p.Months, 7*p.Weeks+p.Days)
}
