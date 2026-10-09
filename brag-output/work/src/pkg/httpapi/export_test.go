// SPDX-License-Identifier: Apache-2.0

package httpapi

import "time"

// SetClockForTest replaces the rate-limit clock and returns a restore function.
func SetClockForTest(f func() time.Time) func() {
	prev := now
	now = f
	return func() { now = prev }
}
