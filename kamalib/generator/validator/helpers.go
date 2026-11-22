package validator

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/gofragment"
)

// helperSet holds the declarations validators declared for the files of a
// package, keyed by name.
//
// No source holds the set: it tells what the generated code of the package
// calls, which is known only once the code is written.
type helperSet struct {
	decls map[string]func(s Spelling) (string, error)
}

func newHelperSet() *helperSet {
	return &helperSet{decls: map[string]func(Spelling) (string, error){}}
}

// emitter is the Emitter of one generated file.
//
// uses and imports are facts about the output, which no source holds: the
// helpers the file calls and the packages it asked for by path.
type emitter struct {
	scope   *base.FileScope
	helpers *helperSet
	uses    map[string]bool
	imports map[string]bool
}

func newEmitter(scope *base.FileScope, helpers *helperSet) *emitter {
	return &emitter{
		scope:   scope,
		helpers: helpers,
		uses:    map[string]bool{},
		imports: map[string]bool{},
	}
}

var _ Emitter = (*emitter)(nil)

func (e *emitter) TypeString(t types.Type) (string, error) { return e.scope.TypeString(t) }

// Import returns the identifier of the package at path. A package the file
// cannot import yet, one the package of the file does not depend on, gets
// its last path element; the caller renders the file again requiring it.
func (e *emitter) Import(p string) (string, error) {
	e.imports[p] = true

	if ident, ok := e.scope.Ident(p); ok {
		return ident, nil
	}

	return path.Base(p), nil
}

func (e *emitter) Declare(name string, decl func(s Spelling) (string, error)) {
	e.uses[name] = true

	if _, ok := e.helpers.decls[name]; !ok {
		e.helpers.decls[name] = decl
	}
}

// missing returns the packages e was asked for that are not among require.
func (e *emitter) missing(require []string) bool {
	return opiter.WrapSeq(maps.Keys(e.imports)).
		Any(func(p string) bool { return !slices.Contains(require, p) })
}

// renderHelpers prints hf, declaring the helpers of helpers it hosts.
func renderHelpers(
	hdr dst.Decorations,
	pkg *packages.Package,
	hf base.Host,
	helpers *helperSet,
) ([]byte, error) {
	var require []string

	for {
		content, e, err := renderHelpersWith(hdr, pkg, hf, helpers, require)
		if err != nil {
			return nil, err
		}

		if !e.missing(require) {
			return content, nil
		}

		require = slices.Sorted(maps.Keys(e.imports))
	}
}

func renderHelpersWith(
	hdr dst.Decorations,
	pkg *packages.Package,
	hf base.Host,
	helpers *helperSet,
	require []string,
) ([]byte, *emitter, error) {
	scope, err := base.NewFileScope(pkg, helperLocals, require)
	if err != nil {
		return nil, nil, err
	}

	e := newEmitter(scope, helpers)

	var src strings.Builder

	for _, name := range hf.Names {
		text, err := helpers.decls[name](e)
		if err != nil {
			return nil, nil, fmt.Errorf("validator: helper %s: %w", name, err)
		}

		src.WriteString(text)
		src.WriteString("\n\n")
	}

	decls, err := gofragment.ParseDstDecls([]byte(src.String()))
	if err != nil {
		return nil, nil, fmt.Errorf("validator: parse helpers of %s: %w", hf.Path, err)
	}

	// Each declaration gets the doc comment naming its helper.
	if len(decls) != len(hf.Names) {
		return nil, nil, fmt.Errorf(
			"validator: helpers %v of %s give %d declarations; want one each",
			hf.Names, hf.Path, len(decls),
		)
	}

	for i, d := range decls {
		d.Decorations().Before = dst.EmptyLine
		d.Decorations().Start = dst.Decorations{
			"// " + hf.Names[i] + " is a helper of the generated Validate methods.",
			"//",
			genmark.Marker(GeneratorName),
		}
	}

	file := &dst.File{Name: dst.NewIdent(pkg.Name), Decls: decls}

	file.Decs.Start = append(slices.Clone(hdr), "\n")
	if hf.BuildLine != "" {
		file.Decs.Start = append(file.Decs.Start, hf.BuildLine, "\n")
	}

	scope.AddImports(file)

	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, file); err != nil {
		return nil, nil, fmt.Errorf("validator: print %s: %w", hf.Path, err)
	}

	return buf.Bytes(), e, nil
}

// helperLocals are the identifiers the built-in helpers declare, which an
// import of the same name would shadow.
var helperLocals = []string{
	"T", "t", "v", "zero", "ok", "got", "want", "op", "re", "s", "u", "err", "rv", "spelled",
	"i", "w",
}

// checkHelperNames reports a package-level declaration of pkg named like a
// helper the generated files call that the generator did not write, and a
// declaration the generator did not write in a helpers file of pkg.
func checkHelperNames(sel *selection, pkg *packages.Package, names []string) error {
	var errs []error

	for _, name := range names {
		obj := pkg.Types.Scope().Lookup(name)
		if obj == nil || sel.generated(obj) {
			continue
		}

		errs = append(errs, fmt.Errorf(
			"%w: %s: %s.%s is already declared for a helper of the generated Validate methods",
			ErrConflict, pkg.Fset.Position(obj.Pos()), pkg.PkgPath, name,
		))
	}

	for _, f := range pkg.Syntax {
		name := filepath.Base(pkg.Fset.Position(f.FileStart).Filename)
		if !naming.IsShared(helperKind, name) {
			continue
		}

		for _, d := range f.Decls {
			if !sel.ownHelperDecl(d) {
				errs = append(errs, fmt.Errorf(
					"%w: %s: %s holds a declaration the generator did not write",
					ErrConflict, pkg.Fset.Position(d.Pos()), name,
				))
			}
		}
	}

	return errors.Join(errs...)
}

// ownHelperDecl reports whether d, a declaration of a helpers file, is one
// the generator writes there: an import, or a function, variable or
// constant carrying a registered marker.
func (s *selection) ownHelperDecl(d ast.Decl) bool {
	switch d := d.(type) {
	case *ast.GenDecl:
		if d.Tok == token.IMPORT {
			return true
		}

		_, ok := s.opts.Markers.Generated(d.Doc)

		return ok && d.Tok != token.TYPE
	case *ast.FuncDecl:
		_, ok := s.opts.Markers.Generated(d.Doc)
		return ok && d.Recv == nil
	}

	return false
}
