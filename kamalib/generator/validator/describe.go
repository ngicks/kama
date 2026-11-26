package validator

import (
	"fmt"
	"go/token"
	"go/types"
	"slices"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// levelWords are the keyword items of a field directive or of g(...) that
// tell how the walk reaches the values of the level, rather than naming a
// role: the switch of the nested Validate call and the description of what
// a value holds.
var levelWords = []string{ext.NestedWord, ext.UnwrapWord, ext.IterWord, ext.Iter2Word}

// descriptionWords are the keyword items naming the method by which a
// value yields what it holds: unwrap for one value, iter for a sequence and
// iter2 for a sequence of pairs.
var descriptionWords = []string{ext.UnwrapWord, ext.IterWord, ext.Iter2Word}

// fieldItems returns the items of the directives and the option file entry
// of the field v of the target.
func (w *writer) fieldItems(v *types.Var) []expr.Arg {
	return opiter.SlicesValues(w.sel.loadedOptions.Option.Field(w.n.Id, v.Name()).Validators).
		Map(func(vo ValidatorOptions) expr.Arg { return vo.Item }).
		CollectSlice()
}

// described reports whether the directives or the option file entry of the
// field of the type id describe what the field holds.
func (s *selection) described(id typegraph.QualifiedType, field string) bool {
	word, _, _ := description(
		opiter.SlicesValues(s.loadedOptions.Option.Field(id, field).Validators).
			Map(func(vo ValidatorOptions) expr.Arg { return vo.Item }).
			CollectSlice(),
	)

	return word != ""
}

// description returns the description among items, its word and the name
// of the method it names, or "" when items hold none.
func description(items []expr.Arg) (word, method string, err error) {
	for _, a := range items {
		if !slices.Contains(descriptionWords, a.Name) {
			continue
		}

		if word != "" {
			return "", "", fmt.Errorf(
				"%s and %s describe one value; write one of them",
				word,
				a.Name,
			)
		}

		name, ok := a.Value.(expr.String)
		if !ok || !token.IsIdentifier(name.Value) {
			return "", "", fmt.Errorf("%s; want %s=\"<method name>\"", a, a.Name)
		}

		word, method = a.Name, name.Value
	}

	return word, method, nil
}

// nestedOn returns the switch of the nested Validate call items set by
// nested=True or nested=False, else on.
func nestedOn(items []expr.Arg, on bool) (bool, error) {
	for _, a := range items {
		if a.Name != ext.NestedWord {
			continue
		}

		b, ok := a.Value.(expr.Bool)
		if !ok {
			return false, fmt.Errorf("%s; want nested=True or nested=False", a)
		}

		on = b.Value
	}

	return on, nil
}

// reshapes reports whether items, or a group of a role item among them at
// any depth, set the nested switch or describe a value, which the nested
// walk then follows level by level.
func reshapes(items []expr.Arg) bool {
	return opiter.SlicesValues(items).
		Any(func(a expr.Arg) bool {
			if slices.Contains(levelWords, a.Name) {
				return true
			}

			c, ok := a.Value.(expr.Call)

			return ok && a.Name != "" && c.Name == ext.GroupWord && reshapes(c.Args)
		})
}

// roleItems returns the items of the group of the role item of items named
// role, or nil when items hold none.
func roleItems(items []expr.Arg, role string) []expr.Arg {
	i := slices.IndexFunc(items, func(a expr.Arg) bool { return a.Name == role })
	if i < 0 {
		return nil
	}

	return groupItems(items[i].Value)
}

// heldRoles returns the roles of the values a value of type t holds: those
// the description among items tells, else those of genext.Roles.
func (w *writer) heldRoles(t types.Type, items []expr.Arg) ([]string, error) {
	word, method, err := description(items)
	if err != nil {
		return nil, err
	}

	switch word {
	case "":
		return genext.Roles(w.sel.opts.Registry, t), nil
	case ext.Iter2Word:
		if _, _, err := iterated(t, w.scope.Pkg.Types, word, method); err != nil {
			return nil, err
		}

		return []string{"k", "v"}, nil
	}

	if _, _, err := iterated(t, w.scope.Pkg.Types, word, method); err != nil {
		return nil, err
	}

	return []string{"v"}, nil
}

// iterated returns the types of the values the method named method of t
// yields as word describes it: the value of unwrap, with ok true for a
// result (T, bool), the element of iter, and the key and the value of
// iter2. The result of iter and iter2 is a function ranged over, as
// func(yield func(T) bool) is, named or not.
//
// pkg is the package of the generated code, which reaches the unexported
// methods of its own types.
func iterated(
	t types.Type,
	pkg *types.Package,
	word, method string,
) (key, value types.Type, err error) {
	shapes := map[string]string{
		ext.UnwrapWord: "func() T or func() (T, bool)",
		ext.IterWord:   "func() S, S a function func(yield func(T) bool)",
		ext.Iter2Word:  "func() S, S a function func(yield func(K, V) bool)",
	}

	sel := types.NewMethodSet(t).Lookup(pkg, method)

	if sel == nil {
		return nil, nil, fmt.Errorf("%s=%q: %s has no method %s; want %s %s",
			word, method, t, method, method, shapes[word])
	}

	sig := sel.Type().(*types.Signature)
	wrong := fmt.Errorf("%s=%q: method %s of %s has signature %s; want %s",
		word, method, method, t, sig, shapes[word])

	if sig.Params().Len() != 0 || sig.Variadic() {
		return nil, nil, wrong
	}

	res := sig.Results()

	if word == ext.UnwrapWord {
		switch {
		case res.Len() == 1:
			return nil, res.At(0).Type(), nil
		case res.Len() == 2 && types.Identical(res.At(1).Type(), types.Typ[types.Bool]):
			return types.Typ[types.Bool], res.At(0).Type(), nil
		}

		return nil, nil, wrong
	}

	if res.Len() != 1 {
		return nil, nil, wrong
	}

	seq, ok := res.At(0).Type().Underlying().(*types.Signature)
	if !ok || seq.Params().Len() != 1 || seq.Results().Len() != 0 {
		return nil, nil, wrong
	}

	yield, ok := seq.Params().At(0).Type().Underlying().(*types.Signature)
	if !ok || yield.Results().Len() != 1 ||
		!types.Identical(yield.Results().At(0).Type(), types.Typ[types.Bool]) {
		return nil, nil, wrong
	}

	switch {
	case word == ext.IterWord && yield.Params().Len() == 1:
		return nil, yield.Params().At(0).Type(), nil
	case word == ext.Iter2Word && yield.Params().Len() == 2:
		return yield.Params().At(0).Type(), yield.Params().At(1).Type(), nil
	}

	return nil, nil, wrong
}

// described is over for the value x of type t that the description word
// method tells.
func (w *writer) described(
	t types.Type,
	x genExpr,
	word, method, role string,
	wrap wrapFunc,
	depth int,
	inner heldFunc,
) (genStep, error) {
	key, value, err := iterated(t, w.scope.Pkg.Types, word, method)
	if err != nil {
		return nil, err
	}

	call := lang.Expr("%s.%s()", x, method)

	if word == ext.UnwrapWord {
		if key == nil {
			return inner(value, call, wrap, depth)
		}

		u, ok := loopName("u", depth), loopName("ok", depth)

		s, err := inner(value, lang.Name(u), wrap, depth)
		if s == nil || err != nil {
			return nil, err
		}

		return block(genSteps{
			lang.Stmt("%s, %s := %s", decl(u), decl(ok), call),
			lang.If(lang.Name(ok), s, nil),
		}), nil
	}

	// A key of a string or an integer kind locates its value, as a map key
	// does. Any other value is located by its position in the sequence.
	keyed := word == ext.Iter2Word && typequery.BasicInfo(key)&(types.IsInteger|types.IsString) != 0
	k, e, i := loopName("k", depth), loopName("e", depth), loopName("i", depth)

	held, v, label, segment := value, lang.Name(e), lang.Name(i), genext.SegmentIndex
	if role == "k" {
		held, v = key, lang.Name(k)
	}

	if keyed {
		label, segment = lang.Name(k), genext.SegmentKey
	}

	s, err := inner(held, v, w.locate(segment, label, wrap), depth+1)
	if s == nil || err != nil {
		return nil, err
	}

	keyVar, elemVar := "", e

	switch {
	case word == ext.IterWord:
		keyVar, elemVar = e, ""
	case role == "k":
		keyVar, elemVar = k, ""
	case keyed:
		keyVar = k
	}

	if keyed {
		return lang.ForRange(keyVar, elemVar, call, s), nil
	}

	return block(genSteps{
		lang.Stmt("%s := 0", decl(i)),
		lang.ForRange(keyVar, elemVar, call, genSteps{s, lang.Stmt("%s++", lang.Name(i))}),
	}), nil
}

// heldNested returns the nested walk of the value x of type t, as nested
// gives it, directed by items, the items of a field or of a group: the
// switch nested=False, or nested=True below it, and a description of what
// the value holds. on tells the switch the levels above set. Items that
// direct no level below leave the walk to nested as a whole.
func (w *writer) heldNested(
	t types.Type,
	x genExpr,
	wrap wrapFunc,
	depth int,
	items []expr.Arg,
	on bool,
) (genStep, error) {
	on, err := nestedOn(items, on)
	if err != nil {
		return nil, err
	}

	word, _, err := description(items)
	if err != nil {
		return nil, err
	}

	if word == "" && (!reshapes(items) || w.callable(t)) {
		if !on {
			return nil, nil
		}

		return w.nested(t, x, wrap, depth)
	}

	roles, err := w.heldRoles(t, items)
	if err != nil {
		return nil, err
	}

	var steps genSteps

	for _, role := range roles {
		// Keys hold nothing the nested walk validates, as for a map.
		if role == "k" {
			continue
		}

		sub := roleItems(items, role)

		s, err := w.over(t, x, items, role, wrap, depth,
			func(held types.Type, value genExpr, wrap wrapFunc, depth int) (genStep, error) {
				return w.heldNested(held, value, wrap, depth, sub, on)
			},
		)
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

// callable reports whether nested validates a value of type t by a call
// rather than by walking what it holds.
func (w *writer) callable(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && w.sel.callable(named)
}

// block returns steps in a block of their own, whose declarations end with
// it.
func block(steps genSteps) genStep {
	return genhelper.FuncStep[genCtx](func(c genCtx, b []byte) (genCtx, []byte, error) {
		outer := c.Scope()
		c = genhelper.Push(c)

		c, body, err := steps.Render(c, nil)
		if err != nil {
			return c, nil, err
		}

		p := c.Printer(b)
		p.Printf("{\n%s}\n", body)

		return c.WithScope(outer), p.Bytes(), nil
	})
}
