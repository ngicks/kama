package jsonschema

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"iter"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/genheader"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
)

// validatorId is the identity of the validator generator of package
// github.com/ngicks/kama/kamalib/generator/validator, which writes the
// Validate methods of the types carrying //kama:validate. It is spelled
// here, as the directive is, so that this generator does not depend on the
// validator generator.
var validatorId = base.Id{Namespace: "kama", Name: "validate"}

// stubsFile returns the file of the stubs of the Validate methods the
// decoders of main call, which the validator generator writes for main, or
// false when no stub is needed.
//
// The decoders call ValidateShallow and Validate on the types themselves,
// and packages.load fails on the package until the methods exist. The
// stubs carry the mark of the validator generator in the file it writes for
// main, so that it takes them for its own output and replaces them. A run
// whose declarations equal those of the existing file keeps the methods the
// validator generator wrote for them. A run changing them replaces those
// methods by stubs too, since the methods may refer to fields and types the
// run drops, even when the decoders call no Validate. Without that file, a
// run whose decoders call no Validate needs no stub. A stub reports that
// the validator generator has not run with an error wrapping
// validator.ErrStub.
//
// A method the package declares in a file of its own, without a generated
// mark, gets no stub, which would declare it twice.
func stubsFile(env base.Env, opts Options, main base.Composed) (base.Whole, bool, error) {
	file := filepath.ToSlash(validatorId.Naming().SourceFile(main.Path))

	exists := false

	if env.Out != nil {
		_, err := fs.Stat(env.Out, file)
		switch {
		case err == nil:
			exists = true

			same, err := sameDecls(env.Out, main)
			if err != nil || same {
				return base.Whole{}, false, err
			}
		case !errors.Is(err, fs.ErrNotExist):
			return base.Whole{}, false, fmt.Errorf("jsonschema: %w", err)
		}
	}

	if !exists && !opts.Validate {
		return base.Whole{}, false, nil
	}

	hdr, err := genheader.GeneratorParams{
		Name:      GeneratorId().String(),
		Params:    main.Params,
		Generated: env.Now,
	}.Comment()
	if err != nil {
		return base.Whole{}, false, fmt.Errorf("jsonschema: %w", err)
	}

	_, f, err := parseParts(main.Parts)
	if err != nil {
		return base.Whole{}, false, err
	}

	own, err := ownFiles(env, path.Dir(main.Path), main.Package)
	if err != nil {
		return base.Whole{}, false, err
	}

	var b strings.Builder

	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE {
			continue
		}

		lines := directives(g.Doc)
		if lines == nil {
			continue
		}

		name := g.Specs[0].(*ast.TypeSpec).Name.Name

		methods := []string{"Validate"}
		if slices.Contains(lines, "decoded") {
			methods = append(methods, "ValidateShallow")
		}

		for _, m := range methods {
			if !declaresMethod(env.Registry, own, name, m) {
				b.WriteString(stub(name, m))
			}
		}
	}

	imports := ""
	if b.Len() > 0 {
		imports = "\nimport \"" + validPath + "\"\n"
	}

	content := hdr + "\n\npackage " + main.Package + "\n" + imports + b.String()

	return base.Whole{Path: file, Content: []byte(content)}, true, nil
}

// stub returns the stub of the method of the type name.
func stub(name, method string) string {
	return fmt.Sprintf(`
// %[2]s is a stub of the method the validator generator writes. It
// reports that the generator has not run.
//
%[3]s
func (v %[1]s) %[2]s(stopOnFirstError bool) error {
	return validator.New(validator.ErrStub, "%[1]s.%[2]s; run validate.generate over the package")
}
`, name, method, validatorId.Marker())
}

// ownFiles returns the parsed Go files of dir in the output of env that
// declare the package pkg. An external test package shares the directory
// of the package it tests, and the package clause tells their files apart.
// A file that does not parse is left to the build to report.
func ownFiles(env base.Env, dir, pkg string) ([]*ast.File, error) {
	if env.Out == nil {
		return nil, nil
	}

	entries, err := fs.ReadDir(env.Out, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("jsonschema: read %q: %w", dir, err)
	}

	var files []*ast.File

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}

		name := path.Join(dir, e.Name())

		src, err := fs.ReadFile(env.Out, name)
		if err != nil {
			return nil, fmt.Errorf("jsonschema: read %q: %w", name, err)
		}

		f, err := parser.ParseFile(
			token.NewFileSet(), name, src, parser.ParseComments|parser.SkipObjectResolution,
		)
		if err == nil && f.Name.Name == pkg {
			files = append(files, f)
		}
	}

	return files, nil
}

// declaresMethod reports whether files declare the method name of the type
// recv in a declaration without a generated mark: one written by hand.
func declaresMethod(reg *genmark.Registry, files []*ast.File, recv, name string) bool {
	return opiter.WrapSeq(slices.Values(files)).
		FlatMap(func(f *ast.File) iter.Seq[ast.Decl] { return slices.Values(f.Decls) }).
		Any(func(d ast.Decl) bool {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name.Name != name || receiverName(fd) != recv {
				return false
			}

			return !slices.ContainsFunc(base.AstDeclDocs(d), func(doc *ast.CommentGroup) bool {
				_, ok := reg.Generated(doc)
				return ok
			})
		})
}

// receiverName returns the name of the type of the receiver of fd. The
// types the generator declares take no type parameters.
func receiverName(fd *ast.FuncDecl) string {
	t := fd.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}

	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}

	return ""
}

// directives returns the args of the //kama:validate lines of doc, one
// string per line, or nil when doc has none.
func directives(doc *ast.CommentGroup) []string {
	if doc == nil {
		return nil
	}

	var args []string

	for _, c := range doc.List {
		if rest, ok := strings.CutPrefix(c.Text, validateDirective); ok &&
			(rest == "" || rest[0] == ' ') {
			args = append(args, strings.TrimSpace(rest))
		}
	}

	return args
}

// sameDecls reports whether the file of main in fsys holds the
// declarations of main. Both sides are printed by go/printer, so the
// formatting of the existing file does not matter.
func sameDecls(fsys fs.FS, main base.Composed) (bool, error) {
	src, err := fs.ReadFile(fsys, main.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("jsonschema: %w", err)
	}

	fset := token.NewFileSet()

	existing, err := parser.ParseFile(fset, main.Path, src, parser.ParseComments)
	if err != nil {
		return false, nil
	}

	stamped := make([]base.Part, 0, len(main.Parts))
	for _, p := range main.Parts {
		if d, ok := p.(base.Decl); ok {
			p = base.Decl(base.Stamp(GeneratorId(), d))
		}

		stamped = append(stamped, p)
	}

	mineSet, mine, err := parseParts(stamped)
	if err != nil {
		return false, err
	}

	a, err := printDecls(fset, existing)
	if err != nil {
		return false, err
	}

	b, err := printDecls(mineSet, mine)
	if err != nil {
		return false, err
	}

	return slices.Equal(a, b), nil
}

// parseParts parses parts, the declarations of a file, into a file of the
// file set it returns.
func parseParts(parts []base.Part) (*token.FileSet, *ast.File, error) {
	var b bytes.Buffer

	b.WriteString("package p\n")

	// render yields declarations only.
	for _, p := range parts {
		if d, ok := p.(base.Decl); ok {
			b.WriteString("\n")
			b.Write(d)
			b.WriteString("\n")
		}
	}

	fset := token.NewFileSet()

	f, err := parser.ParseFile(fset, "", b.Bytes(), parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("jsonschema: parse declarations: %w", err)
	}

	return fset, f, nil
}

// printDecls returns the declarations of f but its imports as go/printer
// prints them.
func printDecls(fset *token.FileSet, f *ast.File) ([]string, error) {
	var out []string

	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			continue
		}

		var b bytes.Buffer
		if err := format.Node(
			&b,
			fset,
			&printer.CommentedNode{Node: d, Comments: f.Comments},
		); err != nil {
			return nil, fmt.Errorf("jsonschema: print declaration: %w", err)
		}

		out = append(out, b.String())
	}

	return out, nil
}
