package base

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// TypeGraph builds the type graph of pkgs, leaving out the types whose
// declarations carry the marker of a generator reg registers.
func TypeGraph(pkgs []*packages.Package, reg *genmark.Registry) (*typegraph.Graph, error) {
	return typegraph.New(pkgs, nil, func(_ *ast.TypeSpec, obj types.Object) (bool, error) {
		return !Generated(pkgs, reg, obj), nil
	})
}

// MarkedTypeError reports the type id, marked explicitly at origin, when
// a generator cannot generate code for it: an alias, which g, built by
// [TypeGraph] from pkgs and reg, has no node for, or a type of another kind
// than struct, slice and map. The error wraps unsupported. A type a
// generator wrote has no node either and is not reported.
func MarkedTypeError(
	pkgs []*packages.Package,
	reg *genmark.Registry,
	g *typegraph.Graph,
	id typegraph.QualifiedType,
	origin string,
	unsupported error,
) error {
	n, ok := g.Get(id)
	if !ok {
		if obj := typegraph.LookupType(pkgs, id); obj == nil || !Generated(pkgs, reg, obj) {
			return fmt.Errorf(
				"%w: %s: %s is an alias; mark the type it denotes",
				unsupported, origin, id,
			)
		}

		return nil
	}

	if !typequery.IsStructSliceOrMap(n.Detail.Type) {
		return fmt.Errorf(
			"%w: %s: %s has underlying type %s; want a struct, slice or map",
			unsupported, origin, id, n.Detail.Type.Underlying(),
		)
	}

	return nil
}
