// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

func (s *server) postEvidence(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	var in compliance.EvidenceInput
	if err := decodeStrict(r, &in, false); err != nil {
		return err
	}
	ev, err := s.eng.AddEvidence(r.Context(), p.Scope, in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, ev)
	return nil
}

func (s *server) listEvidence(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	q := store.EvidenceQuery{Catalog: r.URL.Query().Get("catalog"), ControlID: r.URL.Query().Get("control")}
	evs, err := s.eng.ListEvidence(r.Context(), p.Scope, q)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"evidence": evs})
	return nil
}

func (s *server) getEvidence(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	ev, err := s.eng.Evidence(r.Context(), p.Scope, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ev)
	return nil
}

func (s *server) postEvidenceLink(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	var ref store.ControlRef
	if err := decodeStrict(r, &ref, false); err != nil {
		return err
	}
	ev, err := s.eng.LinkEvidence(r.Context(), p.Scope, r.PathValue("id"), ref)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ev)
	return nil
}

func (s *server) deleteEvidenceLink(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	ref := store.ControlRef{Catalog: r.PathValue("catalog"), ControlID: r.PathValue("control")}
	ev, err := s.eng.UnlinkEvidence(r.Context(), p.Scope, r.PathValue("id"), ref)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ev)
	return nil
}

func (s *server) postEvidenceRevoke(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decodeStrict(r, &body, false); err != nil {
		return err
	}
	ev, err := s.eng.RevokeEvidence(r.Context(), p.Scope, r.PathValue("id"), body.Reason)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ev)
	return nil
}

func (s *server) postEvidenceVerify(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	ev, err := s.eng.VerifyEvidence(r.Context(), p.Scope, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ev)
	return nil
}

func (s *server) postEvidenceCheck(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	var body struct {
		Checksum string `json:"checksum"`
	}
	if err := decodeStrict(r, &body, false); err != nil {
		return err
	}
	ev, err := s.eng.AttestEvidenceCheck(r.Context(), p.Scope, r.PathValue("id"), body.Checksum)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ev)
	return nil
}

func (s *server) listAssessments(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	as, err := s.eng.Assessments(r.Context(), p.Scope, r.URL.Query().Get("catalog"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"assessments": as})
	return nil
}

func (s *server) getAssessment(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	v, err := s.eng.Assessment(r.Context(), p.Scope, r.PathValue("catalog"), r.PathValue("control"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}

func (s *server) putAssessment(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	var in compliance.AssignInput
	if err := decodeStrict(r, &in, false); err != nil {
		return err
	}
	a, err := s.eng.Assign(r.Context(), p.Scope, r.PathValue("catalog"), r.PathValue("control"), in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, a)
	return nil
}

// act serves one workflow action route.
func (s *server) act(action workflow.Action) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
		var in compliance.ActInput
		if err := decodeStrict(r, &in, true); err != nil {
			return err
		}
		a, err := s.eng.Act(r.Context(), p.Scope, r.PathValue("catalog"), r.PathValue("control"), action, in)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, a)
		return nil
	}
}

func (s *server) getStatus(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	var refs []catalog.Ref
	if raw := r.URL.Query().Get("catalogs"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			ref, err := refParam(strings.TrimSpace(part), "catalogs")
			if err != nil {
				return err
			}
			refs = append(refs, ref)
		}
	}
	res, err := s.eng.Status(r.Context(), p.Scope, refs)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *server) getSettings(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	st, err := s.eng.Settings(r.Context(), p.Scope)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, st)
	return nil
}

func (s *server) putSettings(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	var in compliance.SettingsInput
	if err := decodeStrict(r, &in, false); err != nil {
		return err
	}
	st, err := s.eng.UpdateSettings(r.Context(), p.Scope, in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, st)
	return nil
}

func (s *server) postEvidenceUpload(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	mr, err := r.MultipartReader()
	if err != nil {
		return newError(http.StatusBadRequest, "invalid_request", "expected a multipart/form-data body with the evidence fields and a file part")
	}
	in := compliance.EvidenceInput{Links: []store.ControlRef{}}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return newError(http.StatusBadRequest, "invalid_request", "the file part is missing (it must come after the other fields)")
		}
		if err != nil {
			return badJSON(err)
		}
		name := part.FormName()
		if name == "file" {
			ev, err := s.eng.UploadEvidence(r.Context(), p.Scope, in, part)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusCreated, ev)
			return nil
		}
		raw, err := io.ReadAll(io.LimitReader(part, 64<<10))
		if err != nil {
			return badJSON(err)
		}
		v := strings.TrimSpace(string(raw))
		switch name {
		case "title":
			in.Title = v
		case "kind":
			in.Kind = v
		case "source":
			in.Source = v
		case "valid_until":
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return newError(http.StatusUnprocessableEntity, "invalid_request", "valid_until must be an RFC 3339 time")
			}
			in.ValidUntil = &t
		case "retention_min_days":
			n, err := strconv.Atoi(v)
			if err != nil {
				return newError(http.StatusUnprocessableEntity, "invalid_request", "retention_min_days must be an integer")
			}
			in.Retention.MinDays = n
		case "retention_basis":
			in.Retention.Basis = v
		case "link":
			c, ctl, ok := strings.Cut(v, "/")
			if !ok {
				return newError(http.StatusUnprocessableEntity, "invalid_request", "link must be <catalog>/<control>")
			}
			in.Links = append(in.Links, store.ControlRef{Catalog: c, ControlID: ctl})
		default:
			return newError(http.StatusBadRequest, "invalid_request", "unknown form field "+name)
		}
	}
}

func (s *server) getEvidenceContent(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	data, ev, err := s.eng.EvidenceContent(r.Context(), p.Scope, r.PathValue("id"))
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ev.ID+`.bin"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Digest", "sha-256="+ev.Checksum)
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(data)
	return err
}
