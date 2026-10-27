// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/evidence"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/importer"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

type apiError struct {
	Status  int                 `json:"-"`
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Details []schema.FieldError `json:"details,omitempty"`
	Feature extension.Feature   `json:"feature,omitempty"`
	Reason  extension.Reason    `json:"reason,omitempty"`
	// Permission is the role permission a forbidden request lacked.
	Permission access.Permission `json:"permission,omitempty"`
	// Findings are the blocking findings of a refused incomplete export.
	Findings   []extension.Finding `json:"findings,omitempty"`
	retryAfter time.Duration
}

func (e *apiError) Error() string { return e.Message }

func newError(status int, code, message string) *apiError {
	return &apiError{Status: status, Code: code, Message: message}
}

var errUnauthorized = newError(http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")

func forbidden(p access.Permission) *apiError {
	return &apiError{Status: http.StatusForbidden, Code: "forbidden", Permission: p,
		Message: fmt.Sprintf("the caller's roles do not grant %s", p)}
}

func rateLimited(wait time.Duration) *apiError {
	return &apiError{Status: http.StatusTooManyRequests, Code: "rate_limited", retryAfter: wait,
		Message: "too many requests; retry after the delay in the Retry-After header"}
}

// classify maps an error to its API form; nil means an unexpected failure.
func classify(err error) *apiError {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae
	}
	var be *compliance.BatchError
	if errors.As(err, &be) {
		return &apiError{Status: http.StatusUnprocessableEntity, Code: "invalid_batch", Message: "the batch envelope is invalid", Details: be.Errors}
	}
	var fe *importer.FileError
	if errors.As(err, &fe) {
		return &apiError{Status: http.StatusUnprocessableEntity, Code: "invalid_file", Message: fe.Error(), Details: fe.Errors}
	}
	var ne *extension.NotEntitledError
	if errors.As(err, &ne) {
		return &apiError{Status: http.StatusForbidden, Code: "feature_not_entitled", Message: ne.Error(), Feature: ne.Feature, Reason: ne.Reason}
	}
	var ie *compliance.ExportIncompleteError
	if errors.As(err, &ie) {
		return &apiError{Status: http.StatusUnprocessableEntity, Code: "export_incomplete", Message: ie.Error(), Findings: ie.Findings}
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return newError(http.StatusRequestEntityTooLarge, "payload_too_large", fmt.Sprintf("request body exceeds %d bytes", mbe.Limit))
	}
	for _, m := range []struct {
		target error
		status int
		code   string
	}{
		{identity.ErrForbidden, http.StatusForbidden, "forbidden"},
		{identity.ErrRoleNotHeld, http.StatusForbidden, "role_not_held"},
		{identity.ErrServiceRole, http.StatusForbidden, "role_not_held"},
		{identity.ErrLastAdmin, http.StatusConflict, "last_admin"},
		{identity.ErrInvalidEmail, http.StatusUnprocessableEntity, "invalid_request"},
		{identity.ErrInvalidRequest, http.StatusUnprocessableEntity, "invalid_request"},
		{store.ErrExists, http.StatusConflict, "already_exists"},
		{workflow.ErrInvalidTransition, http.StatusConflict, "invalid_transition"},
		{compliance.ErrStaleEvaluation, http.StatusConflict, "stale_evaluation"},
		{workflow.ErrSelfApproval, http.StatusConflict, "self_approval_forbidden"},
		{workflow.ErrEvidenceRequired, http.StatusConflict, "evidence_required"},
		{workflow.ErrRecommendationRequired, http.StatusConflict, "recommendation_required"},
		{workflow.ErrSubmitterApproval, http.StatusConflict, "submitter_approval_forbidden"},
		{workflow.ErrAlreadyApproved, http.StatusConflict, "already_approved"},
		{workflow.ErrNotOwner, http.StatusForbidden, "not_control_owner"},
		{workflow.ErrReasonRequired, http.StatusUnprocessableEntity, "invalid_request"},
		{compliance.ErrReasonRequired, http.StatusUnprocessableEntity, "invalid_request"},
		{compliance.ErrInvalidAssignee, http.StatusUnprocessableEntity, "invalid_request"},
		{evidence.ErrInvalid, http.StatusUnprocessableEntity, "invalid_request"},
		{evidence.ErrCannotVerifyHere, http.StatusUnprocessableEntity, "cannot_verify_here"},
		{compliance.ErrAlreadyRevoked, http.StatusConflict, "already_revoked"},
		{compliance.ErrNoChecksum, http.StatusConflict, "no_checksum"},
		{compliance.ErrUnknownControl, http.StatusNotFound, "not_found"},
		{compliance.ErrInvalidSettings, http.StatusUnprocessableEntity, "invalid_request"},
		{evidence.ErrManagedNotConfigured, http.StatusNotImplemented, "managed_storage_not_configured"},
		{evidence.ErrNotManaged, http.StatusConflict, "not_managed"},
		{compliance.ErrUnknownProfile, http.StatusNotFound, "not_found"},
		{compliance.ErrInvalidReportRequest, http.StatusUnprocessableEntity, "invalid_request"},
		{extension.ErrInvalidParameter, http.StatusUnprocessableEntity, "invalid_parameter"},
		{compliance.ErrWorkspaceSuspended, http.StatusForbidden, "workspace_suspended"},
		{store.ErrWorkspaceLimit, http.StatusForbidden, "workspace_limit_reached"},
		{compliance.ErrReservedTenant, http.StatusUnprocessableEntity, "invalid_request"},
		{compliance.ErrWorkspaceExists, http.StatusConflict, "already_exists"},
		{compliance.ErrEvidenceRetention, http.StatusConflict, "evidence_retention"},
	} {
		if errors.Is(err, m.target) {
			return newError(m.status, m.code, err.Error())
		}
	}
	type mapping struct {
		target error
		status int
		code   string
	}
	for _, m := range []mapping{
		// ErrInvalidManifest first: a manifest error can wrap ErrUnsupportedVersion too.
		{compliance.ErrInvalidManifest, http.StatusUnprocessableEntity, "invalid_manifest"},
		{compliance.ErrTooLarge, http.StatusRequestEntityTooLarge, "too_many_records"},
		{schema.ErrUnsupportedVersion, http.StatusUnprocessableEntity, "unsupported_schema_version"},
		{compliance.ErrUnknownAdapter, http.StatusUnprocessableEntity, "unknown_adapter"},
		{compliance.ErrModeNotSupported, http.StatusUnprocessableEntity, "mode_not_supported"},
		{store.ErrNotFound, http.StatusNotFound, "not_found"},
		{catalog.ErrUnknownCatalog, http.StatusNotFound, "not_found"},
		{store.ErrConflict, http.StatusConflict, "conflict"},
		{compliance.ErrAlreadyRolledBack, http.StatusConflict, "already_rolled_back"},
		{compliance.ErrNothingToRollBack, http.StatusConflict, "nothing_to_roll_back"},
	} {
		if errors.Is(err, m.target) {
			return newError(m.status, m.code, err.Error())
		}
	}
	return nil
}

func (s *server) writeError(w http.ResponseWriter, err error) {
	ae := classify(err)
	if ae == nil {
		s.opts.Logger.Error("request failed", "error", err)
		ae = newError(http.StatusInternalServerError, "internal", "internal error")
	}
	if ae.Status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="compliance-engine"`)
	}
	if ae.Status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", retryAfterSeconds(ae.retryAfter))
	}
	writeJSON(w, ae.Status, map[string]any{"error": ae})
}

// badJSON classifies a body decoding error, keeping size-limit errors intact.
func badJSON(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return err
	}
	return newError(http.StatusBadRequest, "invalid_json", err.Error())
}

// decodeStrict decodes a JSON body, rejecting unknown fields. An empty body
// decodes to the zero value when allowEmpty is true.
func decodeStrict(r *http.Request, v any, allowEmpty bool) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return nil
		}
		return badJSON(err)
	}
	return nil
}
