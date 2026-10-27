// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/importer"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func (s *server) postImport(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	dry, err := boolParam(r, "dry_run")
	if err != nil {
		return err
	}
	mode := adapter.Mode(r.URL.Query().Get("mode"))
	if mode != "" && mode != adapter.ModeFull && mode != adapter.ModeIncremental {
		return newError(http.StatusBadRequest, "invalid_parameter", "mode must be full or incremental")
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	file, header, err := r.FormFile("file")
	if err != nil {
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			return err
		case errors.Is(err, http.ErrMissingFile):
			return newError(http.StatusBadRequest, "missing_parameter", `the multipart field "file" is required`)
		}
		return newError(http.StatusBadRequest, "invalid_parameter", `the request must be multipart/form-data with a "file" field`)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	res, err := s.eng.Import(r.Context(), p.Scope, importer.Input{
		Name: header.Filename, Data: data, Entity: r.URL.Query().Get("entity"), Mode: mode,
	}, dry)
	if err != nil {
		return err
	}
	status := http.StatusCreated
	if dry || res.Result.NoChanges {
		status = http.StatusOK
	}
	writeJSON(w, status, res)
	return nil
}

func writeFile(w http.ResponseWriter, contentType, filename string, data []byte) error {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(data)
	return err
}

func (s *server) templateWorkbook(w http.ResponseWriter, _ *http.Request, _ extension.Principal) error {
	data, err := importer.XLSXTemplate(s.eng.Schema())
	if err != nil {
		return err
	}
	return writeFile(w, xlsxContentType, fmt.Sprintf("compliance-templates-%s.xlsx", s.eng.Schema().Version), data)
}

func (s *server) templateCSV(w http.ResponseWriter, r *http.Request, _ extension.Principal) error {
	name := r.PathValue("file")
	entity, ok := strings.CutSuffix(name, ".csv")
	if !ok {
		return newError(http.StatusNotFound, "not_found", "templates are served as <entity>.csv")
	}
	data, err := importer.CSVTemplate(s.eng.Schema(), entity)
	if err != nil {
		return newError(http.StatusNotFound, "not_found", err.Error())
	}
	return writeFile(w, "text/csv; charset=utf-8", name, data)
}
