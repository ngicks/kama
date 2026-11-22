package validator

import (
	"fmt"
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

// nested returns the step validating the values the value x of type t
// holds, each error passed through wrap, or nil when t holds nothing to
// validate. depth counts the loops around the step, which name their
// indices apart so that wrap can still refer to the outer ones.
//
// A value of a target or an implementor is validated by its Validate and a
// value of a type parameter by the function given for it. The walk goes
// through pointers, slices, arrays, map values, the option types and the
// fields of struct type literals, and never into the underlying type of a
// named type: a type of the run holding
// something to validate is a target itself.
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
		return w.elements(u.Elem(), x, wrap, depth, false)
	case *types.Array:
		return w.elements(u.Elem(), x, wrap, depth, false)
	case *types.Map:
		return w.elements(u.Elem(), x, wrap, depth, true)
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
	switch {
	case isNamed(named, optPath, "Option"):
		return w.guarded(typeArg(named), lang.Expr("%s.IsSome()", x), x, wrap, depth)
	case isNamed(named, undPath, "Und"):
		return w.guarded(typeArg(named), lang.Expr("%s.IsDefined()", x), x, wrap, depth)
	case isNamed(named, elasticPath, "Elastic"):
		// Options holds an opt.Option per element, none for a null one.
		return w.elements(named, lang.Expr("%s.Options()", x), wrap, depth, false)
	case w.sel.callable(named):
		call, err := w.call(named, x)
		if err != nil {
			return nil, err
		}

		return w.check(wrap(call)), nil
	case w.sel.pointerImplementor(named):
		return nil, fmt.Errorf(
			"%w: %s declares Validate on its pointer receiver; declare it on the value receiver",
			ErrUnsupported, named,
		)
	}

	return nil, nil
}

// guarded returns the step validating the value of type elem that the
// option x holds when cond tells it holds one.
func (w *writer) guarded(
	elem types.Type,
	cond, x genExpr,
	wrap wrapFunc,
	depth int,
) (genStep, error) {
	s, err := w.nested(elem, lang.Expr("%s.Value()", x), wrap, depth)
	if s == nil || err != nil {
		return nil, err
	}

	return lang.If(cond, s, nil), nil
}

// elements returns the loop validating the elements of x, whose element
// type is elem: map values keyed by their keys when isMap is set, else
// elements by their indices. For an elastic.Elastic, elem is the
// elastic.Elastic type itself and the elements are options.
func (w *writer) elements(
	elem types.Type,
	x genExpr,
	wrap wrapFunc,
	depth int,
	isMap bool,
) (genStep, error) {
	idx, segment := "i", "Index"
	if isMap {
		idx, segment = "k", "Key"
	}

	if depth > 0 {
		idx += strconv.Itoa(depth + 1)
	}

	inner := func(err genExpr) genExpr {
		return wrap(lang.Expr(
			"%s.%s(%s, %s)", w.ident(validationPath), segment, err, lang.Name(idx),
		))
	}

	var (
		s   genStep
		err error
	)

	if isNamed(elem, elasticPath, "Elastic") {
		s, err = w.guarded(typeArg(elem), lang.Expr("%s.IsSome()", lang.Name("e")),
			lang.Name("e"), inner, depth+1)
	} else {
		s, err = w.nested(elem, lang.Name("e"), inner, depth+1)
	}

	if s == nil || err != nil {
		return nil, err
	}

	return lang.ForRange(idx, "e", x, s), nil
}

// call returns the call of Validate on the value x of the target or
// implementor named, passing the function each type argument needs.
func (w *writer) call(named *types.Named, x genExpr) (genExpr, error) {
	args := []genExpr{lang.Name("stopOnFirstError")}

	for arg := range named.TypeArgs().Types() {
		fn, err := w.typeArgFunc(arg)
		if err != nil {
			return nil, err
		}

		args = append(args, fn)
	}

	return lang.Expr("%s.Validate(%s)", x, lang.Join(", ", args...)), nil
}

// typeArgFunc returns the function validating values of the type argument
// arg: the function of the receiver for its own type parameter, a function
// literal for a type holding something to validate, or nil.
func (w *writer) typeArgFunc(arg types.Type) (genExpr, error) {
	if tp, ok := types.Unalias(arg).(*types.TypeParam); ok {
		return lang.Name(validateRole(w.tparams()[tp.Index()])), nil
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
		// The literal has the parameters of a Validate, under their names.
		c = genhelper.Push(c)

		params, err := lang.Expr("%s %s, %s bool",
			genhelper.Shadow("v"), typ, genhelper.Shadow("stopOnFirstError"))(c)
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
