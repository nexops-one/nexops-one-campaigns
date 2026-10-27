// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/nexops-one/compliance-engine/api"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

type aboutResponse struct {
	Edition        string        `json:"edition"`
	Version        string        `json:"version"`
	SchemaVersions []string      `json:"schema_versions"`
	Catalogs       []catalog.Ref `json:"catalogs"`
	TenantID       string        `json:"tenant_id"`
	WorkspaceID    string        `json:"workspace_id"`
	Encryption     bool          `json:"encryption_at_rest"`
}

func (s *server) healthz(w http.ResponseWriter, _ *http.Request, _ extension.Principal) error {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}

func (s *server) openAPI(w http.ResponseWriter, _ *http.Request, _ extension.Principal) error {
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(api.OpenAPI)
	return err
}

func (s *server) about(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	resp := aboutResponse{
		Edition: s.opts.Edition, Version: s.opts.Version, SchemaVersions: s.eng.SchemaVersions(),
		Catalogs: s.eng.Catalogs(), TenantID: p.Scope.TenantID, WorkspaceID: p.Scope.WorkspaceID, Encryption: s.opts.EncryptionAtRest,
	}
	if len(s.opts.About) == 0 {
		writeJSON(w, http.StatusOK, resp)
		return nil
	}
	// Sections are added beside the fixed fields and never replace them.
	var out map[string]any
	if err := json.Unmarshal(mustMarshal(resp), &out); err != nil {
		return err
	}
	for _, sec := range s.opts.About {
		if k := sec.AboutKey(); k != "" {
			if _, fixed := out[k]; !fixed {
				out[k] = sec.About(r.Context(), p.Scope)
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err) // plain data only
	}
	return data
}

func (s *server) putManifest(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	var m adapter.Manifest
	if err := decodeStrict(r, &m, false); err != nil {
		return err
	}
	if m.Name != r.PathValue("name") {
		return newError(http.StatusUnprocessableEntity, "name_mismatch", "the manifest name must match the adapter name in the path")
	}
	if err := s.eng.RegisterManifest(r.Context(), p.Scope, m); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, m)
	return nil
}

func boolParam(r *http.Request, name string) (bool, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, newError(http.StatusBadRequest, "invalid_parameter", name+" must be true or false")
	}
	return b, nil
}

func (s *server) postIngestion(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	dry, err := boolParam(r, "dry_run")
	if err != nil {
		return err
	}
	b, err := adapter.DecodeBatch(r.Body)
	if err != nil {
		return badJSON(err)
	}
	if dry {
		res, err := s.eng.DryRun(r.Context(), p.Scope, b)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, res)
		return nil
	}
	res, err := s.eng.Ingest(r.Context(), p.Scope, b)
	if err != nil {
		return err
	}
	status := http.StatusCreated
	if res.NoChanges {
		status = http.StatusOK
	}
	writeJSON(w, status, res)
	return nil
}

func (s *server) getIngestion(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	ing, err := s.eng.Ingestion(r.Context(), p.Scope, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ing)
	return nil
}

func (s *server) rollback(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	res, err := s.eng.Rollback(r.Context(), p.Scope, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, res)
	return nil
}
