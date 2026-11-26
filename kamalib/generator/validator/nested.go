package validator

import (
	"fmt"
	"go/types"
	"strconv"
	"strings"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// nested returns the step validating the values the value x of type t
// holds, each error passed through wrap, or nil when t holds nothing to
// validate. depth counts the loops around the step, which name their
// indices apart so that wrap can still refer to the outer ones.
//
// A value of a target or an implementor is validated by its Validate and a
// value of a type parameter by the function given for it. The walk goes
// through pointers, slices, arrays, map values, the values the types an
// genext.Holder or an genext.Container of the registry claims hold, the
// fields of struct type literals and named array and pointer types of
// packages outside the run, and never into the underlying type of another
// named type. A struct, slice or map type of the run holding something to
// validate by itself is a target itself, and so is a type of the run with
// validators of its own. A named pointer type, or a named array type
// without validators of its own, of the run holding something to validate,
// a type parameter of the receiver included, is an error. So is an instance of a generic struct,
// slice or
// map type of the run that is no target and whose type arguments hold
// something to validate. selectTargets reports the fields and types
// holding one. nestedNamed reports one a type argument reaches.
func (w *writer) nested(t types.Type, x genExpr, wrap wrapFunc, depth int) (genStep, error) {
	switch u := types.Unalias(t).(type) {
	case *types.TypeParam:
		fn := lang.Name(validateRole(w.tparams()[u.Index()]))

		return lang.If(lang.Expr("%s != nil", fn), w.check(wrap(lang.Expr(
			"%s(%s, %s)", fn, x, lang.Name("stopOnFirstError"),
		))), nil), nil
	case *types.Named:
		return w.nestedNamed(u, x, wrap, depth)
	case *types.Pointer:
		s, err := w.nested(u.Elem(), lang.Expr("(*%s)", x), wrap, depth)
		if s == nil || err != nil {
			return nil, err
		}

		return lang.If(lang.Expr("%s != nil", x), s, nil), nil
	case *types.Slice:
		return w.indexed(w.walker(u.Elem()), x, wrap, depth)
	case *types.Array:
		return w.indexed(w.walker(u.Elem()), x, wrap, depth)
	case *types.Map:
		return w.elements(w.walker(u.Elem()), x, wrap, depth, genext.SegmentKey, u.Key())
	case *types.Struct:
		return w.structFields(u, x, wrap, depth)
	}

	return nil, nil
}

// structFields returns the steps validating the values the fields of the
// struct type literal st of the value x hold, or nil when they hold
// nothing to validate. Directives and hooks do not apply to such fields.
func (w *writer) structFields(
	st *types.Struct,
	x genExpr,
	wrap wrapFunc,
	depth int,
) (genStep, error) {
	var steps genSteps

	for i := range st.NumFields() {
		v := st.Field(i)
		if !w.accessible(v) {
			continue
		}

		s, err := w.nested(
			v.Type(), lang.Expr("%s.%s", x, v.Name()), w.fieldWrap(st, i, wrap), depth,
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

// nestedNamed is nested for a named type.
func (w *writer) nestedNamed(
	named *types.Named,
	x genExpr,
	wrap wrapFunc,
	depth int,
) (genStep, error) {
	reg := w.registry()

	if h, ok := genext.HolderOf(reg, named); ok {
		_, held, guard, value, _ := h.Held(named, x)
		return w.guarded(held, guard, value, wrap, depth)
	}

	if c, ok := genext.ContainerOf(reg, named); ok {
		_, elem, over, segment, _ := c.Elements(named, x)
		if segment != genext.SegmentIndex {
			return w.elements(w.walker(elem), over, wrap, depth, segment, nil)
		}

		idx, local := loopName("i", depth), loopName("s", depth)

		s, err := w.nested(
			elem,
			containerElem(named, over, local, idx),
			w.locate(segment, lang.Name(idx), wrap),
			depth+1,
		)
		if s == nil || err != nil {
			return nil, err
		}

		return rangeContainer(named, over, idx, local, s), nil
	}

	if c, ok := genext.CompositeOf(reg, named); ok {
		return w.parts(c, named, x, wrap, depth)
	}

	switch {
	case w.shallow && w.sel.callable(named) && w.sel.decoded(named):
		return nil, nil
	case w.sel.callable(named):
		call, err := w.call(named, x)
		if err != nil {
			return nil, err
		}

		return w.check(wrap(call)), nil
	case w.sel.throughForeign(named):
		return w.throughForeign(named, x, wrap, depth)
	case w.sel.unsupported(named, true):
		return nil, unsupportedError(named)
	}

	return nil, nil
}

// throughForeign is nested for a named pointer or array type of a package
// outside the run, which nested walks through as through its underlying
// type. It returns nil for a type holding nothing to validate before
// walking it, so that a deep chain of such types holding nothing costs no
// walk per path. A type holding itself, as in
// type L *struct{ Next L; I Impl }, would need a recursive function, which
// the generator does not write, so it is an error when it holds something
// to validate.
func (w *writer) throughForeign(
	named *types.Named,
	x genExpr,
	wrap wrapFunc,
	depth int,
) (genStep, error) {
	trimmed := w.sel.trim()

	holds := w.sel.reachesLeaf(
		named.Underlying(),
		w.sel.holdsLeaf(trimmed, true, w.sel.unsupportedFunc(trimmed)),
	)
	if !holds {
		return nil, nil
	}

	if w.sel.reaches(named.Underlying(), func(n *types.Named) bool {
		return types.Identical(n, named)
	}) {
		return nil, fmt.Errorf(
			"%w: %s is a named %s type of another package holding itself and values to"+
				" validate; Validate cannot walk through it",
			ErrUnsupported, named, kindName(named.Underlying()),
		)
	}

	return w.nested(named.Underlying(), x, wrap, depth)
}

// guarded returns the step validating the value of type held of the
// expression value when guard tells it is held.
func (w *writer) guarded(
	held types.Type,
	guard, value genExpr,
	wrap wrapFunc,
	depth int,
) (genStep, error) {
	s, err := w.nested(held, value, wrap, depth)
	if s == nil || err != nil {
		return nil, err
	}

	return lang.If(guard, s, nil), nil
}

// parts returns the steps validating the values the value x of type named
// holds as c tells, each under its guard and located by its segment, or nil
// when they hold nothing to validate.
func (w *writer) parts(
	c genext.Composite,
	named *types.Named,
	x genExpr,
	wrap wrapFunc,
	depth int,
) (genStep, error) {
	var steps genSteps

	for _, role := range c.Roles(named) {
		part, guard, value, segment, label, _ := c.Part(named, x, role)

		s, err := w.nested(part, value, w.locate(segment, label, wrap), depth)
		if err != nil {
			return nil, err
		}

		if s == nil {
			continue
		}

		if guard != nil {
			s = lang.If(guard, s, nil)
		}

		steps = append(steps, s)
	}

	if len(steps) == 0 {
		return nil, nil
	}

	return steps, nil
}

// locate returns wrap adding the segment of the kind segment labelled label
// to an error first.
func (w *writer) locate(segment genext.Segment, label genExpr, wrap wrapFunc) wrapFunc {
	var fn string

	switch segment {
	case genext.SegmentIndex:
		fn = "Index"
	case genext.SegmentKey:
		fn = "Key"
	case genext.SegmentField:
		fn = "Field"
	default:
		return wrap
	}

	return func(err genExpr) genExpr {
		return wrap(lang.Expr("%s.%s(%s, %s)", w.Import(ext.ValidationPath), fn, err, label))
	}
}

// elementFunc returns the step validating the element e, each error passed
// through wrap, or nil when e holds nothing to validate.
type elementFunc func(e genExpr, wrap wrapFunc, depth int) (genStep, error)

// walker returns the elementFunc of elements of type elem, which nested
// walks.
func (w *writer) walker(elem types.Type) elementFunc {
	return func(e genExpr, wrap wrapFunc, depth int) (genStep, error) {
		return w.nested(elem, e, wrap, depth)
	}
}

// indexed returns the loop validating each element of the slice or array
// x by element, located by its index. The element is spelled x[i], so that
// a Validate on the pointer receiver runs on the element in place rather
// than on a copy in a range variable. An x calling a method is bound to a
// local once, so that the method runs once, and the element is spelled by
// index into the local.
func (w *writer) indexed(
	element elementFunc,
	x genExpr,
	wrap wrapFunc,
	depth int,
) (genStep, error) {
	idx, local := loopName("i", depth), loopName("s", depth)

	s, err := element(
		indexElem(x, idx, local), w.locate(genext.SegmentIndex, lang.Name(idx), wrap), depth+1,
	)
	if s == nil || err != nil {
		return nil, err
	}

	return rangeIndexed(x, idx, local, s), nil
}

// indexElem returns the element at idx of the slice or array x, as
// rangeIndexed declares the loop variables: x[idx], or local[idx] when x
// calls a method.
func indexElem(x genExpr, idx, local string) genExpr {
	return func(c genCtx) (string, error) {
		text, err := x(c)
		if err != nil {
			return "", err
		}

		if callsMethod(text) {
			return lang.Expr("%s[%s]", lang.Name(local), lang.Name(idx))(c)
		}

		return lang.Expr("%s[%s]", text, lang.Name(idx))(c)
	}
}

// rangeIndexed returns the loop over the indices of the slice or array x,
// declaring idx, as indexed spells the element. An x calling a method is
// first bound to local, which the loop ranges over.
func rangeIndexed(x genExpr, idx, local string, body genStep) genStep {
	return genhelper.FuncStep[genCtx](func(c genCtx, b []byte) (genCtx, []byte, error) {
		text, err := x(c)
		if err != nil {
			return c, nil, err
		}

		if !callsMethod(text) {
			return lang.ForRange(idx, "", x, body).Render(c, b)
		}

		return genSteps{
			lang.Stmt("%[2]s := %[1]s", x, decl(local)),
			lang.ForRange(idx, "", lang.Name(local), body),
		}.Render(c, b)
	})
}

// containerElem returns the element at idx of the elements over yields for
// a container of type t, as rangeContainer declares the loop variables:
// local[idx] when over calls a method of t returning a slice or an array,
// else the range variable e.
func containerElem(t types.Type, over genExpr, local, idx string) genExpr {
	return func(c genCtx) (string, error) {
		text, err := over(c)
		if err != nil {
			return "", err
		}

		if indexedResult(t, text) {
			return lang.Expr("%s[%s]", lang.Name(local), lang.Name(idx))(c)
		}

		return lang.Name("e")(c)
	}
}

// rangeContainer returns the loop over the elements over yields for a
// container of type t, declaring idx and e, or binding over to local and
// ranging over its indices when indexedResult tells, as containerElem
// spells the element.
func rangeContainer(t types.Type, over genExpr, idx, local string, body genStep) genStep {
	return genhelper.FuncStep[genCtx](func(c genCtx, b []byte) (genCtx, []byte, error) {
		text, err := over(c)
		if err != nil {
			return c, nil, err
		}

		if !indexedResult(t, text) {
			return lang.ForRange(idx, "e", over, body).Render(c, b)
		}

		return genSteps{
			lang.Stmt("%[2]s := %[1]s", over, decl(local)),
			lang.ForRange(idx, "", lang.Name(local), body),
		}.Render(c, b)
	})
}

// indexedResult reports whether text, the range expression a type support
// gives for a value of type t, calls a method of t taking no args whose
// result is a slice or an array. Ranging over such a result with a range
// variable would copy each element, so the walk indexes it instead.
func indexedResult(t types.Type, text string) bool {
	head, ok := strings.CutSuffix(text, "()")
	if !ok {
		return false
	}

	name := head[strings.LastIndex(head, ".")+1:]

	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, name)

	fn, ok := obj.(*types.Func)
	if !ok || fn.Signature().Params().Len() != 0 || fn.Signature().Results().Len() != 1 {
		return false
	}

	switch fn.Signature().Results().At(0).Type().Underlying().(type) {
	case *types.Slice, *types.Array:
		return true
	}

	return false
}

// callsMethod reports whether the expression text calls a method. The
// values the walk reaches are variables, fields, index expressions,
// dereferences and results of methods taking no args, which alone spell
// ().
func callsMethod(text string) bool { return strings.Contains(text, "()") }

// elements returns the loop validating each element of x by element,
// located by what the range over x yields first as segment tells. key is
// the key type of a map x, nil for any other x.
//
// Integer and string keys of a map are visited in sorted order. The first error and
// the order of the errors are then stable. Other keys are visited in the
// order of range. A lookup by a NaN float key never finds its value. The
// other kinds have no order to sort by.
func (w *writer) elements(
	element elementFunc,
	x genExpr,
	wrap wrapFunc,
	depth int,
	segment genext.Segment,
	key types.Type,
) (genStep, error) {
	idx := "i"
	if segment == genext.SegmentKey {
		idx = "k"
	}

	if depth > 0 {
		idx += strconv.Itoa(depth + 1)
	}

	inner := w.locate(segment, lang.Name(idx), wrap)

	s, err := element(lang.Name("e"), inner, depth+1)
	if s == nil || err != nil {
		return nil, err
	}

	if key == nil || typequery.BasicInfo(key)&(types.IsInteger|types.IsString) == 0 {
		return lang.ForRange(idx, "e", x, s), nil
	}

	return w.rangeSortedKeys(x, idx, loopName("m", depth), false, s), nil
}

// rangeSortedKeys returns the loop over the sorted keys of the map m,
// declaring idx. Unless keyOnly, the body is preceded by e := m[idx]. An m
// calling a method is first bound to local, so that the method runs once
// rather than once per key.
func (w *writer) rangeSortedKeys(
	m genExpr,
	idx, local string,
	keyOnly bool,
	body genStep,
) genStep {
	slicesPkg, mapsPkg := w.Import("slices"), w.Import("maps")

	return genhelper.FuncStep[genCtx](func(c genCtx, b []byte) (genCtx, []byte, error) {
		text, err := m(c)
		if err != nil {
			return c, nil, err
		}

		var steps genSteps

		over := m
		if !keyOnly && callsMethod(text) {
			steps = append(steps, lang.Stmt("%[2]s := %[1]s", m, decl(local)))
			over = lang.Name(local)
		}

		keys := lang.Expr("%s.Sorted(%s.Keys(%s))", slicesPkg, mapsPkg, over)

		if keyOnly {
			return append(steps, lang.ForRange("", idx, keys, body)).Render(c, b)
		}

		// over renders before e is declared in the loop, so that an over
		// spelling an outer e refers to that one.
		value := lang.Stmt("%[2]s := %[1]s[%[3]s]", over, decl("e"), lang.Name(idx))

		return append(steps, lang.ForRange("", idx, keys, genSteps{value, body})).Render(c, b)
	})
}

// call returns the call of Validate on the value x of the target or
// implementor named, passing the function each type argument needs. In
// ValidateShallow it calls ValidateShallow when named has one. An
// implementor whose Validate takes no stopOnFirstError is called without
// args. A Validate on the pointer receiver, as a target of a struct type
// has, is called on x in place, or on a copy of x when x holds the result
// of a call, which has no address.
func (w *writer) call(named *types.Named, x genExpr) (genExpr, error) {
	method := "Validate"
	if w.shallow && w.sel.hasShallow(named) {
		method = shallowName
	}

	fn, onPointer, kama := w.sel.validateMethod(named)
	if fn == nil {
		onPointer = onPointerReceiver(named)
	}

	var args []genExpr

	if fn == nil || kama {
		args = append(args, lang.Name("stopOnFirstError"))

		for arg := range named.TypeArgs().Types() {
			f, err := w.typeArgFunc(arg)
			if err != nil {
				return nil, err
			}

			args = append(args, f)
		}
	}

	if !onPointer {
		return lang.Expr("%s.%s(%s)", x, method, lang.Join(", ", args...)), nil
	}

	return func(c genCtx) (string, error) {
		text, err := x(c)
		if err != nil {
			return "", err
		}

		// Go takes the address of an addressable operand of a call of a
		// method on the pointer receiver itself, as it does of (*p) for a
		// pointer p, so the call copies nothing.
		if !callsMethod(text) {
			return lang.Expr("%s.%s(%s)", text, method, lang.Join(", ", args...))(c)
		}

		return lang.Expr("func() error {\n%s := %s\nreturn %s.%s(%s)\n}()",
			decl("p"), text, lang.Name("p"), method, lang.Join(", ", args...))(c)
	}, nil
}

// typeArgFunc returns the function validating values of the type argument
// arg: the function of the receiver for its own type parameter, a function
// literal for a type holding something to validate, or nil. The literal
// validates as Validate does in ValidateShallow too, since a function for a
// type parameter validates in full whichever method receives it.
func (w *writer) typeArgFunc(arg types.Type) (genExpr, error) {
	if tp, ok := types.Unalias(arg).(*types.TypeParam); ok {
		return lang.Name(validateRole(w.tparams()[tp.Index()])), nil
	}

	if w.shallow {
		deep := *w
		deep.shallow = false

		return deep.typeArgFunc(arg)
	}

	var body []genStep

	if named, ok := types.Unalias(arg).(*types.Named); ok && w.sel.callable(named) {
		call, err := w.call(named, lang.Name("v"))
		if err != nil {
			return nil, err
		}

		body = []genStep{lang.Stmt("return %s", call)}
	} else {
		s, err := w.nested(arg, lang.Name("v"), noWrap, 0)
		if s == nil || err != nil {
			return lang.Expr("nil"), err
		}

		body = w.collect([]genStep{s})
	}

	return func(c genCtx) (string, error) {
		typ, err := w.TypeString(arg)
		if err != nil {
			return "", err
		}
		// The literal has the parameters of a Validate, under their names
		// unless a type parameter has one.
		c = genhelper.Push(c)

		params, err := lang.Expr("%s %s, %s bool",
			w.local("v"), typ, w.local("stopOnFirstError"))(c)
		if err != nil {
			return "", err
		}

		text, err := lang.Render(c, body...)
		if err != nil {
			return "", err
		}

		return "func(" + params + ") error {\n" + text + "}", nil
	}, nil
}
