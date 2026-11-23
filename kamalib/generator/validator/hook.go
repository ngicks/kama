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
// Validate cannot call: a missing one, one on the pointer receiver and one
// of another signature than func(bool) error.
func (s *selection) checkHook(n *typegraph.Node, v *types.Var) error {
	named := n.Detail.Type
	name := hookName(v)
	pkg := named.Obj().Pkg()

	recv := named.Obj().Name()
	if named.TypeParams().Len() > 0 {
		tps := opiter.WrapSeq(named.TypeParams().TypeParams()).
			Map(func(tp *types.TypeParam) string { return tp.Obj().Name() }).
			CollectSlice()

		recv += "[" + strings.Join(tps, ", ") + "]"
	}

	fail := func(problem string) error {
		return fmt.Errorf(
			"%w: %s: field %s of %s %s; declare func (%s) %s(stopOnFirstError bool) error"+
				" or turn the hook off with //kama:validate hook=off on the field or the type",
			ErrHook, n.Detail.Pkg.Fset.Position(v.Pos()), v.Name(), n.Id, problem, recv, name,
		)
	}

	sel := types.NewMethodSet(named).Lookup(pkg, name)
	if sel == nil {
		if types.NewMethodSet(types.NewPointer(named)).Lookup(pkg, name) != nil {
			return fail("has its hook " + name + " on the pointer receiver")
		}

		return fail("has no hook " + name)
	}

	sig := sel.Type().(*types.Signature)
	if sig.Variadic() || sig.Params().Len() != 1 || !returnsError(sig) ||
		!types.Identical(sig.Params().At(0).Type(), types.Typ[types.Bool]) {
		return fail(fmt.Sprintf("has its hook %s with signature %s", name, sig))
	}

	return nil
}
