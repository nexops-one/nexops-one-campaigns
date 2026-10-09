// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// Problem is the API form of an error, shared with other front ends (the
// console) so that both answer the same failure with the same status and code.
type Problem struct {
	Status     int
	Code       string
	Message    string
	Details    []schema.FieldError
	Findings   []extension.Finding
	Feature    extension.Feature
	Permission access.Permission
}

// Describe returns the API form of err; false means an unexpected failure
// (500 internal), whose message must not be shown.
func Describe(err error) (Problem, bool) {
	ae := classify(err)
	if ae == nil {
		return Problem{}, false
	}
	return Problem{Status: ae.Status, Code: ae.Code, Message: ae.Message, Details: ae.Details, Findings: ae.Findings,
		Feature: ae.Feature, Permission: ae.Permission}, true
}

// Forbidden is the problem of a principal lacking p.
func Forbidden(p access.Permission) Problem {
	ae := forbidden(p)
	return Problem{Status: ae.Status, Code: ae.Code, Message: ae.Message, Permission: p}
}

// FailureLimiter counts failed authentications per client address. One
// limiter can be shared by the API and the console sign-in, so that both draw
// on the same budget.
type FailureLimiter struct{ l *limiter }

// NewFailureLimiter allows perMinute failures per address; 0 disables it.
func NewFailureLimiter(perMinute int) *FailureLimiter {
	return &FailureLimiter{l: newLimiter(perMinute, perMinute)}
}

// Blocked reports whether the request's address has used up its budget, and
// how long to wait.
func (f *FailureLimiter) Blocked(r *http.Request) (time.Duration, bool) {
	if f == nil {
		return 0, false
	}
	return f.l.blocked(clientIP(r))
}

// Fail records a failed authentication from the request's address.
func (f *FailureLimiter) Fail(r *http.Request) {
	if f != nil {
		f.l.take(clientIP(r))
	}
}

// RetryAfter formats a wait as Retry-After seconds (at least 1).
func RetryAfter(d time.Duration) string { return retryAfterSeconds(d) }

// NewProblem returns an error the API answers with status and code, for
// handlers of an edition's extra routes.
func NewProblem(status int, code, message string) error { return newError(status, code, message) }

// WriteJSON writes v as a JSON response, as the API does.
func WriteJSON(w http.ResponseWriter, status int, v any) { writeJSON(w, status, v) }

// DecodeJSON decodes a request body strictly (unknown fields are refused);
// errors are answered as the API answers malformed bodies.
func DecodeJSON(r *http.Request, v any) error { return decodeStrict(r, v, false) }

// WriteError answers err in the API's error shape. Unexpected errors are
// logged and answered 500 without their message.
func WriteError(w http.ResponseWriter, err error, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	(&server{opts: Options{Logger: logger}}).writeError(w, err)
}
