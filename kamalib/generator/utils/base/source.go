package base

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"go/types"
	"iter"
	"slices"
	"strings"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
)

// SourcePackages returns the packages of pkgs a generator reads, each path
// once. A test variant of a package, which holds the files of the package
// and its _test.go files declaring the same package, takes the place of
// the package: it declares everything the package does and the types of
// those test files too. An external test package stays. Other variants,
// such as a package compiled again for the tests of another package, are
// dropped, and so is a generated test main package, which declares nothing
// a generator reads but shares the directory of the package it tests.
func SourcePackages(pkgs []*packages.Package) []*packages.Package {
	tested := map[string]bool{}

	for _, p := range pkgs {
		if gopackages.IsTestVariant(p) {
			tested[p.PkgPath] = true
		}
	}

	// The ID of any variant differs from its path.
	return opiter.WrapSeq(slices.Values(pkgs)).
		Filter(func(p *packages.Package) bool {
			return gopackages.IsTestVariant(p) || gopackages.IsExternalTest(p) ||
				(p.ID == p.PkgPath && !tested[p.PkgPath] && !gopackages.IsTestMain(p))
		}).
		UniqueFunc(pkgPathOf).
		CollectSlice()
}

// DeclaringPackage returns the package of pkgs declaring obj, or nil.
//
// A package and its test variant hold distinct objects for one
// declaration, and a package of pkgs may import the one pkgs do not hold.
// The package is therefore found by path. The variants share their syntax
// trees, so a position of obj is one in the package found.
func DeclaringPackage(pkgs []*packages.Package, obj types.Object) *packages.Package {
	if obj.Pkg() == nil {
		return nil
	}

	return opiter.WrapSeq(slices.Values(pkgs)).
		Filter(func(p *packages.Package) bool { return p.PkgPath == obj.Pkg().Path() }).
		First().
		UnwrapOrDefault()
}

// PackageFile returns name, the name of a file without a directory that
// the generated files of pkg share, as pkg names it. An external test
// package shares its directory with the package it tests, so its file
// takes the _test suffix, as in zz_gen.helper.kama_partial_test.go.
func PackageFile(pkg *packages.Package, name string) string {
	if !gopackages.IsExternalTest(pkg) {
		return name
	}

	return strings.TrimSuffix(name, ".go") + "_test.go"
}

// PackageDir returns the directory of pkg as the path of an output:
// slash-separated and relative to root, an absolute directory. A package
// outside root, or one whose directory is unknown, is an error. It is the
// directory genopt finds the package option file and the documents of pkg
// relative to.
func PackageDir(root string, pkg *packages.Package) (string, error) {
	dir, err := genopt.PackageDir(root, pkg)
	if err != nil {
		return "", fmt.Errorf("base: %w", err)
	}

	return dir, nil
}

func pkgPathOf(p *packages.Package) string { return p.PkgPath }

// source returns the package clause and the parts of c, the source of the
// declarations of c without the imports and the marks.
func (c Composed) source() []byte {
	var b bytes.Buffer

	b.WriteString("package " + c.Package + "\n")

	for _, part := range c.Parts {
		b.WriteString("\n")

		switch p := part.(type) {
		case Decl:
			b.Write(p)
		case Text:
			b.Write(p)
		}

		b.WriteString("\n")
	}

	return b.Bytes()
}

// BuildConstraint returns the //go:build line of f.
func BuildConstraint(f *ast.File) (string, bool) {
	return opiter.WrapSeq(slices.Values(f.Comments)).
		TakeWhile(false, func(cg *ast.CommentGroup) bool { return cg.Pos() < f.Package }).
		FlatMap(func(cg *ast.CommentGroup) iter.Seq[*ast.Comment] { return slices.Values(cg.List) }).
		Filter(func(c *ast.Comment) bool { return constraint.IsGoBuild(c.Text) }).
		First().
		Map(func(c *ast.Comment) string { return c.Text }).
		Unpack()
}

// CheckPredeclared reports package-level declarations of pkg named like a
// predeclared identifier the parts of files spell, which the declaration
// would shadow there. Each error wraps conflict. See [ShadowedPredeclared]
// for what counts as spelled.
func CheckPredeclared(pkg *packages.Package, files []Composed, conflict error) error {
	objs, err := ShadowedPredeclared(
		pkg,
		opiter.WrapSeq(slices.Values(files)).
			Map(Composed.source).
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
