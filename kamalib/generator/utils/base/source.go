package base

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"go/types"
	"iter"
	"slices"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
)

// SourcePackages drops test variants, which repeat the declarations of the
// package they test, and repeated packages. A generated test main package,
// which declares nothing a generator reads, stays.
func SourcePackages(pkgs []*packages.Package) []*packages.Package {
	// The ID of a test variant differs from its path.
	return opiter.WrapSeq(slices.Values(pkgs)).
		Filter(func(p *packages.Package) bool { return p.ID == p.PkgPath }).
		UniqueFunc(pkgPathOf).
		CollectSlice()
}

func pkgPathOf(p *packages.Package) string { return p.PkgPath }

// BuildConstraint returns the //go:build line of f.
func BuildConstraint(f *ast.File) (string, bool) {
	return opiter.WrapSeq(slices.Values(f.Comments)).
		LimitUntil(func(cg *ast.CommentGroup) bool { return cg.Pos() < f.Package }).
		FlatMap(func(cg *ast.CommentGroup) iter.Seq[*ast.Comment] { return slices.Values(cg.List) }).
		Filter(func(c *ast.Comment) bool { return constraint.IsGoBuild(c.Text) }).
		First().
		Map(func(c *ast.Comment) string { return c.Text }).
		Unpack()
}

// CheckPredeclared reports package-level declarations of pkg named like a
// predeclared identifier the generated files spell, which the declaration
// would shadow there. Each error wraps conflict. See [ShadowedPredeclared]
// for what counts as spelled.
func CheckPredeclared(pkg *packages.Package, files []File, conflict error) error {
	objs, err := ShadowedPredeclared(
		pkg,
		opiter.WrapSeq(slices.Values(files)).
			Map(func(f File) []byte { return f.Content }).
			CollectSlice(),
	)
	if err != nil {
		return err
	}

	shadowErr := func(obj types.Object) error {
		return fmt.Errorf(
			"%w: %s: %s.%s shadows the predeclared %s the generated code spells",
			conflict, pkg.Fset.Position(obj.Pos()), pkg.PkgPath, obj.Name(), obj.Name(),
		)
	}

	return errors.Join(
		opiter.WrapSeq(slices.Values(objs)).
			Map(shadowErr).
			CollectSlice()...,
	)
}

// MethodTypeParams returns the names generated methods spell the type
// parameters of tparams by. A named parameter keeps its name. A blank one,
// which the methods cannot spell, gets a name taken by no package-level
// identifier of scope, no other type parameter, none of reserved and no
// predeclared identifier.
func MethodTypeParams(
	scope *types.Scope,
	tparams *types.TypeParamList,
	reserved []string,
) []string {
	taken := map[string]bool{}

	for _, names := range [][]string{scope.Names(), types.Universe.Names(), reserved} {
		for _, name := range names {
			taken[name] = true
		}
	}

	for tp := range tparams.TypeParams() {
		taken[tp.Obj().Name()] = true
	}

	var out []string

	for i := range tparams.Len() {
		name := tparams.At(i).Obj().Name()
		if name == "_" {
			name = fmt.Sprintf("T%d", i)
			for taken[name] {
				name += "_"
			}

			taken[name] = true
		}

		out = append(out, name)
	}

	return out
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

	return opiter.WrapSeq2(opiter.MapsSorted(used)).
		OmitL().
		Filter(func(ident string) bool { return types.Universe.Lookup(ident) != nil }).
		Map(pkg.Types.Scope().Lookup).
		Filter(func(obj types.Object) bool { return obj != nil }).
		CollectSlice(), nil
}
