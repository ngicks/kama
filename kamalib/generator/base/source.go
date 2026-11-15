package base

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
)

// SourcePackages drops test variants, which repeat the declarations of the
// package they test, and repeated packages. A generated test main package,
// which declares nothing a generator reads, stays.
func SourcePackages(pkgs []*packages.Package) []*packages.Package {
	sources := opiter.Filter(isSourceVariant, slices.Values(pkgs))
	return slices.Collect(opiter.UniqueFunc(pkgPathOf, sources))
}

func pkgPathOf(p *packages.Package) string { return p.PkgPath }

// isSourceVariant reports whether p is the package itself rather than a test
// variant, whose ID differs from its path.
func isSourceVariant(p *packages.Package) bool { return p.ID == p.PkgPath }

// BuildConstraint returns the //go:build line of f.
func BuildConstraint(f *ast.File) (string, bool) {
	for _, cg := range f.Comments {
		if cg.Pos() >= f.Package {
			break
		}
		for _, c := range cg.List {
			if constraint.IsGoBuild(c.Text) {
				return c.Text, true
			}
		}
	}
	return "", false
}

// ShadowedPredeclared returns the package-level objects of pkg named like a
// predeclared identifier the generated files contents spell, which the
// object would shadow there, sorted by name.
//
// Every identifier the files use other than selected names, field and
// parameter names and declared names is taken as a use, so a predeclared
// name spelled for a type of pkg itself, as und.Und[string] for a field of
// a package-level type string, is returned too.
func ShadowedPredeclared(pkg *packages.Package, contents [][]byte) ([]types.Object, error) {
	used := map[string]bool{}
	for _, c := range contents {
		f, err := parser.ParseFile(token.NewFileSet(), "", c, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("base: parse generated file of %s: %w", pkg.PkgPath, err)
		}
		declared := map[*ast.Ident]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				declared[n.Sel] = true
			case *ast.Field:
				for _, ident := range n.Names {
					declared[ident] = true
				}
			case *ast.FuncDecl:
				declared[n.Name] = true
			case *ast.TypeSpec:
				declared[n.Name] = true
			case *ast.ImportSpec:
				return false
			case *ast.Ident:
				if !declared[n] {
					used[n.Name] = true
				}
			}
			return true
		})
	}

	isDeclared := func(obj types.Object) bool { return obj != nil }
	predeclared := slices.Sorted(opiter.Filter(isPredeclared, maps.Keys(used)))
	objs := opiter.Map(pkg.Types.Scope().Lookup, slices.Values(predeclared))
	return slices.Collect(opiter.Filter(isDeclared, objs)), nil
}

func isPredeclared(ident string) bool { return types.Universe.Lookup(ident) != nil }
