// SPDX-License-Identifier: Apache-2.0

package httpapi_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
)

type echoProfile struct{}

func (echoProfile) ID() string                 { return "echo" }
func (echoProfile) Feature() extension.Feature { return extension.FeatureReportProfileB }
func (echoProfile) Formats() []string          { return nil }
func (echoProfile) Parameters() []extension.ParameterSpec {
	return []extension.ParameterSpec{{Name: "label", Label: "Label", Pattern: `[a-z]+`}}
}
func (echoProfile) Generate(_ context.Context, in extension.ReportInput) (extension.ReportOutput, error) {
	facts, _ := json.Marshal(map[string]any{"label": in.Parameters["label"], "allow_incomplete": in.AllowIncomplete})
	return extension.ReportOutput{Facts: facts, Complete: true}, nil
}

func TestReportParametersInTheAPI(t *testing.T) {
	api := newAPI(t, setup{engine: func(c *compliance.Config) {
		c.Extensions.ReportProfiles = []extension.ReportProfile{echoProfile{}}
	}})
	r := call(t, api, "POST", "/api/v1/reports", tokenA, map[string]any{"profile": "echo", "parameters": map[string]string{"label": "hello"}, "allow_incomplete": true})
	facts, _ := r.Body["facts"].(map[string]any)
	if r.Status != 201 || facts["label"] != "hello" || facts["allow_incomplete"] != true {
		t.Fatalf("report = %d %v", r.Status, r.Body)
	}
	for _, params := range []map[string]string{{"nope": "x"}, {"label": "UPPER"}} {
		if r := call(t, api, "POST", "/api/v1/reports", tokenA, map[string]any{"profile": "echo", "parameters": params}); r.Status != 422 || errorCode(r) != "invalid_parameter" {
			t.Fatalf("%v = %d %v", params, r.Status, r.Body)
		}
	}
	profiles := call(t, api, "GET", "/api/v1/report-profiles", tokenA, nil)
	list, _ := profiles.Body["profiles"].([]any)
	if p := list[0].(map[string]any); p["id"] != "echo" || len(p["parameters"].([]any)) != 1 {
		t.Fatalf("profiles = %v", profiles.Body)
	}
}
