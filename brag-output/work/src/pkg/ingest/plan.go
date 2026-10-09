// SPDX-License-Identifier: Apache-2.0

// Package ingest plans the effect of a batch on a workspace: record-level
// validation (L1, L1b), identity, and the changes to commit. It performs no I/O.
package ingest

import (
	"sort"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// Options is the context a batch is planned in.
type Options struct {
	Schema      *schema.Schema
	Manifest    adapter.Manifest
	Policy      canonical.IdentifierPolicy
	Current     []store.RecordVersion
	IngestionID string
	Now         time.Time
	// Hash computes record content hashes; nil uses canonical.Hash (SHA-256).
	Hash func(adapter.Record) string
}

// Result is the outcome of an ingestion (or dry run).
type Result struct {
	IngestionID     string              `json:"ingestion_id"`
	BatchID         string              `json:"batch_id,omitempty"`
	SchemaVersion   string              `json:"schema_version"`
	Source          adapter.Source      `json:"source"`
	Mode            adapter.Mode        `json:"mode"`
	DryRun          bool                `json:"dry_run"`
	Accepted        int                 `json:"accepted"`
	RejectedRecords int                 `json:"rejected_records"`
	Errors          []schema.FieldError `json:"errors"`
	Warnings        []schema.FieldError `json:"warnings"`
	Created         int                 `json:"created"`
	Updated         int                 `json:"updated"`
	Deleted         int                 `json:"deleted"`
	Unchanged       int                 `json:"unchanged"`
	NoChanges       bool                `json:"no_changes"`
	SnapshotID      string              `json:"snapshot_id"`
}

func recKey(entity, key string) string { return entity + "\x00" + key }

// Plan validates every record and computes the changes to apply to Current.
func Plan(b adapter.Batch, opt Options) (Result, []store.Change) {
	hash := opt.Hash
	if hash == nil {
		hash = canonical.Hash
	}
	res := Result{
		IngestionID: opt.IngestionID, BatchID: b.BatchID(), SchemaVersion: b.SchemaVersion,
		Source: b.Source, Mode: b.Mode(), Errors: []schema.FieldError{}, Warnings: []schema.FieldError{},
	}
	current := map[string]store.RecordVersion{}
	for _, v := range opt.Current {
		current[recKey(v.Entity, v.Key)] = v
	}
	accepted := map[string]store.RecordVersion{}
	var order []string
	rejectedEntities := map[string]bool{}

	entities := make([]string, 0, len(b.Entities))
	for name := range b.Entities {
		entities = append(entities, name)
	}
	sort.Strings(entities)

	for _, entity := range entities {
		e, known := opt.Schema.Entity(entity)
		for i, raw := range b.Entities[entity] {
			rec, err := canonical.Normalize(raw)
			ref := adapter.MetaOf(rec).SourceRecordRef
			var errs []schema.FieldError
			switch {
			case err != nil:
				errs = []schema.FieldError{{Entity: entity, Code: "invalid_json", Message: err.Error()}}
			case !known:
				errs = []schema.FieldError{{Entity: entity, Code: "unknown_entity", Message: "entity is not defined in the canonical schema"}}
			case len(nulFields(rec)) > 0:
				for _, f := range nulFields(rec) {
					errs = append(errs, schema.FieldError{Entity: entity, Field: f, Code: "invalid_character", Message: "text must not contain the NUL character (U+0000)"})
				}
			default:
				errs = opt.Schema.ValidateRecord(entity, rec)
			}
			if len(errs) == 0 {
				ids := canonical.CheckIdentifiers(e, rec)
				if opt.Policy == canonical.PolicyReject {
					errs = ids
				} else {
					res.Warnings = append(res.Warnings, locate(ids, i, ref)...)
				}
			}
			var key string
			if len(errs) == 0 {
				key, _ = canonical.Key(e, rec) // identity fields are guaranteed by L1 "required"
				if _, dup := accepted[recKey(entity, key)]; dup {
					errs = []schema.FieldError{{Entity: entity, Code: "duplicate_identity", Message: "another record in this batch has identity key " + key}}
				}
			}
			if len(errs) > 0 {
				res.RejectedRecords++
				rejectedEntities[entity] = true
				res.Errors = append(res.Errors, locate(errs, i, ref)...)
				continue
			}
			res.Accepted++
			k := recKey(entity, key)
			accepted[k] = store.RecordVersion{
				Entity: entity, Key: key, Data: rec, Hash: hash(rec), SchemaVersion: opt.Schema.Version,
				Source: b.Source, IngestionID: opt.IngestionID, SourceRecordRef: ref, WrittenAt: opt.Now,
			}
			order = append(order, k)
		}
	}

	var changes []store.Change
	for _, k := range order {
		v := accepted[k]
		prev, exists := current[k]
		switch {
		case exists && prev.Hash == v.Hash:
			res.Unchanged++
		case exists:
			res.Updated++
			changes = append(changes, store.Change{Op: store.OpUpdate, Entity: v.Entity, Key: v.Key, Version: &v, PreviousHash: prev.Hash})
		default:
			res.Created++
			changes = append(changes, store.Change{Op: store.OpCreate, Entity: v.Entity, Key: v.Key, Version: &v})
		}
	}

	if b.Mode() == adapter.ModeFull {
		skipped := map[string]bool{}
		for _, v := range opt.Current {
			if _, keep := accepted[recKey(v.Entity, v.Key)]; keep {
				continue
			}
			if v.Source.Adapter != opt.Manifest.Name {
				continue
			}
			if _, inScope := opt.Manifest.Supplies[v.Entity]; !inScope {
				continue
			}
			if rejectedEntities[v.Entity] {
				if !skipped[v.Entity] {
					skipped[v.Entity] = true
					res.Warnings = append(res.Warnings, schema.FieldError{
						Entity: v.Entity, Index: -1, Code: "full_sync_deletions_skipped",
						Message: "records of this entity were rejected, so records absent from the batch were not deleted",
					})
				}
				continue
			}
			res.Deleted++
			changes = append(changes, store.Change{Op: store.OpDelete, Entity: v.Entity, Key: v.Key, PreviousHash: v.Hash})
		}
	}
	res.NoChanges = len(changes) == 0
	return res, changes
}

// nulFields returns, sorted, the top-level fields whose name or value contains
// U+0000. PostgreSQL cannot store it, and no register value needs it, so such
// records are rejected as data by every store alike.
func nulFields(rec adapter.Record) []string {
	var out []string
	for k, v := range rec {
		if strings.ContainsRune(k, 0) || hasNUL(v) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func hasNUL(v any) bool {
	switch t := v.(type) {
	case string:
		return strings.ContainsRune(t, 0)
	case map[string]any:
		for k, x := range t {
			if strings.ContainsRune(k, 0) || hasNUL(x) {
				return true
			}
		}
	case []any:
		for _, x := range t {
			if hasNUL(x) {
				return true
			}
		}
	}
	return false
}

func locate(errs []schema.FieldError, index int, ref string) []schema.FieldError {
	for i := range errs {
		errs[i].Index, errs[i].SourceRecordRef = index, ref
	}
	return errs
}
