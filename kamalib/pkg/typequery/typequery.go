package typequery

import "go/types"

// Import paths of the packages of github.com/ngicks/option.
const (
	OptPath     = "github.com/ngicks/option/opt"
	UndPath     = "github.com/ngicks/option/und"
	ElasticPath = "github.com/ngicks/option/elastic"
)

// IsNamed reports whether t refers to the type name of the package at
// pkgPath, through aliases.
func IsNamed(t types.Type, pkgPath, name string) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}

	obj := named.Obj()

	return obj.Pkg() != nil && obj.Pkg().Path() == pkgPath && obj.Name() == name
}

// TypeArg returns the first type argument of the instantiated type t.
// It panics when t is not an instantiated named type.
func TypeArg(t types.Type) types.Type {
	return types.Unalias(t).(*types.Named).TypeArgs().At(0)
}

// IsStructSliceOrMap reports whether the underlying type of t is a struct,
// a slice or a map.
func IsStructSliceOrMap(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Struct, *types.Slice, *types.Map:
		return true
	}

	return false
}
