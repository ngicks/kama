package validator

import (
	"fmt"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// hookName returns the name of the hook of the field v: validate followed
// by the name of v with its first letter upper-cased, as in validateName
// for Name and for name.
func hookName(v *types.Var) string {
	r, size := utf8.DecodeRuneInString(v.Name())
	return "validate" + string(unicode.ToUpper(r)) + v.Name()[size:]
}

// checkHook reports a hook of the field v of the struct target n that
// Validate cannot call: a missing one and one of another signature than
// func(bool) error. Validate of a struct target has the pointer receiver,
// so it calls a hook on either receiver.
func (s *selection) checkHook(n *typegraph.Node, v *types.Var) error {
	named := n.Detail.Type
	name := hookName(v)
	pkg := named.Obj().Pkg()

	recv := named.Obj().Name()
	if named.TypeParams().Len() > 0 {
		recv += "[" + strings.Join(typeParamNames(named), ", ") + "]"
	}

	fail := func(problem string) error {
		return fmt.Errorf(
			"%w: %s: field %s of %s %s; declare func (%s) %s(stopOnFirstError bool) error"+
				" or turn the hook off with //kama:validate hook=False on the field or the type",
			ErrHook, n.Detail.Pkg.Fset.Position(v.Pos()), v.Name(), n.Id, problem, recv, name,
		)
	}

	sel := types.NewMethodSet(types.NewPointer(named)).Lookup(pkg, name)
	if sel == nil {
		return fail("has no hook " + name)
	}

	sig := sel.Type().(*types.Signature)
	if sig.Variadic() || sig.Params().Len() != 1 || !returnsError(sig) ||
		!types.Identical(sig.Params().At(0).Type(), types.Typ[types.Bool]) {
		return fail(fmt.Sprintf("has its hook %s with signature %s", name, sig))
	}

	return nil
}

// typeOptions returns the options of the doc comment or the option file
// entry of the type id, without those of its type group.
func (s *selection) typeOptions(id typegraph.QualifiedType) TypeOptions {
	to, _ := s.loadedOptions.Option.Type(id)
	return to.Type
}

// checkTypeFunc reports the function name that pre or post of the target n
// names, set at src, when Validate cannot call it: a missing one and one of
// another signature than func(T, bool) error, with T the type of n, or
// func(*T, bool) error for a struct target. A generic n needs a function
// with type parameters of the same number that takes n instantiated with
// them.
func (s *selection) checkTypeFunc(n *typegraph.Node, name string, src optionSource) error {
	named := n.Detail.Type
	pkg := named.Obj().Pkg()
	tps := opiter.WrapSeq(named.TypeParams().TypeParams()).
		Map(func(tp *types.TypeParam) string {
			return tp.Obj().Name() + " " + types.TypeString(tp.Constraint(), types.RelativeTo(pkg))
		}).
		CollectSlice()

	want := "func " + name
	recv := named.Obj().Name()

	if len(tps) > 0 {
		want += "[" + strings.Join(tps, ", ") + "]"
		recv += "[" + strings.Join(typeParamNames(named), ", ") + "]"
	}

	fail := func(problem string) error {
		return fmt.Errorf(
			"%w: %s: %s on %s %s; declare %s(v %s, stopOnFirstError bool) error",
			ErrHook, src.origin(), src.spelling(), n.Id, problem, want, recv,
		)
	}

	fn, ok := pkg.Scope().Lookup(name).(*types.Func)
	if !ok {
		return fail("names no package-level function " + name)
	}

	sig := fn.Type().(*types.Signature)
	if ok, pointer := takesValue(sig, named); !ok || pointer && !onPointerReceiver(named) {
		return fail(fmt.Sprintf("names %s with signature %s", name, sig))
	}

	return nil
}

// typeFuncTakesPointer reports whether the function name, which pre or
// post of the target n names and checkTypeFunc accepts, takes a pointer to
// the value.
func (s *selection) typeFuncTakesPointer(n *typegraph.Node, name string) bool {
	named := n.Detail.Type
	fn := named.Obj().Pkg().Scope().Lookup(name).(*types.Func)
	_, pointer := takesValue(fn.Type().(*types.Signature), named)

	return pointer
}

// takesValue reports whether sig is func(named, bool) error, or
// func(*named, bool) error, which pointer tells. For a generic named, sig
// declares as many type parameters, and named instantiated with them is
// its first parameter.
func takesValue(sig *types.Signature, named *types.Named) (ok, pointer bool) {
	if sig.Variadic() || sig.Params().Len() != 2 || !returnsError(sig) ||
		!types.Identical(sig.Params().At(1).Type(), types.Typ[types.Bool]) ||
		sig.TypeParams().Len() != named.TypeParams().Len() {
		return false, false
	}

	first, value := sig.Params().At(0).Type(), types.Type(named)

	if named.TypeParams().Len() > 0 {
		args := opiter.WrapSeq(named.TypeParams().TypeParams()).
			Map(func(tp *types.TypeParam) types.Type { return tp }).
			CollectSlice()

		inst, err := types.Instantiate(nil, sig, args, true)
		if err != nil {
			return false, false
		}

		value, err = types.Instantiate(nil, named.Origin(), args, false)
		if err != nil {
			return false, false
		}

		first = inst.(*types.Signature).Params().At(0).Type()
	}

	if types.Identical(first, value) {
		return true, false
	}

	return types.Identical(first, types.NewPointer(value)), true
}

// typeParamNames returns the names of the type parameters of named.
func typeParamNames(named *types.Named) []string {
	return opiter.WrapSeq(named.TypeParams().TypeParams()).
		Map(func(tp *types.TypeParam) string { return tp.Obj().Name() }).
		CollectSlice()
}
