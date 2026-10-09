// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := config.Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8080" || !c.AutoMigrate || c.IdentifierPolicy != "warn" || c.MaxRecordsPerBatch != 50000 ||
		c.MaxBodyBytes != 33554432 || c.LogLevel != "info" || c.DatabaseURL != "" || c.Tokens != "" {
		t.Fatalf("defaults = %+v", c)
	}
}

func TestOverrides(t *testing.T) {
	c, err := config.Load(env(map[string]string{
		"COMPLIANCE_LISTEN":            "127.0.0.1:9000",
		"COMPLIANCE_DATABASE_URL":      "postgres://x",
		"COMPLIANCE_AUTO_MIGRATE":      "false",
		"COMPLIANCE_IDENTIFIER_POLICY": "reject",
		"COMPLIANCE_MAX_RECORDS":       " 10 ",
		"COMPLIANCE_LOG_LEVEL":         "debug",
		"COMPLIANCE_TLS_CERT_FILE":     "c.pem",
		"COMPLIANCE_TLS_KEY_FILE":      "k.pem",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:9000" || c.DatabaseURL != "postgres://x" || c.AutoMigrate || c.IdentifierPolicy != "reject" ||
		c.MaxRecordsPerBatch != 10 || c.LogLevel != "debug" || c.TLSCertFile != "c.pem" || c.TLSKeyFile != "k.pem" {
		t.Fatalf("overrides = %+v", c)
	}
}

func TestInvalidValues(t *testing.T) {
	cases := map[string]map[string]string{
		"COMPLIANCE_MAX_RECORDS":       {"COMPLIANCE_MAX_RECORDS": "many"},
		"COMPLIANCE_MAX_BODY_BYTES":    {"COMPLIANCE_MAX_BODY_BYTES": "-1"},
		"COMPLIANCE_AUTO_MIGRATE":      {"COMPLIANCE_AUTO_MIGRATE": "sometimes"},
		"COMPLIANCE_IDENTIFIER_POLICY": {"COMPLIANCE_IDENTIFIER_POLICY": "ignore"},
		"COMPLIANCE_LOG_LEVEL":         {"COMPLIANCE_LOG_LEVEL": "loud"},
		"COMPLIANCE_TLS_KEY_FILE":      {"COMPLIANCE_TLS_CERT_FILE": "c.pem"},
		"COMPLIANCE_DEMO_TOKEN_FILE":   {"COMPLIANCE_DEMO": "true", "COMPLIANCE_DEMO_CONNECTED_WORKSPACE": "nexops"},
		"COMPLIANCE_DEMO=true":         {"COMPLIANCE_DEMO_CONNECTED_WORKSPACE": "nexops", "COMPLIANCE_DEMO_TOKEN_FILE": "t"},
	}
	for name, vars := range cases {
		_, err := config.Load(env(vars))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestVariablesAreDocumented(t *testing.T) {
	vars := config.Variables()
	if len(vars) != 30 {
		t.Fatalf("variables = %d", len(vars))
	}
	seen := map[string]bool{}
	for _, v := range vars {
		if !strings.HasPrefix(v.Name, "COMPLIANCE_") || v.Doc == "" || seen[v.Name] {
			t.Errorf("bad variable %+v", v)
		}
		seen[v.Name] = true
	}
}

func TestSessionDurations(t *testing.T) {
	env := map[string]string{"COMPLIANCE_SESSION_IDLE": "10s", "COMPLIANCE_SESSION_MAX": "forever"}
	_, err := config.Load(func(k string) string { return env[k] })
	if err == nil || !strings.Contains(err.Error(), "COMPLIANCE_SESSION_IDLE") || !strings.Contains(err.Error(), "COMPLIANCE_SESSION_MAX") {
		t.Fatalf("err = %v", err)
	}
	c, err := config.Load(func(string) string { return "" })
	if err != nil || c.SessionIdle != "30m" || c.SessionMax != "12h" || !c.Console || c.Demo {
		t.Fatalf("defaults = %+v, %v", c, err)
	}
}
