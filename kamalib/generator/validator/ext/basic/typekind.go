package basic

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

// isStringType reports whether the underlying type of t is a string type.
func isStringType(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// convert returns the conversion of the value v of t to the predeclared
// type to, spelled spelled, or v when t is to already.
func convert(t, to types.Type, spelled string, v ext.Gen) ext.Gen {
	if types.Identical(t, to) {
		return v
	}

	return lang.Expr("%s(%s)", spelled, v)
}

// nillable reports whether the values of t can be nil: t is a pointer,
// slice, map, channel, function or interface type. The underlying type of
// a type parameter is its constraint interface, which does not make its
// values nillable.
func nillable(t types.Type) bool {
	if _, isParam := types.Unalias(t).(*types.TypeParam); isParam {
		return false
	}

	switch t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature,
		*types.Interface:
		return true
	}

	return false
}

// hasMethod reports whether every value of t can call the method name with
// no parameter and the single result result. t declares the method itself,
// or embedded fields promote it without a pointer or an interface on the
// way. A method promoted through an embedded pointer or interface panics on
// the nil the zero value holds.
func hasMethod(t types.Type, name string, result types.Type) bool {
	sel := types.NewMethodSet(t).Lookup(nil, name)
	if sel == nil {
		return false
	}

	sig := sel.Type().(*types.Signature)
	if sig.Params().Len() != 0 || sig.Results().Len() != 1 ||
		!types.Identical(sig.Results().At(0).Type(), result) {
		return false
	}

	path := sel.Index()
	cur := t

	for _, i := range path[:len(path)-1] {
		v := cur.Underlying().(*types.Struct).Field(i)

		switch v.Type().Underlying().(type) {
		case *types.Pointer, *types.Interface:
			return false
		}

		cur = v.Type()
	}

	return true
}
