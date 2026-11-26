package validator

import (
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// groupWord is the name of the call grouping the items applying to the
// values of a role, as in e=g(length(ge=1), v=cmp(ge=0)). It names no
// validator.
const groupWord = "g"

// role returns the step applying the value of the item a, a role keyword
// item of vo, to the values of that role of the value x of type t, each
// error passed through wrap. place names what a applies to in errors.
// depth counts the loops around the step, as nested does.
func (w *writer) role(
	vo ValidatorOptions,
	a expr.Arg,
	place string,
	t types.Type,
	x genExpr,
	wrap wrapFunc,
	depth int,
) (genStep, error) {
	reg := w.sel.opts.Registry
	roles := ext.Roles(reg, t)

	if err := checkRoles(reg, t, roles); err != nil {
		return nil, fmt.Errorf("%w: %s: %s on %s: %w",
			ErrConflict, vo.src.origin(), vo.src.spelling(), place, err)
	}

	if !slices.Contains(roles, a.Name) {
		if _, ok := reg.Lookup(a.Name); ok {
			return nil, keywordError(reg, vo, place)
		}

		return nil, fmt.Errorf("%w: %s: %s on %s: %s offers no role %s; %s",
			ErrDirective, vo.src.origin(), vo.src.spelling(), place, t, a.Name, offered(roles))
	}

	inner := place + " role " + a.Name
	items := groupItems(a.Value)

	idx := "i"
	if _, _, _, _, segment, _, _ := ext.RoleValue(
		reg,
		t,
		x,
		a.Name,
		nil,
		nil,
	); segment == ext.SegmentKey {
		idx = "k"
	}

	if depth > 0 {
		idx += strconv.Itoa(depth + 1)
	}

	held, guard, over, value, segment, label, _ := ext.RoleValue(
		reg, t, x, a.Name, lang.Name(idx), lang.Name("e"),
	)

	if over == nil {
		s, err := w.items(vo, items, inner, held, value, w.locate(segment, label, wrap), depth)
		if s == nil || err != nil || guard == nil {
			return s, err
		}

		return lang.If(guard, s, nil), nil
	}

	s, err := w.items(vo, items, inner, held, value, w.locate(segment, label, wrap), depth+1)
	if s == nil || err != nil {
		return nil, err
	}

	m, isMap := types.Unalias(t).Underlying().(*types.Map)
	// The keys of a map are the values of k, which reads no element.
	keyRole := isMap && a.Name == "k"

	if isMap && typequery.BasicInfo(m.Key())&(types.IsInteger|types.IsString) != 0 {
		keys := lang.Expr("%s.Sorted(%s.Keys(%s))", w.Import("slices"), w.Import("maps"), over)
		if keyRole {
			return lang.ForRange("", idx, keys, s), nil
		}

		// over renders before e is declared in the loop, so that an over
		// spelling an outer e refers to that one.
		elem := lang.Stmt("%[2]s := %[1]s[%[3]s]", over, decl("e"), lang.Name(idx))

		return lang.ForRange("", idx, keys, genSteps{elem, s}), nil
	}

	if keyRole {
		return lang.ForRange(idx, "", over, s), nil
	}

	return lang.ForRange(idx, "e", over, s), nil
}

// items returns the steps of items, the items of a group or the value of
// a role item of vo, on the value x of type t. A positional item is a rule
// and a keyword item a role of t. place names what the items apply to in
// errors.
func (w *writer) items(
	vo ValidatorOptions,
	items []expr.Arg,
	place string,
	t types.Type,
	x genExpr,
	wrap wrapFunc,
	depth int,
) (genStep, error) {
	var steps genSteps

	for _, a := range items {
		var (
			s   genStep
			err error
		)

		if a.Name != "" {
			s, err = w.role(vo, a, place, t, x, wrap, depth)
		} else {
			s, err = w.heldRule(vo, a.Value, place, t, x, wrap)
		}

		if err != nil {
			return nil, err
		}

		if s != nil {
			steps = append(steps, s)
		}
	}

	if len(steps) == 0 {
		return nil, nil
	}

	return steps, nil
}

// heldRule returns the step running the rule n of vo on the value x of type
// t, a value a role tells. A rule reading the siblings of a field or
// validating a struct as a whole applies to a field or a type only.
func (w *writer) heldRule(
	vo ValidatorOptions,
	n expr.Node,
	place string,
	t types.Type,
	x genExpr,
	wrap wrapFunc,
) (genStep, error) {
	r, err := w.resolve(vo, n, place)
	if err != nil {
		return nil, err
	}

	_, sibling := r.(ext.SiblingRule)
	_, whole := r.(ext.StructRule)

	if sibling || whole && !acceptsHeld(w.sel.opts.Registry, r, t) {
		return nil, fmt.Errorf(
			"%w: %s: %s on %s: %s applies to a field or the doc comment of a type only",
			ErrDirective, vo.src.origin(), vo.src.spelling(), place, n,
		)
	}

	s, err := w.apply(r, t, x, wrap)
	if err != nil {
		return nil, ruleError(vo, place, err)
	}

	return s, nil
}

// groupItems returns the items n stands for as the value of a role item:
// the args of g(...), else n alone.
func groupItems(n expr.Node) []expr.Arg {
	if c, ok := n.(expr.Call); ok && c.Name == groupWord {
		return c.Args
	}

	return []expr.Arg{{Value: n}}
}

// checkRoles reports a role of roles, those of t, that an item of a
// directive cannot name: a word of //kama:validate naming no validator, the
// name of a validator of reg, or a word that is no name.
func checkRoles(reg *ext.Registry, t types.Type, roles []string) error {
	for _, role := range roles {
		_, isRule := reg.Lookup(role)

		if !expr.IsName(role) || isRule || slices.Contains(itemWords, role) {
			return fmt.Errorf(
				"%s has the role %s, which names a validator or a word of %s;"+
					" a type support must name its roles otherwise",
				t, role, GeneratorId().Directive(),
			)
		}
	}

	return nil
}

// offered spells the roles a type offers.
func offered(roles []string) string {
	if len(roles) == 0 {
		return "it holds no value a role names"
	}

	return "its roles are " + strings.Join(roles, ", ")
}
