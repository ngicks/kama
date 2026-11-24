package validator

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dave/dst"
	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
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
	hosts := base.HostShared(
		naming, helperKind, filepath.Join(dir, base.PackageFile(pkg, HelpersFile)), callers,
	)

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

	if err := base.CheckPredeclared(pkg, out, ErrConflict); err != nil {
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
		tparams := base.MethodTypeParams(pkg.Types.Scope(), n.Detail.Type.TypeParams(), fixedNames)
		for _, tp := range tparams {
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
		forms := []*writer{{emitter: e, sel: sel, n: n}}
		if sel.hasShallow(n.Detail.Type) {
			forms = append(forms, &writer{emitter: e, sel: sel, n: n, shallow: true})
		}

		if hasEvaluated(n.Detail.Type) {
			forms = append(forms, &writer{emitter: e, sel: sel, n: n, evaluated: true})
		}

		for _, w := range forms {
			m, err := w.method()
			if err != nil {
				errs = append(errs, err)
				break
			}

			text.WriteString(m)
			text.WriteString("\n")
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, nil, err
	}

	decls, err := genhelper.ParseDecls([]byte(text.String()))
	if err != nil {
		return nil, nil, fmt.Errorf("validator: parse methods of %s: %w", src, err)
	}

	// The methods are declared on the source types, so they must build under
	// the same constraint. A constraint in the file name is kept by the
	// output name.
	line, _ := base.BuildConstraint(nodes[0].Detail.File)

	content, err := genhelper.PrintDstFile(pkg.Name, hdr, line, decls, scope.Imports)
	if err != nil {
		return nil, nil, fmt.Errorf("validator: print %s: %w", src, err)
	}

	return content, e, nil
}

// fileHeader returns the comment lines every generated file of the package
// at pkgPath starts with.
func fileHeader(opts Options, pkgPath string, now time.Time) (dst.Decorations, error) {
	return base.Header(generatorPath, opts.Loaded.HeaderParams(pkgPath), now)
}
