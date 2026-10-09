// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// snapshotParam maps the {id} path value; "current" means the latest snapshot.
func snapshotParam(r *http.Request) string {
	if id := r.PathValue("id"); id != "current" {
		return id
	}
	return ""
}

type snapshotSummary struct {
	ID       string         `json:"id"`
	Revision store.Revision `json:"revision"`
	Records  int            `json:"records"`
	Entities map[string]int `json:"entities"`
}

type recordView struct {
	Key             string                          `json:"key"`
	Data            adapter.Record                  `json:"data"`
	Hash            string                          `json:"hash"`
	Source          adapter.Source                  `json:"source"`
	IngestionID     string                          `json:"ingestion_id"`
	SourceRecordRef string                          `json:"source_record_ref,omitempty"`
	WrittenAt       time.Time                       `json:"written_at"`
	FieldStates     map[string]canonical.FieldState `json:"field_states"`
}

type catalogSummary struct {
	Catalog       string `json:"catalog"`
	Version       string `json:"version"`
	Framework     string `json:"framework"`
	Jurisdiction  string `json:"jurisdiction"`
	EffectiveDate string `json:"effective_date"`
	SchemaVersion string `json:"schema_version"`
	Controls      int    `json:"controls"`
	Feature       string `json:"feature,omitempty"`
	Origin        string `json:"origin,omitempty"`
	Entitled      bool   `json:"entitled"`
}

type evaluationRequest struct {
	SnapshotID string   `json:"snapshot_id"`
	Catalogs   []string `json:"catalogs"`
}

func (s *server) listSnapshots(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	revs, err := s.eng.Snapshots(r.Context(), p.Scope)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": revs})
	return nil
}

func (s *server) getSnapshot(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	snap, err := s.eng.Snapshot(r.Context(), p.Scope, snapshotParam(r))
	if err != nil {
		return err
	}
	sum := snapshotSummary{ID: snap.ID, Revision: snap.Revision, Records: len(snap.Records), Entities: map[string]int{}}
	for _, v := range snap.Records {
		sum.Entities[v.Entity]++
	}
	writeJSON(w, http.StatusOK, sum)
	return nil
}

func (s *server) getRecords(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	entity := r.PathValue("entity")
	e, ok := s.eng.Schema().Entity(entity)
	if !ok {
		return newError(http.StatusNotFound, "not_found", fmt.Sprintf("entity %q is not defined in canonical schema %s", entity, s.eng.Schema().Version))
	}
	snap, err := s.eng.Snapshot(r.Context(), p.Scope, snapshotParam(r))
	if err != nil {
		return err
	}
	out := []recordView{}
	for _, v := range snap.Records {
		if v.Entity != entity {
			continue
		}
		view := canonical.NewView(v.Data)
		states := map[string]canonical.FieldState{}
		for _, f := range e.FieldNames {
			states[f] = view.State(f)
		}
		out = append(out, recordView{Key: v.Key, Data: v.Data, Hash: v.Hash, Source: v.Source, IngestionID: v.IngestionID,
			SourceRecordRef: v.SourceRecordRef, WrittenAt: v.WrittenAt, FieldStates: states})
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot_id": snap.ID, "entity": entity, "records": out})
	return nil
}

func (s *server) getCompleteness(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	c, err := s.eng.Completeness(r.Context(), p.Scope, snapshotParam(r))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, c)
	return nil
}

func (s *server) getProvenance(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	q := r.URL.Query()
	entity, key := q.Get("entity"), q.Get("key")
	if entity == "" || key == "" {
		return newError(http.StatusBadRequest, "missing_parameter", "entity and key are required")
	}
	entries, err := s.eng.Provenance(r.Context(), p.Scope, entity, key)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	return nil
}

func (s *server) listCatalogs(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	out := []catalogSummary{}
	for _, info := range s.eng.CatalogInfos(r.Context(), p.Scope) {
		c, err := s.eng.Catalog(info.Ref)
		if err != nil {
			return err
		}
		out = append(out, catalogSummary{Catalog: c.Catalog, Version: c.Version, Framework: c.Framework, Jurisdiction: c.Jurisdiction,
			EffectiveDate: c.EffectiveDate, SchemaVersion: c.SchemaVersion, Controls: len(c.Controls),
			Feature: info.Feature, Origin: info.Origin, Entitled: info.Entitled})
	}
	writeJSON(w, http.StatusOK, map[string]any{"catalogs": out})
	return nil
}

func (s *server) getCatalog(w http.ResponseWriter, r *http.Request, _ extension.Principal) error {
	c, err := s.eng.Catalog(catalog.Ref{Catalog: r.PathValue("catalog"), Version: r.PathValue("version")})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, c)
	return nil
}

func refParam(value, name string) (catalog.Ref, error) {
	ref, err := catalog.ParseRef(value)
	if err != nil {
		return catalog.Ref{}, newError(http.StatusBadRequest, "invalid_parameter", fmt.Sprintf("%s: %v", name, err))
	}
	return ref, nil
}

func (s *server) diffCatalogs(w http.ResponseWriter, r *http.Request, _ extension.Principal) error {
	a, err := refParam(r.URL.Query().Get("a"), "a")
	if err != nil {
		return err
	}
	b, err := refParam(r.URL.Query().Get("b"), "b")
	if err != nil {
		return err
	}
	d, err := s.eng.CatalogDiff(a, b)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, d)
	return nil
}

func (s *server) postEvaluation(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	var req evaluationRequest
	if err := decodeStrict(r, &req, true); err != nil {
		return err
	}
	var refs []catalog.Ref
	for _, raw := range req.Catalogs {
		ref, err := refParam(raw, "catalogs")
		if err != nil {
			return err
		}
		refs = append(refs, ref)
	}
	ev, err := s.eng.Evaluate(r.Context(), p.Scope, req.SnapshotID, refs)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, ev)
	return nil
}

func (s *server) getEvaluation(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	ev, err := s.eng.Evaluation(r.Context(), p.Scope, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ev)
	return nil
}
