// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"errors"
	"fmt"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// Validate checks a parsed catalog against the canonical schema it targets and
// returns that schema. Every requires path must exist and belong to the rule
// entity; filter and rule fields must also be listed in requires.
func Validate(c *Catalog, reg *schema.Registry) (*schema.Schema, error) {
	s, err := reg.Match(c.SchemaVersion)
	if err != nil {
		return nil, fmt.Errorf("catalog %s: %w", c.Ref(), err)
	}
	var errs []error
	seen := map[string]bool{}
	for _, ctl := range c.Controls {
		if seen[ctl.ID] {
			errs = append(errs, fmt.Errorf("control %s: duplicate id", ctl.ID))
			continue
		}
		seen[ctl.ID] = true
		errs = append(errs, validateControl(s, ctl)...)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("catalog %s: %w", c.Ref(), err)
	}
	return s, nil
}

func validateControl(s *schema.Schema, ctl Control) []error {
	var errs []error
	fail := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("control %s: %s", ctl.ID, fmt.Sprintf(format, a...)))
	}
	r := ctl.Rule
	if r.Kind == KindManual {
		if len(ctl.Requires) > 0 || r.Entity != "" {
			fail("manual rules take no requires and no entity")
		}
		return errs
	}
	e, ok := s.Entity(r.Entity)
	if !ok {
		fail("rule entity %q is not in canonical schema %s", r.Entity, s.Version)
		return errs
	}
	required := map[string]bool{}
	for _, p := range ctl.Requires {
		pe, pf, ok := s.Field(p)
		switch {
		case !ok:
			fail("requires %q is not a canonical field", p)
		case pe.Name != e.Name:
			fail("requires %q must belong to rule entity %s", p, e.Name)
		default:
			required[pf.Name] = true
		}
	}
	needRequired := func(field, role string) {
		if _, ok := e.Fields[field]; !ok {
			fail("%s %q is not a field of %s", role, field, e.Name)
			return
		}
		if !required[field] {
			fail("%s %q must also be listed in requires", role, field)
		}
	}
	for _, cond := range r.Filter {
		needRequired(cond.Field, "filter field")
	}
	switch r.Kind {
	case KindFieldsComplete:
		if len(ctl.Requires) == 0 {
			fail("fields_complete needs at least one requires field")
		}
	case KindReferencesResolved:
		for _, f := range r.Fields {
			needRequired(f, "reference field")
			if fd, ok := e.Fields[f]; ok && fd.References == "" {
				fail("reference field %q has no x-references", f)
			}
		}
	case KindFieldEquals, KindFieldIn:
		needRequired(r.Field, "rule field")
	}
	return errs
}
