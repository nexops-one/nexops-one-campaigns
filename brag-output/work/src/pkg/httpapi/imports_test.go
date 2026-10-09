// SPDX-License-Identifier: Apache-2.0

package httpapi_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/nexops-one/compliance-engine/pkg/httpapi"
)

const providersCSV = "provider_id_code,legal_name,hq_country\nP1,One Ltd,IE\nP2,Two Inc,US\n"

// upload posts a multipart form; an empty filename sends a form without a file part.
func upload(t *testing.T, h http.Handler, path, token, filename string, data []byte) response {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if filename != "" {
		fw, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write(data)
	} else {
		mw.WriteField("note", "no file")
	}
	mw.Close()
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := response{Status: rec.Code, Header: rec.Header(), Raw: rec.Body.Bytes()}
	_ = json.Unmarshal(res.Raw, &res.Body)
	return res
}

func resultOf(r response) map[string]any {
	m, _ := r.Body["result"].(map[string]any)
	return m
}

func TestTemplates(t *testing.T) {
	h := newAPI(t, setup{})
	wb := call(t, h, "GET", "/api/v1/templates.xlsx", tokenA, nil)
	if wb.Status != 200 || wb.Header.Get("Content-Type") != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" ||
		!strings.Contains(wb.Header.Get("Content-Disposition"), "compliance-templates-0.1.0.xlsx") {
		t.Fatalf("workbook = %d %v", wb.Status, wb.Header)
	}
	f, err := excelize.OpenReader(bytes.NewReader(wb.Raw))
	if err != nil || f.GetSheetList()[0] != "README" {
		t.Fatalf("workbook content: %v", err)
	}
	csv := call(t, h, "GET", "/api/v1/templates/ict_provider.csv", tokenA, nil)
	if csv.Status != 200 || !strings.HasPrefix(string(csv.Raw), "provider_id_code,") || csv.Header.Get("Content-Type") != "text/csv; charset=utf-8" {
		t.Fatalf("csv = %d %q", csv.Status, csv.Raw)
	}
	for _, p := range []string{"/api/v1/templates/nope.csv", "/api/v1/templates/ict_provider.txt"} {
		if r := call(t, h, "GET", p, tokenA, nil); r.Status != 404 {
			t.Errorf("%s = %d", p, r.Status)
		}
	}
	if r := call(t, h, "GET", "/api/v1/templates.xlsx", "", nil); r.Status != 401 {
		t.Fatalf("templates require a token: %d", r.Status)
	}
}

func TestImportEndpoint(t *testing.T) {
	h := newAPI(t, setup{})
	dry := upload(t, h, "/api/v1/imports?dry_run=true", tokenA, "ict_provider.csv", []byte(providersCSV))
	if dry.Status != 200 || resultOf(dry)["dry_run"] != true || dry.Body["completeness"] == nil {
		t.Fatalf("dry run = %d %s", dry.Status, dry.Raw)
	}
	commit := upload(t, h, "/api/v1/imports", tokenA, "ict_provider.csv", []byte(providersCSV))
	if commit.Status != 201 || resultOf(commit)["snapshot_id"] != "rev-1" || commit.Body["adapter"] != "csv-import.ict_provider" {
		t.Fatalf("commit = %d %s", commit.Status, commit.Raw)
	}
	again := upload(t, h, "/api/v1/imports", tokenA, "ict_provider.csv", []byte(providersCSV))
	if again.Status != 200 || resultOf(again)["no_changes"] != true {
		t.Fatalf("again = %d %s", again.Status, again.Raw)
	}
	if r := call(t, h, "GET", "/api/v1/snapshots/current", tokenB, nil); r.Body["records"] != float64(0) {
		t.Fatalf("imports must stay in the token's workspace: %s", r.Raw)
	}
}

func TestImportEndpointErrors(t *testing.T) {
	h := newAPI(t, setup{})
	bad := upload(t, h, "/api/v1/imports", tokenA, "ict_provider.csv", []byte("provider_id_code,colour\nP1,red\n"))
	details, _ := bad.Body["error"].(map[string]any)["details"].([]any)
	if bad.Status != 422 || errorCode(bad) != "invalid_file" || len(details) != 1 {
		t.Fatalf("unknown column = %d %s", bad.Status, bad.Raw)
	}
	if r := upload(t, h, "/api/v1/imports", tokenA, "notes.txt", []byte("x")); r.Status != 422 || errorCode(r) != "invalid_file" {
		t.Fatalf("unsupported format = %d %s", r.Status, r.Raw)
	}
	if r := upload(t, h, "/api/v1/imports", tokenA, "", nil); r.Status != 400 || errorCode(r) != "missing_parameter" {
		t.Fatalf("no file part = %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "POST", "/api/v1/imports", tokenA, []byte("x")); r.Status != 400 || errorCode(r) != "invalid_parameter" {
		t.Fatalf("not multipart = %d %s", r.Status, r.Raw)
	}
	if r := upload(t, h, "/api/v1/imports?mode=weekly", tokenA, "ict_provider.csv", []byte(providersCSV)); r.Status != 400 || errorCode(r) != "invalid_parameter" {
		t.Fatalf("bad mode = %d %s", r.Status, r.Raw)
	}
}

func TestImportBodyLimit(t *testing.T) {
	h := newAPI(t, setup{options: func(o *httpapi.Options) { o.MaxBodyBytes = 100 }})
	r := upload(t, h, "/api/v1/imports", tokenA, "ict_provider.csv", bytes.Repeat([]byte("P1,One,IE\n"), 50))
	if r.Status != 413 || errorCode(r) != "payload_too_large" {
		t.Fatalf("oversized upload = %d %s", r.Status, r.Raw)
	}
}
