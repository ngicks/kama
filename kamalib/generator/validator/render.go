package validator

import (
	"bytes"
	"errors"
	"fmt"
	"go/types"
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/gofragment"
)

// renderPackage prints the files of pkg generated from the source files
// srcs and the helpers files they call.
func renderPackage(
	sel *selection,
	hdr dst.Decorations,
	pkg *packages.Package,
	srcs []string,
) ([]File, error) {
	helpers := newHelperSet()

	var (
		out     []File
		callers []base.Caller
		errs    []error
	)

	for _, src := range srcs {
		content, e, err := renderFile(sel, hdr, pkg, src, helpers)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		nodes := sel.targetsIn(src)
		callers = append(callers, base.Caller{Src: src, File: nodes[0].Detail.File, Uses: e.uses})
		out = append(out, File{Path: naming.SourceFile(src), Content: content})
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	dir := filepath.Dir(srcs[0])
	hosts := base.HostShared(naming, helperKind, filepath.Join(dir, HelpersFile), callers)

	names := opiter.WrapSeq(slices.Values(hosts)).
		FlatMap(func(h base.Host) iter.Seq[string] { return slices.Values(h.Names) }).
		CollectSlice()

	if err := errors.Join(
		checkHelperNames(sel, pkg, names),
		checkTypeParamNames(sel, pkg, names),
	); err != nil {
		return nil, err
	}

	for _, hf := range hosts {
		content, err := renderHelpers(hdr, pkg, hf, helpers)
		if err != nil {
			return nil, err
		}

		out = append(out, File{Path: hf.Path, Content: content})
	}

	contents := opiter.WrapSeq(slices.Values(out)).
		Map(func(f File) []byte { return f.Content }).
		CollectSlice()

	if err := checkPredeclared(pkg, contents); err != nil {
		return nil, err
	}

	return out, nil
}

// renderFile prints the Validate methods of the targets the source file src
// declares. A first render asks for the packages the methods spell by path;
// when the file cannot import one of them yet, a second render requires
// them, as an import required but not spelled would not compile.
func renderFile(
	sel *selection,
	hdr dst.Decorations,
	pkg *packages.Package,
	src string,
	helpers *helperSet,
) ([]byte, *emitter, error) {
	var require []string

	for {
		content, e, err := renderFileWith(sel, hdr, pkg, src, helpers, require)
		if err != nil {
			return nil, nil, err
		}

		if !e.missing(require) {
			return content, e, nil
		}

		require = slices.Sorted(maps.Keys(e.imports))
	}
}

func renderFileWith(
	sel *selection,
	hdr dst.Decorations,
	pkg *packages.Package,
	src string,
	helpers *helperSet,
	require []string,
) ([]byte, *emitter, error) {
	nodes := sel.targetsIn(src)
	reserve := slices.Clone(fixedNames)

	for _, n := range nodes {
		for _, tp := range methodTypeParams(pkg.Types.Scope(), n.Detail.Type.TypeParams()) {
			reserve = append(reserve, tp, validateRole(tp))
		}
	}

	scope, err := base.NewFileScope(pkg, reserve, require)
	if err != nil {
		return nil, nil, err
	}

	e := newEmitter(scope, helpers)

	var (
		text strings.Builder
		errs []error
	)

	for _, n := range nodes {
		w := &writer{emitter: e, sel: sel, n: n}

		m, err := w.method()
		if err != nil {
			errs = append(errs, err)
			continue
		}

		text.WriteString(m)
		text.WriteString("\n")
	}

	if err := errors.Join(errs...); err != nil {
		return nil, nil, err
	}

	decls, err := gofragment.ParseDstDecls([]byte(text.String()))
	if err != nil {
		return nil, nil, fmt.Errorf("validator: parse methods of %s: %w", src, err)
	}

	for _, d := range decls {
		d.Decorations().Before = dst.EmptyLine
	}

	file := &dst.File{Name: dst.NewIdent(pkg.Name), Decls: decls}

	file.Decs.Start = append(slices.Clone(hdr), "\n")
	// The methods are declared on the source types, so they must build under
	// the same constraint. A constraint in the file name is kept by the
	// output name.
	if line, ok := base.BuildConstraint(nodes[0].Detail.File); ok {
		file.Decs.Start = append(file.Decs.Start, line, "\n")
	}

	scope.AddImports(file)

	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, file); err != nil {
		return nil, nil, fmt.Errorf("validator: print %s: %w", src, err)
	}

	return buf.Bytes(), e, nil
}

// fileHeader returns the comment lines every generated file of the package
// at pkgPath starts with.
func fileHeader(opts Options, pkgPath string, now time.Time) (dst.Decorations, error) {
	return base.Header(generatorPath, opts.Loaded.HeaderParams(pkgPath), now)
}

// methodTypeParams returns the names the method spells the type parameters
// of tparams by. A named parameter keeps its name. A blank one gets a name
// taken by no package-level identifier of scope, no other type parameter,
// no fixed name and no predeclared identifier.
func methodTypeParams(scope *types.Scope, tparams *types.TypeParamList) []string {
	taken := map[string]bool{}

	for _, names := range [][]string{scope.Names(), types.Universe.Names(), fixedNames} {
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

// checkPredeclared reports package-level declarations of pkg named like a
// predeclared identifier the generated files contents spell, which the
// declaration would shadow there.
func checkPredeclared(pkg *packages.Package, contents [][]byte) error {
	objs, err := base.ShadowedPredeclared(pkg, contents)
	if err != nil {
		return err
	}

	shadowErr := func(obj types.Object) error {
		return fmt.Errorf(
			"%w: %s: %s.%s shadows the predeclared %s the generated code spells",
			ErrConflict, pkg.Fset.Position(obj.Pos()), pkg.PkgPath, obj.Name(), obj.Name(),
		)
	}

	return errors.Join(
		opiter.WrapSeq(slices.Values(objs)).
			Map(shadowErr).
			CollectSlice()...,
	)
}
