// SPDX-License-Identifier: Apache-2.0

// Package config reads the standalone server configuration from COMPLIANCE_*
// environment variables. The struct tags are the single source of truth for
// variable names, defaults and documentation.
package config

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Config is the standalone server configuration.
type Config struct {
	Listen                 string `env:"COMPLIANCE_LISTEN" default:":8080" doc:"Address the HTTP server listens on."`
	DatabaseURL            string `env:"COMPLIANCE_DATABASE_URL" doc:"PostgreSQL connection URL. Empty selects the in-memory store: all data is lost when the process stops."`
	AutoMigrate            bool   `env:"COMPLIANCE_AUTO_MIGRATE" default:"true" doc:"Apply pending database migrations when the server starts. When false, the server refuses to start on a schema with pending migrations."`
	CatalogDir             string `env:"COMPLIANCE_CATALOG_DIR" doc:"Directory of additional control catalogs laid out as <catalog>/<version>.yaml, loaded after the built-in catalogs."`
	IdentifierPolicy       string `env:"COMPLIANCE_IDENTIFIER_POLICY" default:"warn" doc:"What to do with records whose LEI, country or currency codes fail validation: warn or reject."`
	MaxRecordsPerBatch     int    `env:"COMPLIANCE_MAX_RECORDS" default:"50000" doc:"Maximum number of records in one ingestion batch."`
	MaxBodyBytes           int    `env:"COMPLIANCE_MAX_BODY_BYTES" default:"33554432" doc:"Maximum HTTP request body size in bytes."`
	Tokens                 string `env:"COMPLIANCE_TOKENS" doc:"Comma-separated static API tokens as <sha256-hex>:<tenant>:<workspace>[:<role>+<role>]; without roles a token gets owner+admin. Create entries with 'compliance-engine token generate'. Required by serve unless the database holds an active stored token ('compliance-engine token create')."`
	TLSCertFile            string `env:"COMPLIANCE_TLS_CERT_FILE" doc:"TLS certificate file (PEM). Set together with COMPLIANCE_TLS_KEY_FILE to serve HTTPS directly; otherwise terminate TLS at a reverse proxy."`
	TLSKeyFile             string `env:"COMPLIANCE_TLS_KEY_FILE" doc:"TLS private key file (PEM)."`
	LicenseFile            string `env:"COMPLIANCE_LICENSE_FILE" doc:"Commercial license file. Ignored by the open-core edition."`
	LogLevel               string `env:"COMPLIANCE_LOG_LEVEL" default:"info" doc:"Log level: debug, info, warn or error. Logs are JSON on standard error."`
	RateLimit              int    `env:"COMPLIANCE_RATE_LIMIT" default:"600" doc:"Requests per minute per authenticated caller (token bucket, per server process)."`
	RateBurst              int    `env:"COMPLIANCE_RATE_BURST" default:"120" doc:"Burst size of the per-caller rate limit."`
	AuthFailureLimit       int    `env:"COMPLIANCE_AUTH_FAILURE_LIMIT" default:"30" doc:"Failed authentications per minute per client address; further requests from that address get 429 until the budget refills."`
	AllowSelfApproval      bool   `env:"COMPLIANCE_ALLOW_SELF_APPROVAL" default:"false" doc:"Let a control's owner approve their own control. Each such approval is flagged in the history and the audit log."`
	EvidenceRoot           string `env:"COMPLIANCE_EVIDENCE_ROOT" doc:"Directory whose files the engine may hash to verify file:// evidence. Empty: file evidence is verified by the customer with 'compliance-engine evidence verify'."`
	EvidenceNoChecksum     bool   `env:"COMPLIANCE_EVIDENCE_ALLOW_NO_CHECKSUM" default:"false" doc:"Accept evidence references without a checksum (integrity no_checksum), for data carried over from systems that did not record one. Such evidence never satisfies an evidence requirement until an attested check records its checksum."`
	EvidenceFetchAllow     string `env:"COMPLIANCE_EVIDENCE_FETCH_ALLOW" doc:"Comma-separated HTTPS hosts (host[:port]) the engine may download evidence from to verify it. Empty (default): the engine makes no outbound calls."`
	EncryptionKeyFile      string `env:"COMPLIANCE_ENCRYPTION_KEY_FILE" doc:"Key-encryption key file (32 random bytes, base64; create it with 'compliance-engine keys generate', mode 0600). When set, sensitive fields, evidence locations and workflow texts are encrypted at rest with per-tenant keys. Empty: no encryption at rest (a warning is logged)."`
	EvidenceStorageDir     string `env:"COMPLIANCE_EVIDENCE_STORAGE_DIR" doc:"Directory for evidence files uploaded through POST /api/v1/evidence/upload, stored encrypted with the tenant key (requires COMPLIANCE_ENCRYPTION_KEY_FILE). Empty: evidence is referenced only, never stored."`
	AuditRetentionDays     int    `env:"COMPLIANCE_AUDIT_RETENTION_DAYS" default:"3650" doc:"Audit events are kept at least this many days; retention deletes only older events, from the start of each chain."`
	RetentionInterval      string `env:"COMPLIANCE_RETENTION_INTERVAL" default:"24h" doc:"How often serve applies every workspace's retention policy (Go duration such as 24h). 0 disables the job; run 'compliance-engine retention run' instead."`
	Console                bool   `env:"COMPLIANCE_CONSOLE" default:"true" doc:"Serve the web console at /console. Sign-in uses local accounts ('compliance-engine user create'); the session cookie is Secure, so use HTTPS or localhost."`
	ConsoleTenant          string `env:"COMPLIANCE_CONSOLE_TENANT" doc:"Tenant the console signs users into. Empty: the sign-in form asks for the tenant."`
	SessionIdle            string `env:"COMPLIANCE_SESSION_IDLE" default:"30m" doc:"Console sessions end after this long without a request (Go duration, at least 1m)."`
	SessionMax             string `env:"COMPLIANCE_SESSION_MAX" default:"12h" doc:"Console sessions end this long after sign-in, whatever the activity (Go duration, at least 1m)."`
	Demo                   bool   `env:"COMPLIANCE_DEMO" default:"false" doc:"Demo mode: on first start, create tenant demo with the sample workspace and demo users, and print their passwords once. Never enable it on a deployment holding real data."`
	DemoConnectedWorkspace string `env:"COMPLIANCE_DEMO_CONNECTED_WORKSPACE" doc:"Demo mode only: also prepare this second sample workspace of tenant demo for a connected host product (spec step 6), with a service token written to COMPLIANCE_DEMO_TOKEN_FILE."`
	DemoTokenFile          string `env:"COMPLIANCE_DEMO_TOKEN_FILE" doc:"Demo mode only: file receiving the connected workspace's service token (mode 0600). An existing file is kept and no token is created."`
}

// Variable describes one supported environment variable.
type Variable struct {
	Name    string
	Default string
	Doc     string
}

// Variables lists every supported variable in declaration order.
func Variables() []Variable { return VariablesOf(Config{}) }

// VariablesOf lists the variables of a configuration struct whose fields carry
// env, default and doc tags, in declaration order. Editions describe their own
// configuration this way.
func VariablesOf(cfg any) []Variable {
	t := reflect.TypeOf(cfg)
	out := make([]Variable, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		out = append(out, Variable{Name: f.Tag.Get("env"), Default: f.Tag.Get("default"), Doc: f.Tag.Get("doc")})
	}
	return out
}

// Load reads the configuration through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	var c Config
	errs := []error{LoadInto(getenv, &c)}
	if c.IdentifierPolicy != "warn" && c.IdentifierPolicy != "reject" {
		errs = append(errs, fmt.Errorf("COMPLIANCE_IDENTIFIER_POLICY: %q must be warn or reject", c.IdentifierPolicy))
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("COMPLIANCE_LOG_LEVEL: %q must be debug, info, warn or error", c.LogLevel))
	}
	if c.EvidenceStorageDir != "" && c.EncryptionKeyFile == "" {
		errs = append(errs, errors.New("COMPLIANCE_EVIDENCE_STORAGE_DIR requires COMPLIANCE_ENCRYPTION_KEY_FILE: uploaded evidence is always stored encrypted"))
	}
	if c.RetentionInterval != "0" {
		if d, err := time.ParseDuration(c.RetentionInterval); err != nil || d < time.Minute {
			errs = append(errs, fmt.Errorf("COMPLIANCE_RETENTION_INTERVAL: %q must be 0 or a duration of at least 1m such as 24h", c.RetentionInterval))
		}
	}
	for name, v := range map[string]string{"COMPLIANCE_SESSION_IDLE": c.SessionIdle, "COMPLIANCE_SESSION_MAX": c.SessionMax} {
		if d, err := time.ParseDuration(v); err != nil || d < time.Minute {
			errs = append(errs, fmt.Errorf("%s: %q must be a duration of at least 1m such as 30m", name, v))
		}
	}
	if (c.DemoConnectedWorkspace == "") != (c.DemoTokenFile == "") {
		errs = append(errs, errors.New("COMPLIANCE_DEMO_CONNECTED_WORKSPACE and COMPLIANCE_DEMO_TOKEN_FILE must be set together"))
	} else if c.DemoConnectedWorkspace != "" && !c.Demo {
		errs = append(errs, errors.New("COMPLIANCE_DEMO_CONNECTED_WORKSPACE requires COMPLIANCE_DEMO=true"))
	}
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		errs = append(errs, errors.New("COMPLIANCE_TLS_CERT_FILE and COMPLIANCE_TLS_KEY_FILE must be set together"))
	}
	return c, errors.Join(errs...)
}

// LoadInto fills the string, bool and int fields of the struct cfg points
// to from their env tags (default tag when unset). Integers must be positive.
// Every invalid value is reported.
func LoadInto(getenv func(string) string, cfg any) error {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()
	var errs []error
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := f.Tag.Get("env")
		raw := strings.TrimSpace(getenv(name))
		if raw == "" {
			raw = f.Tag.Get("default")
		}
		if raw == "" {
			continue
		}
		switch f.Type.Kind() {
		case reflect.String:
			v.Field(i).SetString(raw)
		case reflect.Bool:
			b, err := strconv.ParseBool(raw)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %q is not true or false", name, raw))
				continue
			}
			v.Field(i).SetBool(b)
		case reflect.Int:
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				errs = append(errs, fmt.Errorf("%s: %q is not a positive integer", name, raw))
				continue
			}
			v.Field(i).SetInt(int64(n))
		}
	}
	return errors.Join(errs...)
}
