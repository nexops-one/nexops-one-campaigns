// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/nexops-one/compliance-engine/pkg/config"
	"github.com/nexops-one/compliance-engine/pkg/crypt"
	"github.com/nexops-one/compliance-engine/pkg/evidence"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/sealed"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// secureStore wraps st with encryption at rest when a KEK is configured, and
// returns the content hasher to use. Without a KEK it returns st unchanged.
func secureStore(cfg config.Config, st store.Store, logger *slog.Logger) (store.Store, func(context.Context, adapter.Scope) (func(adapter.Record) string, error), *sealed.Store, error) {
	if cfg.EncryptionKeyFile == "" {
		if logger != nil {
			logger.Warn("COMPLIANCE_ENCRYPTION_KEY_FILE is empty: sensitive fields, evidence locations and workflow texts are stored unencrypted")
		}
		return st, nil, nil, nil
	}
	kek, err := crypt.LoadKEK(cfg.EncryptionKeyFile)
	if err != nil {
		return nil, nil, nil, err
	}
	reg, err := schema.Default()
	if err != nil {
		return nil, nil, nil, err
	}
	sens, err := crypt.LoadSensitivity(reg.Latest().Version)
	if err != nil {
		return nil, nil, nil, err
	}
	ring := crypt.NewKeyring(kek, st, nil)
	s := sealed.Wrap(st, ring, sens)
	if logger != nil {
		logger.Info("encryption at rest enabled", "kek_id", kek.ID)
	}
	return s, sealed.Hasher(ring), s, nil
}

const keysSynopsis = `keys generate --out FILE
       compliance-engine keys rotate --from OLD_FILE --to NEW_FILE
       compliance-engine keys rotate --data --tenant t
       compliance-engine keys seal-existing --tenant t`

func runKeys(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 {
		return usageErr(env, keysSynopsis)
	}
	fs := flag.NewFlagSet("keys "+args[0], flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	out := fs.String("out", "", "file to write the new key to (generate)")
	from := fs.String("from", "", "current key file (rotate)")
	to := fs.String("to", "", "new key file (rotate)")
	data := fs.Bool("data", false, "rotate a tenant's data key instead of the key-encryption key")
	tenant := fs.String("tenant", "", "tenant ID")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return usageErr(env, keysSynopsis)
	}
	switch args[0] {
	case "generate":
		if *out == "" {
			return usageErr(env, "keys generate --out FILE")
		}
		enc, err := crypt.GenerateKEK()
		if err != nil {
			return fail(env, "%v", err)
		}
		f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fail(env, "%v (the key file must not exist yet)", err)
		}
		_, err = f.WriteString(enc + "\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fail(env, "%v", err)
		}
		k, _ := crypt.LoadKEK(*out)
		fmt.Fprintf(env.Stdout, "key-encryption key written to %s (id %s)\n\nKeep it outside the database backups: a backup cannot be read without it,\nand losing it makes encrypted data unrecoverable.\n", *out, k.ID)
		return 0
	case "rotate", "seal-existing":
	default:
		return usageErr(env, keysSynopsis)
	}
	cfg, err := config.Load(env.Getenv)
	if err != nil {
		return fail(env, "invalid configuration:\n%v", err)
	}
	if cfg.DatabaseURL == "" {
		return fail(env, "COMPLIANCE_DATABASE_URL is required: keys are stored in PostgreSQL")
	}
	pg, err := openPostgres(ctx, cfg, nil)
	if err != nil {
		return fail(env, "%v", err)
	}
	defer pg.Close()
	if args[0] == "rotate" && !*data {
		if *from == "" || *to == "" || *tenant != "" {
			return usageErr(env, "keys rotate --from OLD_FILE --to NEW_FILE")
		}
		old, err := crypt.LoadKEK(*from)
		if err != nil {
			return fail(env, "%v", err)
		}
		next, err := crypt.LoadKEK(*to)
		if err != nil {
			return fail(env, "%v", err)
		}
		n, err := crypt.RewrapAll(ctx, pg, old, next, nil)
		if err != nil {
			return fail(env, "%v", err)
		}
		fmt.Fprintf(env.Stdout, "re-wrapped the keys of %d tenant(s) from %s to %s\nNow set COMPLIANCE_ENCRYPTION_KEY_FILE to %s and restart the engine.\n", n, old.ID, next.ID, *to)
		return 0
	}
	if *tenant == "" || *from != "" || *to != "" {
		return usageErr(env, keysSynopsis)
	}
	if cfg.EncryptionKeyFile == "" {
		return fail(env, "COMPLIANCE_ENCRYPTION_KEY_FILE is required")
	}
	_, _, s, err := secureStore(cfg, pg, nil)
	if err != nil {
		return fail(env, "%v", err)
	}
	if args[0] == "rotate" {
		kek, _ := crypt.LoadKEK(cfg.EncryptionKeyFile)
		v, err := crypt.NewKeyring(kek, pg, nil).RotateData(ctx, *tenant)
		if err != nil {
			return fail(env, "%v", err)
		}
		fmt.Fprintf(env.Stdout, "tenant %s now writes with data key version %d; older versions stay available for reading\n", *tenant, v)
		return 0
	}
	rep, err := s.SealExisting(ctx, pg, *tenant)
	if err != nil {
		return fail(env, "%v", err)
	}
	fmt.Fprintf(env.Stdout, "sealed %d record version(s), %d evidence item(s), %d assessment(s) of tenant %s\n", rep.Records, rep.Evidence, rep.Assessments, *tenant)
	return 0
}

// managedStorage returns the managed evidence storage when configured
// (config.Load guarantees a KEK, hence a sealed store, alongside the directory).
func managedStorage(cfg config.Config, s *sealed.Store) *evidence.Managed {
	if cfg.EvidenceStorageDir == "" || s == nil {
		return nil
	}
	return &evidence.Managed{Dir: cfg.EvidenceStorageDir, Ring: s.Ring(), MaxBytes: int64(cfg.MaxBodyBytes)}
}
