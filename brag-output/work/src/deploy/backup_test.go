// SPDX-License-Identifier: Apache-2.0

package deploy_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/crypt"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres/pgtest"
	"github.com/nexops-one/compliance-engine/pkg/store/sealed"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// container is the PostgreSQL container of the test database; its client
// tools produce and restore the backup, as an operator would.
const container = "ce-test-pg"

func dockerPG(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("docker", append([]string{"exec", container}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker exec %v: %v\n%s", args, err, out)
	}
}

func engineOn(t *testing.T, dbURL string, kek crypt.KEK) (*compliance.Engine, func()) {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pg.MigrateUp(ctx); err != nil {
		t.Fatal(err)
	}
	sens, _ := crypt.LoadSensitivity("0.1.0")
	ring := crypt.NewKeyring(kek, pg, nil)
	eng, err := compliance.New(ctx, compliance.Config{Store: sealed.Wrap(pg, ring, sens), Hasher: sealed.Hasher(ring)})
	if err != nil {
		t.Fatal(err)
	}
	return eng, pg.Close
}

func TestBackupAndRestoreWithEncryption(t *testing.T) {
	if os.Getenv(pgtest.EnvURL) == "" {
		t.Skip("set " + pgtest.EnvURL + " to run the backup and restore test")
	}
	if err := exec.Command("docker", "exec", container, "pg_dump", "--version").Run(); err != nil {
		t.Skipf("the %s container with pg_dump is required: %v", container, err)
	}
	ctx := context.Background()
	scope := adapter.Scope{TenantID: "acme", WorkspaceID: "ws-1"}
	kek, _ := crypt.NewKEK([]byte(strings.Repeat("b", 32)))

	source := pgtest.URL(t)
	u, _ := url.Parse(source)
	schemaName := u.Query().Get("search_path")
	eng, closeSrc := engineOn(t, source, kek)
	f, err := os.Open("../pkg/compliance/testdata/sample-batch.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := adapter.DecodeBatch(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	supplies := map[string][]string{}
	for entity, recs := range b.Entities {
		for field := range recs[0] {
			if field != "_meta" {
				supplies[entity] = append(supplies[entity], field)
			}
		}
	}
	if err := eng.RegisterManifest(ctx, scope, adapter.Manifest{Name: b.Source.Adapter, Version: "1", SchemaVersion: b.SchemaVersion,
		Supplies: supplies, Modes: []adapter.Mode{adapter.ModeFull, adapter.ModeIncremental}}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Ingest(ctx, scope, b); err != nil {
		t.Fatal(err)
	}
	closeSrc()

	// Back up, then restore into a fresh database.
	dump := fmt.Sprintf("/tmp/ce-backup-%d.dump", time.Now().UnixNano())
	restored := fmt.Sprintf("ce_restore_%d", time.Now().UnixNano())
	dockerPG(t, "pg_dump", "-U", "postgres", "-d", u.Path[1:], "-n", schemaName, "-Fc", "-f", dump)
	dockerPG(t, "createdb", "-U", "postgres", restored)
	t.Cleanup(func() {
		_ = exec.Command("docker", "exec", container, "dropdb", "-U", "postgres", "--if-exists", restored).Run()
		_ = exec.Command("docker", "exec", container, "rm", "-f", dump).Run()
	})
	dockerPG(t, "pg_restore", "-U", "postgres", "-d", restored, dump)
	ru := *u
	ru.Path = "/" + restored
	restoredURL := ru.String()

	withKey, closeRestored := engineOn(t, restoredURL, kek)
	defer closeRestored()
	snap, err := withKey.Snapshot(ctx, scope, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range snap.Records {
		if r.Entity == "contractual_arrangement" && fmt.Sprint(r.Data["annual_cost"]) == "48000" {
			found = true
		}
	}
	if !found {
		t.Fatal("the restored copy must read back with the same key-encryption key")
	}
	if v, err := withKey.VerifyAudit(ctx, scope); err != nil || !v.OK {
		t.Fatalf("the restored audit chain must verify: %+v %v", v, err)
	}
	if res, err := withKey.Ingest(ctx, scope, b); err != nil || !res.NoChanges {
		t.Fatalf("re-ingesting into the restored copy must change nothing: %+v %v", res, err)
	}

	otherKEK, _ := crypt.NewKEK([]byte(strings.Repeat("z", 32)))
	wrong, closeWrong := engineOn(t, restoredURL, otherKEK)
	defer closeWrong()
	if _, err := wrong.Snapshot(ctx, scope, ""); err == nil || !strings.Contains(err.Error(), "different key-encryption key") {
		t.Fatalf("a backup must not be readable without its key-encryption key: %v", err)
	}
}
