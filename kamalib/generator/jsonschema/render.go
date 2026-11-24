package jsonschema

import (
	"fmt"
	"go/token"
	"go/types"
	"iter"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/dave/dst"
	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// genCtx is the context the generated declarations render with.
type genCtx = genhelper.BaseContext

type (
	genStep = genhelper.Step[genCtx]
	genExpr = genhelper.Gen[genCtx]
)

// lang is the vocabulary of steps for genCtx. It is zero-size and holds no
// state.
var lang genhelper.Lang[genCtx]

// The packages generated code imports.
const (
	jsontextPath = "encoding/json/jsontext"
	jsonPath     = "encoding/json/v2"
	runtimePath  = "github.com/ngicks/kama/kamalib/pkg/jsonschema/jsonschemart"
	validPath    = "github.com/ngicks/kama/kamalib/pkg/validation"
	optPath      = "github.com/ngicks/option/opt"
	undPath      = "github.com/ngicks/option/und"
)

// commentWidth is the width doc comments are wrapped at.
const commentWidth = 76

// render prints the file holding the declarations of every schema of doc
// that declares a Go type, in the order schemas yields them, after the
// type of a root that declares none.
//
// The bodies spell their locals by fixed names rather than through the
// roles of genhelper.Scope. No identifier of the file can collide with
// them: the declared types and constants are exported, the wire structs
// end in "Wire", and the imports keep the names of their packages.
func render(doc *jsonschema.Schema, opts Options) ([]byte, error) {
	imports, err := newImports(opts.Package)
	if err != nil {
		return nil, err
	}

	c := genhelper.NewBaseContext(
		genhelper.NewScope(),
		&genhelper.Package{Imports: imports, Path: opts.Package},
	)

	var steps []genStep

	if !declares(doc, "") {
		steps = rootSteps(doc, opts)
	}

	for ptr := range schemas(doc) {
		if declares(doc, ptr) {
			steps = append(steps, declSteps(doc, opts, ptr)...)
		}
	}

	text, err := lang.Render(c, steps...)
	if err != nil {
		return nil, err
	}

	decls, err := genhelper.ParseDecls([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("jsonschema: parse declarations: %w", err)
	}

	typeName := any(nil)
	if opts.TypeName != "" {
		typeName = opts.TypeName
	}

	hdr, err := base.Header(
		generatorPath,
		map[string]any{"schema": opts.Schema, "type_name": typeName},
		opts.Now(),
	)
	if err != nil {
		return nil, err
	}

	if imp := importDecl(imports); imp != nil {
		decls = slices.Insert(decls, 0, dst.Decl(imp))
	}

	content, err := genhelper.PrintDstFile(opts.Package, hdr, "", decls, nil)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: print: %w", err)
	}

	return content, nil
}

// declSteps returns the declarations of the schema at ptr, which declares
// a Go type.
func declSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	switch {
	case union(s):
		return unionSteps(doc, opts, ptr)
	case stringEnum(s):
		return enumSteps(doc, opts, ptr)
	}

	return objectSteps(doc, opts, ptr)
}

// newImports returns the imports of a file of the package pkgName that
// knows the packages generated code imports. Spelling a name of one
// records it as used.
func newImports(pkgName string) (*importdecl.Map, error) {
	pkg := types.NewPackage(pkgName, pkgName)
	pkg.SetImports([]*types.Package{
		types.NewPackage(jsontextPath, "jsontext"),
		types.NewPackage(jsonPath, "json"),
		types.NewPackage(runtimePath, "jsonschemart"),
		types.NewPackage(validPath, "validation"),
		types.NewPackage(optPath, "opt"),
		types.NewPackage(undPath, "und"),
	})

	return importdecl.NewParser(pkg).ParseDst(nil)
}

// importDecl returns the declaration of the imports the file used: the
// standard library, an empty line, then the other packages, as goimports
// groups them. It returns nil when there are none.
func importDecl(m *importdecl.Map) *dst.GenDecl {
	imps := slices.Collect(m.Missing())
	if len(imps) == 0 {
		return nil
	}

	std := func(imp importdecl.Import) bool {
		first, _, _ := strings.Cut(imp.Package.Path, "/")
		return !strings.Contains(first, ".")
	}

	ordered := slices.Concat(
		opiter.WrapSeq(slices.Values(imps)).
			Filter(std).
			CollectSlice(),
		opiter.WrapSeq(slices.Values(imps)).
			Filter(func(imp importdecl.Import) bool { return !std(imp) }).
			CollectSlice(),
	)

	decl := &dst.GenDecl{Tok: token.IMPORT, Lparen: len(ordered) > 1}

	for i, imp := range ordered {
		spec := &dst.ImportSpec{
			Path: &dst.BasicLit{Kind: token.STRING, Value: strconv.Quote(imp.Package.Path)},
		}
		if imp.Name != "" {
			spec.Name = dst.NewIdent(imp.Name)
		}

		if i > 0 && !std(imp) && std(ordered[i-1]) {
			spec.Decs.Before = dst.EmptyLine
		}

		decl.Specs = append(decl.Specs, spec)
	}

	return decl
}

// q spells name of the package at path, recording the import.
func q(path, name string) genExpr {
	return func(c genCtx) (string, error) { return c.Package().Qual(path, name) }
}

// docComment returns the doc comment of a declaration: the lines of
// commentLines of text, an empty comment line and the marker of the
// generator.
func docComment(text string) genExpr {
	return lang.DocComment(GeneratorId.Marker(), "%s", commentLines(text))
}

// commentLines returns text as Go line comments: each line of text, as
// "\r\n", "\r" and "\n" end them, wrapped at commentWidth, every line
// starting with "// " and an empty line being "//". genhelper prints text
// that is comments already as it is.
//
// Text from the schema thus never starts a comment line itself, so it
// cannot write a directive such as //go:generate or //line, forge the
// marker of the generator or open or close a block comment. A control
// character other than a line end and a byte order mark, which Go source
// rejects, becomes a space.
func commentLines(text string) string {
	printable := func(r rune) rune {
		if unicode.IsControl(r) || r == '\ufeff' {
			return ' '
		}

		return r
	}

	lines := strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text), "\n")

	commented := opiter.WrapSeq(slices.Values(lines)).
		FlatMap(func(l string) iter.Seq[string] {
			return strings.SplitSeq(
				genhelper.WrapComment(strings.Map(printable, l), commentWidth),
				"\n",
			)
		}).
		Map(func(l string) string {
			if l == "" {
				return "//"
			}

			return "// " + l
		}).
		CollectSlice()

	return strings.Join(commented, "\n")
}

// typeDoc returns lead followed by the description of the schema at ptr
// and a deprecation notice when the schema says so.
func typeDoc(doc *jsonschema.Schema, ptr, lead string) string {
	s, err := at(doc, ptr)
	if err != nil {
		return lead
	}

	return strings.Join(append([]string{lead}, annotations(s)...), "\n\n")
}

// annotations returns the paragraphs of a doc comment the description and
// deprecated keywords of s give. The title names a type or a member
// instead, so it gives no paragraph.
func annotations(s *jsonschema.Schema) []string {
	var out []string

	if s.Description.IsSome() {
		out = append(out, s.Description.Value())
	}

	if s.Deprecated.Value() {
		out = append(out, "Deprecated: the schema marks it deprecated.")
	}

	return out
}

// listText joins names with commas and a final " and ", as a, b and c.
func listText(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}

	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// goType spells the Go type of the schema at ptr, without the option of a
// nullable value: the name of the type it declares, else a built-in type.
func goType(doc *jsonschema.Schema, opts Options, ptr string) genExpr {
	return func(c genCtx) (string, error) { return goTypeSeen(c, doc, opts, ptr, nil) }
}

func goTypeSeen(
	c genCtx,
	doc *jsonschema.Schema,
	opts Options,
	ptr string,
	seen []string,
) (string, error) {
	r, s, err := resolved(doc, ptr)
	if err != nil {
		return "", err
	}

	if slices.Contains(seen, r) {
		return "", unsupported(r, "holds itself through no declared type")
	}

	seen = append(seen, r)

	switch {
	case s.Bool.IsSome():
		if !s.Bool.Value() {
			return "", unsupported(r, "the false schema admits no value")
		}

		return q(jsontextPath, "Value")(c)
	case nullableUnion(doc, r):
		return goTypeSeen(c, doc, opts, valueMember(doc, r), seen)
	case declares(doc, r):
		return typeName(doc, opts, r)
	}

	k, err := kinds(doc, r)
	if err != nil {
		return "", err
	}

	if slices.Equal(k, allKinds) {
		return q(jsontextPath, "Value")(c)
	}

	nonNull := opiter.WrapSeq(slices.Values(k)).
		Filter(func(k string) bool { return k != "null" }).
		CollectSlice()
	if len(nonNull) != 1 {
		return "", unsupported(r, "admits no kind other than null")
	}

	switch nonNull[0] {
	case "string":
		return "string", nil
	case "integer":
		return "int64", nil
	case "number":
		return "float64", nil
	case "boolean":
		return "bool", nil
	case "array":
		e, err := q(jsontextPath, "Value")(c)
		if s.Items != nil {
			e, err = elemTypeSeen(c, doc, opts, child(r, "items"), seen)
		}

		return "[]" + e, err
	case "object":
		e, err := elemTypeSeen(c, doc, opts, child(r, "additionalProperties"), seen)

		return "map[string]" + e, err
	}

	return "", unsupported(r, "unknown type %q", nonNull[0])
}

// elemType spells the Go type of a value the schema at ptr describes
// outside an object property: an element of a slice or a map, a member of
// a union or of an allOf. A nullable value is an opt.Option.
func elemType(doc *jsonschema.Schema, opts Options, ptr string) genExpr {
	return func(c genCtx) (string, error) { return elemTypeSeen(c, doc, opts, ptr, nil) }
}

func elemTypeSeen(
	c genCtx,
	doc *jsonschema.Schema,
	opts Options,
	ptr string,
	seen []string,
) (string, error) {
	r, s, err := resolvedValue(doc, ptr)
	if err != nil {
		return "", err
	}

	// Only an object property and the root have a place to check a value
	// against them, and the root does not come here.
	if s.Const != nil || (s.Enum != nil && !stringEnum(s)) {
		return "", unsupported(r, "enum or const outside an object property")
	}

	t, err := goTypeSeen(c, doc, opts, ptr, seen)
	if err != nil || !nullable(doc, ptr) {
		return t, err
	}

	return lang.Expr("%s[%s]", q(optPath, "Option"), t)(c)
}
