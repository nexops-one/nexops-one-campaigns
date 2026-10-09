// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"mime"
	"net/http"
	"strconv"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
)

type reportRequest struct {
	Profile      string   `json:"profile"`
	EvaluationID string   `json:"evaluation_id"`
	SnapshotID   string   `json:"snapshot_id"`
	Catalogs     []string `json:"catalogs"`
	Formats      []string `json:"formats"`
	// Parameters and AllowIncomplete go to the profile (see compliance.ReportRequest).
	Parameters      map[string]string `json:"parameters"`
	AllowIncomplete bool              `json:"allow_incomplete"`
}

func (s *server) listReportProfiles(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	profiles, err := s.eng.ReportProfiles(r.Context(), p.Scope)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
	return nil
}

func (s *server) postReport(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	var req reportRequest
	if err := decodeStrict(r, &req, false); err != nil {
		return err
	}
	if req.Profile == "" {
		return newError(http.StatusUnprocessableEntity, "invalid_request", "profile is required (see GET /api/v1/report-profiles)")
	}
	var refs []catalog.Ref
	for _, raw := range req.Catalogs {
		ref, err := refParam(raw, "catalogs")
		if err != nil {
			return err
		}
		refs = append(refs, ref)
	}
	profiles, err := s.eng.ReportProfiles(r.Context(), p.Scope)
	if err != nil {
		return err
	}
	for _, pi := range profiles {
		if pi.ID == req.Profile {
			s.graceWarning(w, r, p, pi.Feature)
		}
	}
	rep, err := s.eng.GenerateReport(r.Context(), p.Scope, compliance.ReportRequest{
		Profile: req.Profile, EvaluationID: req.EvaluationID, SnapshotID: req.SnapshotID, Catalogs: refs, Formats: req.Formats,
		Parameters: req.Parameters, AllowIncomplete: req.AllowIncomplete,
	})
	if err != nil {
		return err
	}
	rep.Inputs = nil
	writeJSON(w, http.StatusCreated, rep)
	return nil
}

func (s *server) listReports(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	reps, err := s.eng.Reports(r.Context(), p.Scope)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reps})
	return nil
}

func (s *server) getReport(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	rep, err := s.eng.Report(r.Context(), p.Scope, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rep)
	return nil
}

// getReportFile serves a rendering byte for byte; report.json is the exact
// document the facts hash is computed over.
func (s *server) getReportFile(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	f, err := s.eng.ReportFile(r.Context(), p.Scope, r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", f.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": r.PathValue("id") + "-" + f.Name}))
	w.Header().Set("Content-Length", strconv.Itoa(len(f.Data)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Digest", "sha-256="+f.SHA256)
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(f.Data)
	return err
}

func (s *server) postReportRegenerate(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	res, err := s.eng.RegenerateReport(r.Context(), p.Scope, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}
