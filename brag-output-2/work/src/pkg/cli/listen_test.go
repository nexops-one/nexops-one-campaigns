// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/push"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

type listenSession struct {
	client *push.Client
	stop   func() (int, string)
}

func startListen(t *testing.T, junit string) listenSession {
	t.Helper()
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var out, errOut bytes.Buffer
	env := Env{Stdout: &out, Stderr: &errOut, Getenv: func(string) string { return "" }}
	done := make(chan int, 1)
	derived := map[string][]string{"cloud_resource": {"country"}}
	go func() { done <- serveConformance(ctx, ln, newConformanceServer(reg, derived), env, junit) }()
	c := push.New("http://"+ln.Addr().String(), "any-token")
	c.MaxAttempts = 1
	t.Cleanup(cancel)
	return listenSession{client: c, stop: func() (int, string) {
		cancel()
		code := <-done
		return code, out.String() + errOut.String()
	}}
}

var listenManifest = adapter.Manifest{Name: "vendor-inventory", Version: "1.0.0", SchemaVersion: "0.1.0",
	Supplies: map[string][]string{
		"ict_provider":   {"provider_id_code", "legal_name", "hq_country"},
		"cloud_resource": {"resource_ref", "region", "country"},
	},
	Modes: []adapter.Mode{adapter.ModeFull}}

func listenBatch(country string) adapter.Batch {
	res := adapter.Record{"resource_ref": "r-1", "region": "eu-west-1", "country": "IE"}
	adapter.MarkDerived(res, adapter.DerivedField{Field: "country", Method: "region_lookup"})
	return adapter.NewBuilder(listenManifest, "vendor-db").Mode(adapter.ModeFull).
		Add("ict_provider", adapter.Record{"provider_id_code": "V1", "legal_name": "Nimbus", "hq_country": country}).
		Add("cloud_resource", res).
		Build()
}

func TestListenPassesConformingPushes(t *testing.T) {
	junit := filepath.Join(t.TempDir(), "report.xml")
	s := startListen(t, junit)
	ctx := context.Background()
	if err := s.client.RegisterManifest(ctx, listenManifest); err != nil {
		t.Fatal(err)
	}
	res, err := s.client.Push(ctx, listenBatch("IE"))
	if err != nil || len(res) != 1 || res[0].Accepted != 2 || res[0].IngestionID != "conformance-push-1" {
		t.Fatalf("push = %+v, %v", res, err)
	}
	code, out := s.stop()
	if code != 0 || !strings.Contains(out, "listening on http://127.0.0.1:") || !strings.Contains(out, "result: PASS") {
		t.Fatalf("listen = %d\n%s", code, out)
	}
	if x, err := os.ReadFile(junit); err != nil || !strings.Contains(string(x), `failures="0"`) {
		t.Fatalf("junit = %s, %v", x, err)
	}
}

func TestListenReportsProblems(t *testing.T) {
	s := startListen(t, "")
	ctx := context.Background()
	if err := s.client.RegisterManifest(ctx, listenManifest); err != nil {
		t.Fatal(err)
	}
	res, err := s.client.Push(ctx, listenBatch("Ireland"))
	if err != nil || res[0].RejectedRecords != 1 || res[0].Errors[0].Field != "hq_country" {
		t.Fatalf("push = %+v, %v", res, err)
	}
	if _, err := s.client.Push(ctx, listenBatch("IE")); err != nil {
		t.Fatal(err)
	}
	unknown := listenBatch("IE")
	unknown.Source.Adapter = "unknown"
	var ae *push.APIError
	if _, err := s.client.Push(ctx, unknown); !errors.As(err, &ae) || ae.Status != 422 || ae.Code != "unknown_adapter" {
		t.Fatalf("unknown adapter = %v", err)
	}
	code, out := s.stop()
	for _, want := range []string{"FAIL records", "[records] push 1 ict_provider[0] hq_country", "FAIL full_scope", "FAIL manifest_match", `adapter "unknown"`, "result: FAIL"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q", want)
		}
	}
	if code != 1 {
		t.Fatalf("exit code = %d\n%s", code, out)
	}
}

func TestListenWithoutPushesFails(t *testing.T) {
	s := startListen(t, "")
	code, out := s.stop()
	if code != 1 || !strings.Contains(out, "no manifest was registered") || !strings.Contains(out, "no batch was pushed") {
		t.Fatalf("empty session = %d\n%s", code, out)
	}
}
