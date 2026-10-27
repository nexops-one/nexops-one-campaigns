// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/config"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres"
)

// operatorEngine opens the configured PostgreSQL engine (with encryption at
// rest when a KEK is configured) for operator commands.
func operatorEngine(ctx context.Context, env Env) (*compliance.Engine, func(), error) {
	eng, _, closeFn, err := operatorEngineStore(ctx, env)
	return eng, closeFn, err
}

// operatorEngineStore also returns the undecorated PostgreSQL store (key operations).
func operatorEngineStore(ctx context.Context, env Env) (*compliance.Engine, *postgres.Store, func(), error) {
	cfg, err := config.Load(env.Getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid configuration:\n%v", err)
	}
	if cfg.DatabaseURL == "" {
		return nil, nil, nil, fmt.Errorf("COMPLIANCE_DATABASE_URL is required")
	}
	pg, err := openPostgres(ctx, cfg, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	st, hasher, sealedStore, err := secureStore(cfg, pg, nil)
	if err != nil {
		pg.Close()
		return nil, nil, nil, err
	}
	eng, err := compliance.New(ctx, compliance.Config{Store: st, Hasher: hasher,
		IdentifierPolicy: canonical.IdentifierPolicy(cfg.IdentifierPolicy),
		Retention:        compliance.RetentionOptions{AuditDays: cfg.AuditRetentionDays},
		Workflow:         compliance.WorkflowOptions{Managed: managedStorage(cfg, sealedStore)}})
	if err != nil {
		pg.Close()
		return nil, nil, nil, err
	}
	return eng, pg, pg.Close, nil
}

// retentionAll applies retention to every workspace (or the given one) and
// reports each result through report.
func retentionAll(ctx context.Context, eng *compliance.Engine, only compliance.Scope, dryRun bool, report func(compliance.RetentionReport, error)) error {
	scopes := []compliance.Scope{only}
	if only.TenantID == "" {
		var err error
		if scopes, err = eng.WorkspaceScopes(ctx); err != nil {
			return err
		}
	}
	for _, sc := range scopes {
		report(eng.RunRetention(ctx, sc, dryRun))
	}
	return nil
}

func runRetention(ctx context.Context, args []string, env Env) int {
	const synopsis = "retention run [--dry-run] [--tenant t --workspace w]"
	if len(args) == 0 || args[0] != "run" {
		return usageErr(env, synopsis)
	}
	fs := flag.NewFlagSet("retention run", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	dry := fs.Bool("dry-run", false, "report what would be deleted without deleting")
	tenant := fs.String("tenant", "", "tenant ID (default: every workspace)")
	workspace := fs.String("workspace", "", "workspace ID")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || (*tenant == "") != (*workspace == "") {
		return usageErr(env, synopsis)
	}
	eng, closeFn, err := operatorEngine(ctx, env)
	if err != nil {
		return fail(env, "%v", err)
	}
	defer closeFn()
	failed := 0
	err = retentionAll(ctx, eng, compliance.Scope{TenantID: *tenant, WorkspaceID: *workspace}, *dry, func(r compliance.RetentionReport, err error) {
		name := r.Scope.TenantID + "/" + r.Scope.WorkspaceID
		if err != nil {
			failed++
			fmt.Fprintf(env.Stderr, "%s: %v\n", name, err)
			return
		}
		if r.DryRun {
			fmt.Fprintf(env.Stdout, "%s\twould delete %d revision(s), %d evaluation(s), %d evidence item(s)\n", name,
				r.Plan.DeleteRevisions, len(r.Plan.DeleteEvaluations), len(r.Plan.DeleteEvidence))
			return
		}
		fmt.Fprintf(env.Stdout, "%s\tdeleted %d revision(s), %d record version(s), %d evaluation(s), %d evidence item(s), %d audit event(s)\n", name,
			r.Deleted.Revisions, r.Deleted.RecordVersions, r.Deleted.Evaluations, r.Deleted.Evidence, r.AuditPruned)
	})
	if err != nil {
		return fail(env, "%v", err)
	}
	if failed > 0 {
		return fail(env, "retention failed for %d workspace(s)", failed)
	}
	return 0
}

// startRetentionJob applies retention every interval until ctx ends.
func startRetentionJob(ctx context.Context, eng *compliance.Engine, interval time.Duration, logger *slog.Logger) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = retentionAll(ctx, eng, compliance.Scope{}, false, func(r compliance.RetentionReport, err error) {
					if err != nil {
						logger.Error("retention failed", "tenant", r.Scope.TenantID, "workspace", r.Scope.WorkspaceID, "error", err)
						return
					}
					logger.Info("retention", "tenant", r.Scope.TenantID, "workspace", r.Scope.WorkspaceID,
						"revisions", r.Deleted.Revisions, "evaluations", r.Deleted.Evaluations, "evidence", r.Deleted.Evidence, "audit_events", r.AuditPruned)
				})
			}
		}
	}()
}
