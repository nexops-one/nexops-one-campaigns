// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func loadSet(ctx context.Context, src catalog.Source) (*catalog.Set, error) {
	cs, err := src.Catalogs(ctx)
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, fmt.Errorf("no catalogs found (expected <catalog>/<version>.yaml files)")
	}
	reg, err := schema.Default()
	if err != nil {
		return nil, err
	}
	return catalog.NewSet(reg, cs)
}

func runCatalog(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 {
		return usageErr(env, "catalog validate [dir] | catalog diff [--dir d] [--json] <a> <b>")
	}
	switch args[0] {
	case "validate":
		if len(args) > 2 {
			return usageErr(env, "catalog validate [dir]")
		}
		var src catalog.Source = catalog.Embedded()
		if len(args) == 2 {
			src = catalog.FSSource{FS: os.DirFS(args[1])}
		}
		set, err := loadSet(ctx, src)
		if err != nil {
			return fail(env, "%v", err)
		}
		for _, ref := range set.Refs() {
			c, _ := set.Get(ref)
			fmt.Fprintf(env.Stdout, "ok  %s  %s, %d control(s)\n", ref, c.Framework, len(c.Controls))
		}
		return 0
	case "diff":
		fs := flag.NewFlagSet("catalog diff", flag.ContinueOnError)
		fs.SetOutput(env.Stderr)
		dir := fs.String("dir", "", "directory of additional catalogs")
		asJSON := fs.Bool("json", false, "print the diff as JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 2 {
			return usageErr(env, "catalog diff [--dir d] [--json] <catalog@version> <catalog@version>")
		}
		a, errA := catalog.ParseRef(fs.Arg(0))
		b, errB := catalog.ParseRef(fs.Arg(1))
		if errA != nil || errB != nil {
			return usageErr(env, "catalog diff [--dir d] [--json] <catalog@version> <catalog@version>")
		}
		sources := catalog.Sources{catalog.Embedded()}
		if *dir != "" {
			sources = append(sources, catalog.FSSource{FS: os.DirFS(*dir)})
		}
		set, err := loadSet(ctx, sources)
		if err != nil {
			return fail(env, "%v", err)
		}
		ca, okA := set.Get(a)
		cb, okB := set.Get(b)
		if !okA || !okB {
			return fail(env, "unknown catalog version (loaded: %v)", set.Refs())
		}
		d := catalog.DiffCatalogs(ca, cb)
		if *asJSON {
			enc := json.NewEncoder(env.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(d)
			return 0
		}
		fmt.Fprintf(env.Stdout, "%s -> %s\n", d.From, d.To)
		if len(d.CatalogFields) > 0 {
			fmt.Fprintf(env.Stdout, "catalog fields changed: %s\n", strings.Join(d.CatalogFields, ", "))
		}
		for _, c := range d.Controls {
			switch c.Kind {
			case catalog.ChangeAdded:
				fmt.Fprintf(env.Stdout, "+ %s\n", c.ControlID)
			case catalog.ChangeRemoved:
				fmt.Fprintf(env.Stdout, "- %s\n", c.ControlID)
			default:
				fmt.Fprintf(env.Stdout, "~ %s (%s)\n", c.ControlID, strings.Join(c.Fields, ", "))
			}
		}
		if len(d.Controls) == 0 && len(d.CatalogFields) == 0 {
			fmt.Fprintln(env.Stdout, "no differences")
		}
		return 0
	}
	return usageErr(env, "catalog validate [dir] | catalog diff [--dir d] [--json] <a> <b>")
}
