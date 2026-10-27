// SPDX-License-Identifier: Apache-2.0

package console

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/importer"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// recentImports is how many ingestions the import page lists.
const recentImports = 20

func download(w http.ResponseWriter, contentType, filename string, data []byte) error {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "no-store")
	_, err := w.Write(data)
	return err
}

type ingestionRow struct {
	Revision  store.Revision
	Ingestion store.Ingestion
	Result    map[string]any
	CanRoll   bool
}

type importPage struct {
	Entities  []string
	Sample    bool
	Ingestion []ingestionRow
}

func (c *Console) importPage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	revs, err := c.eng.Snapshots(r.Context(), rc.p.Scope)
	if err != nil {
		return err
	}
	page := importPage{Entities: c.eng.Schema().EntityNames(), Sample: rc.sample && c.opts.SampleRegister != nil}
	for i := len(revs) - 1; i >= 0 && len(page.Ingestion) < recentImports; i-- {
		ing, err := c.eng.Ingestion(r.Context(), rc.p.Scope, revs[i].IngestionID)
		if err != nil {
			return err
		}
		row := ingestionRow{Revision: revs[i], Ingestion: ing}
		_ = json.Unmarshal(ing.Result, &row.Result)
		row.CanRoll = revs[i].Kind == store.KindIngestion && ing.RolledBackBy == ""
		page.Ingestion = append(page.Ingestion, row)
	}
	c.render(w, r, rc, "import", "Import", http.StatusOK, page)
	return nil
}

func (c *Console) templateWorkbook(w http.ResponseWriter, _ *http.Request, _ *reqCtx) error {
	data, err := importer.XLSXTemplate(c.eng.Schema())
	if err != nil {
		return err
	}
	return download(w, xlsxContentType, fmt.Sprintf("compliance-templates-%s.xlsx", c.eng.Schema().Version), data)
}

func (c *Console) templateCSV(w http.ResponseWriter, r *http.Request, _ *reqCtx) error {
	name := r.PathValue("file")
	entity, ok := strings.CutSuffix(name, ".csv")
	if !ok {
		return problem(http.StatusNotFound, "not_found", "Templates are served as <entity>.csv.")
	}
	data, err := importer.CSVTemplate(c.eng.Schema(), entity)
	if err != nil {
		return problem(http.StatusNotFound, "not_found", err.Error())
	}
	return download(w, "text/csv; charset=utf-8", name, data)
}

func (c *Console) sampleRegister(w http.ResponseWriter, _ *http.Request, rc *reqCtx) error {
	if !rc.sample || c.opts.SampleRegister == nil {
		return problem(http.StatusNotFound, "not_found", "The sample register is offered in sample workspaces only.")
	}
	return download(w, xlsxContentType, "sample-register.xlsx", c.opts.SampleRegister)
}

type importReport struct {
	Pending string
	Mode    string
	Result  compliance.ImportResult
	// Gaps groups the would-be snapshot's export-required gaps by entity and field.
	Gaps []gapGroup
}

// readUpload reads the import form into an importer input.
func readUpload(r *http.Request) (importer.Input, error) {
	mode := adapter.Mode(r.PostFormValue("mode"))
	if mode != "" && mode != adapter.ModeFull && mode != adapter.ModeIncremental {
		return importer.Input{}, problem(http.StatusBadRequest, "invalid_parameter", "The mode must be full or incremental.")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) || errors.Is(err, http.ErrNotMultipart) {
			return importer.Input{}, problem(http.StatusBadRequest, "missing_parameter", "Choose a .xlsx or .csv file to import.")
		}
		return importer.Input{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return importer.Input{}, err
	}
	return importer.Input{Name: header.Filename, Data: data, Entity: r.PostFormValue("entity"), Mode: mode}, nil
}

func (c *Console) validateImport(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	in, err := readUpload(r)
	if err != nil {
		return err
	}
	res, err := c.eng.Import(r.Context(), rc.p.Scope, in, true)
	if err != nil {
		return err
	}
	rep := importReport{Result: res, Mode: string(in.Mode), Pending: c.pending.put(rc.session.Hash, in)}
	if res.Completeness != nil {
		rep.Gaps = groupGaps(res.Completeness.Gaps)
	}
	c.render(w, r, rc, "import_report", "Import report", http.StatusOK, rep)
	return nil
}

func (c *Console) commitImport(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	in, ok := c.pending.take(rc.session.Hash, r.PostFormValue("pending"))
	if !ok {
		return problem(http.StatusConflict, "import_expired", "This validated import is no longer available (it expires after 30 minutes, or was already committed). Upload the file again.")
	}
	res, err := c.eng.Import(r.Context(), rc.p.Scope, in, false)
	if err != nil {
		return err
	}
	http.Redirect(w, r, Prefix+"/import?ok=imported&ingestion="+url.QueryEscape(res.Result.IngestionID), http.StatusSeeOther)
	return nil
}

func (c *Console) rollback(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	if _, err := c.eng.Rollback(r.Context(), rc.p.Scope, r.PathValue("id")); err != nil {
		return err
	}
	http.Redirect(w, r, Prefix+"/import?ok=rolled_back", http.StatusSeeOther)
	return nil
}

type entityCount struct {
	Entity  string
	Records int
}

type recordsPage struct {
	Snapshot compliance.Snapshot
	Entities []entityCount
}

func (c *Console) recordsPage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	snap, err := c.eng.Snapshot(r.Context(), rc.p.Scope, "")
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, v := range snap.Records {
		counts[v.Entity]++
	}
	page := recordsPage{Snapshot: compliance.Snapshot{ID: snap.ID, Revision: snap.Revision}}
	for _, e := range c.eng.Schema().EntityNames() {
		page.Entities = append(page.Entities, entityCount{Entity: e, Records: counts[e]})
	}
	c.render(w, r, rc, "records", "Records", http.StatusOK, page)
	return nil
}

type stateCounts struct{ Provided, Derived, NotApplicable, Missing int }

type recordRow struct {
	Key     string
	Version store.RecordVersion
	States  stateCounts
}

type entityPage struct {
	Entity     string
	SnapshotID string
	Records    []recordRow
}

func (c *Console) entityPage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	name := r.PathValue("entity")
	e, ok := c.eng.Schema().Entity(name)
	if !ok {
		return problem(http.StatusNotFound, "not_found", fmt.Sprintf("%q is not an entity of the canonical schema.", name))
	}
	snap, err := c.eng.Snapshot(r.Context(), rc.p.Scope, "")
	if err != nil {
		return err
	}
	page := entityPage{Entity: name, SnapshotID: snap.ID}
	for _, v := range snap.Records {
		if v.Entity != name {
			continue
		}
		row := recordRow{Key: v.Key, Version: v}
		view := canonical.NewView(v.Data)
		for _, f := range e.FieldNames {
			switch view.State(f) {
			case canonical.StateProvided:
				row.States.Provided++
			case canonical.StateDerived:
				row.States.Derived++
			case canonical.StateNotApplicable:
				row.States.NotApplicable++
			default:
				row.States.Missing++
			}
		}
		page.Records = append(page.Records, row)
	}
	c.render(w, r, rc, "entity", name, http.StatusOK, page)
	return nil
}

type fieldRow struct {
	Name, RoIRef, Value, Derivation string
	State                           canonical.FieldState
	Required, Identity              bool
}

type recordPage struct {
	Entity     string
	Key        string
	SnapshotID string
	Version    store.RecordVersion
	Fields     []fieldRow
	Provenance []store.ProvenanceEntry
}

func displayValue(v any) string {
	if s, ok := canonical.ScalarString(v); ok {
		return s
	}
	if v == nil {
		return ""
	}
	data, _ := json.Marshal(v)
	return string(data)
}

func (c *Console) recordPage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	name, key := r.PathValue("entity"), r.URL.Query().Get("key")
	e, ok := c.eng.Schema().Entity(name)
	if !ok {
		return problem(http.StatusNotFound, "not_found", fmt.Sprintf("%q is not an entity of the canonical schema.", name))
	}
	snap, err := c.eng.Snapshot(r.Context(), rc.p.Scope, "")
	if err != nil {
		return err
	}
	page := recordPage{Entity: name, Key: key, SnapshotID: snap.ID}
	found := false
	for _, v := range snap.Records {
		if v.Entity == name && v.Key == key {
			page.Version, found = v, true
		}
	}
	if found {
		view := canonical.NewView(page.Version.Data)
		for _, f := range e.FieldNames {
			fd := e.Fields[f]
			row := fieldRow{Name: f, RoIRef: fd.RoIRef, State: view.State(f), Value: displayValue(page.Version.Data[f]),
				Required: fd.RoIRequired, Identity: fd.IsIdentity()}
			if d, ok := view.Derived(f); ok {
				row.Derivation = d.Method
			}
			page.Fields = append(page.Fields, row)
		}
	}
	if page.Provenance, err = c.eng.Provenance(r.Context(), rc.p.Scope, name, key); err != nil {
		return err
	}
	if !found && len(page.Provenance) == 0 {
		return problem(http.StatusNotFound, "not_found", "There is no such record.")
	}
	c.render(w, r, rc, "record", name+" "+key, http.StatusOK, page)
	return nil
}

// gapGroup is the export-required gaps of one entity field.
type gapGroup struct {
	Entity, Field, RoIRef string
	Conditional, Supplied bool
	Missing, Derived      int
	Keys                  []string
}

func groupGaps(gaps []canonical.Gap) []gapGroup {
	idx := map[string]*gapGroup{}
	var order []string
	for _, g := range gaps {
		k := g.Entity + "\x00" + g.Field
		gg, ok := idx[k]
		if !ok {
			gg = &gapGroup{Entity: g.Entity, Field: g.Field, RoIRef: g.RoIRef, Conditional: g.Conditional, Supplied: g.Supplied}
			idx[k] = gg
			order = append(order, k)
		}
		if g.State == canonical.StateDerived {
			gg.Derived++
		} else {
			gg.Missing++
		}
		gg.Keys = append(gg.Keys, g.Key)
	}
	sort.Strings(order)
	out := make([]gapGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *idx[k])
	}
	return out
}

type completenessPage struct {
	Completeness  canonical.Completeness
	Supplied      []gapGroup
	NotSupplied   []gapGroup
	TotalRecords  int
	TotalMissing  int
	TotalDerived  int
	HasReferences bool
}

func (c *Console) completenessPage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	comp, err := c.eng.Completeness(r.Context(), rc.p.Scope, "")
	if err != nil {
		return err
	}
	page := completenessPage{Completeness: comp,
		HasReferences: len(comp.References.Dangling)+len(comp.References.Cycles) > 0}
	for _, g := range groupGaps(comp.Gaps) {
		if g.Supplied {
			page.Supplied = append(page.Supplied, g)
		} else {
			page.NotSupplied = append(page.NotSupplied, g)
		}
	}
	for _, e := range comp.Entities {
		page.TotalRecords += e.Records
		page.TotalMissing += e.Missing
		page.TotalDerived += e.Derived
	}
	c.render(w, r, rc, "completeness", "Completeness", http.StatusOK, page)
	return nil
}
