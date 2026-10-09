// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"fmt"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/extension"
)

// catalogEntitled reports whether the workspace may evaluate against c.
func (e *Engine) catalogEntitled(ctx context.Context, scope Scope, c *catalog.Catalog) bool {
	return c.Feature == "" || e.ents.Allowed(ctx, scope, extension.Feature(c.Feature)).Allowed
}

// latestFor returns the highest version of each catalog the workspace is
// entitled to, sorted by name: a maintained catalog version the workspace
// may not use is skipped in favor of the version before it.
func (e *Engine) latestFor(ctx context.Context, scope Scope) []*catalog.Catalog {
	refs := e.catalogs.Refs() // sorted by name, then ascending version
	var out []*catalog.Catalog
	for i := len(refs) - 1; i >= 0; i-- {
		c, _ := e.catalogs.Get(refs[i])
		if len(out) > 0 && out[len(out)-1].Catalog == c.Catalog {
			continue
		}
		if e.catalogEntitled(ctx, scope, c) {
			out = append(out, c)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// resolveCatalogs returns the catalogs for refs (none: latestFor). An
// explicit version the workspace is not entitled to is refused.
func (e *Engine) resolveCatalogs(ctx context.Context, scope Scope, refs []catalog.Ref) ([]*catalog.Catalog, error) {
	if len(refs) == 0 {
		return e.latestFor(ctx, scope), nil
	}
	cats, err := e.catalogs.Resolve(refs)
	if err != nil {
		return nil, err
	}
	for _, c := range cats {
		if c.Feature == "" {
			continue
		}
		if _, err := extension.Require(ctx, e.ents, scope, extension.Feature(c.Feature)); err != nil {
			return nil, fmt.Errorf("catalog %s: %w", c.Ref(), err)
		}
	}
	return cats, nil
}

// CatalogInfo describes a loaded catalog version for a workspace.
type CatalogInfo struct {
	Ref      catalog.Ref `json:"ref"`
	Feature  string      `json:"feature,omitempty"`
	Origin   string      `json:"origin,omitempty"`
	Entitled bool        `json:"entitled"`
}

// CatalogInfos lists the loaded catalog versions with the workspace's
// entitlement to evaluate against each. Reading catalogs is never gated.
func (e *Engine) CatalogInfos(ctx context.Context, scope Scope) []CatalogInfo {
	out := []CatalogInfo{}
	for _, r := range e.catalogs.Refs() {
		c, _ := e.catalogs.Get(r)
		out = append(out, CatalogInfo{Ref: r, Feature: c.Feature, Origin: c.Origin, Entitled: e.catalogEntitled(ctx, scope, c)})
	}
	return out
}
