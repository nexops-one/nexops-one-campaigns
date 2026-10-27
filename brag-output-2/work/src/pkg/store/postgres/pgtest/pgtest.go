// SPDX-License-Identifier: Apache-2.0

// Package pgtest gives each test an isolated, empty PostgreSQL schema.
package pgtest

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnvURL names the variable holding the test database URL.
const EnvURL = "COMPLIANCE_TEST_DATABASE_URL"

// URL returns a connection URL whose search_path is a fresh, empty schema.
// It skips the test when EnvURL is not set, and drops the schema when the
// test ends.
func URL(t testing.TB) string {
	t.Helper()
	base := os.Getenv(EnvURL)
	if base == "" {
		t.Skip("set " + EnvURL + " to run PostgreSQL tests")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect to the test database: %v", err)
	}
	schema := fmt.Sprintf("ce_test_%d_%d", time.Now().UnixNano(), rand.IntN(1_000_000))
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close(ctx)
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
