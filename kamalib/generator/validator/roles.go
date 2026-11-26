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

// role returns the step applying the value of the item a, a role keyword
// item of vo, to the values of that role of the value x of type t, each
// error passed through wrap. level holds the items of the level a belongs
// to, whose description tells the roles of t in place of the type
// supports. place names what a applies to in errors. depth counts the
// loops around the step, as nested does.
func (w *writer) role(
	vo ValidatorOptions,
	a expr.Arg,
	level []expr.Arg,
	place string,
	t types.Type,
	x genExpr,
	wrap wrapFunc,
	depth int,
) (genStep, error) {
	reg := w.sel.opts.Registry

	if slices.Contains(ext.ItemWords, a.Name) {
		return nil, fmt.Errorf("%w: %s: %s on %s: %s is written on a field or inside %s(...)",
			ErrDirective, vo.src.origin(), vo.src.spelling(), place, a.Name, ext.GroupWord)
	}

	roles, err := w.heldRoles(t, level)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %s on %s: %w",
			ErrDirective, vo.src.origin(), vo.src.spelling(), place, err)
	}

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

	return w.over(t, x, level, a.Name, wrap, depth,
		func(held types.Type, value genExpr, wrap wrapFunc, depth int) (genStep, error) {
			return w.items(vo, items, inner, held, value, wrap, depth)
		},
	)
}

// heldFunc returns the step for value, a value of type held a role tells,
// each error passed through wrap, or nil for none.
type heldFunc func(held types.Type, value genExpr, wrap wrapFunc, depth int) (genStep, error)

// over returns the step running inner on each value of role the value x of
// type t holds: under its guard for a role of one value, in a loop for a
// role of many, each located by its segment. The description among level,
// the items of the level, tells the values in place of the type supports.
func (w *writer) over(
	t types.Type,
	x genExpr,
	level []expr.Arg,
	role string,
	wrap wrapFunc,
	depth int,
	inner heldFunc,
) (genStep, error) {
	word, method, err := description(level)
	if err != nil {
		return nil, err
	}

	if word != "" {
		return w.described(t, x, word, method, role, wrap, depth, inner)
	}

	reg := w.sel.opts.Registry

	idx := "i"

	_, _, over, _, segment, _, _ := ext.RoleValue(reg, t, x, role, nil, nil)
	if segment == ext.SegmentKey {
		idx = "k"
	}

	idx, local := loopName(idx, depth), loopName("s", depth)

	_, indexable := types.Unalias(t).Underlying().(*types.Slice)
	if _, ok := types.Unalias(t).Underlying().(*types.Array); ok {
		indexable = true
	}

	elem := lang.Name("e")

	switch {
	case indexable:
		elem = indexElem(x, idx, local)
	case over != nil && segment == ext.SegmentIndex:
		elem = containerElem(t, over, local, idx)
	}

	held, guard, over, value, segment, label, _ := ext.RoleValue(
		reg, t, x, role, lang.Name(idx), elem,
	)

	if over == nil {
		s, err := inner(held, value, w.locate(segment, label, wrap), depth)
		if s == nil || err != nil || guard == nil {
			return s, err
		}

		return lang.If(guard, s, nil), nil
	}

	s, err := inner(held, value, w.locate(segment, label, wrap), depth+1)
	if s == nil || err != nil {
		return nil, err
	}

	if indexable {
		return rangeIndexed(over, idx, local, s), nil
	}

	m, isMap := types.Unalias(t).Underlying().(*types.Map)
	// The keys of a map are the values of k, which reads no element.
	keyRole := isMap && role == "k"

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

	if segment == ext.SegmentIndex {
		return rangeContainer(t, over, idx, local, s), nil
	}

	return lang.ForRange(idx, "e", over, s), nil
}

// loopName returns the name of a loop variable of the role name at depth,
// apart from those of the loops around it.
func loopName(name string, depth int) string {
	if depth > 0 {
		return name + strconv.Itoa(depth+1)
	}

	return name
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

		switch {
		case slices.Contains(levelWords, a.Name):
		case a.Name != "":
			s, err = w.role(vo, a, items, place, t, x, wrap, depth)
		default:
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
	if c, ok := n.(expr.Call); ok && c.Name == ext.GroupWord {
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

		if !expr.IsName(role) || isRule || slices.Contains(ext.ItemWords, role) {
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
