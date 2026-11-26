package jsonschema

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"iter"
	"slices"
	"strings"
	"unicode"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
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
	jsonv1Path   = "encoding/json"
	validPath    = "github.com/ngicks/kama/kamaruntime/validator"
	rawjsonPath  = "github.com/ngicks/kama/kamaruntime/validator/rawjson"
	wirePath     = "github.com/ngicks/kama/kamaruntime/wire"
	optPath      = "github.com/ngicks/option/opt"
	undPath      = "github.com/ngicks/option/und"
)

// commentWidth is the width doc comments are wrapped at.
const commentWidth = 76

// render returns the declarations of every schema of doc that declares a
// Go type, in the order schemas yields them, after the type of a root that
// declares none, and the imports they use.
//
// The bodies spell their locals by fixed names rather than through the
// roles of genhelper.Scope. No identifier of the file can collide with
// them: the declared types and constants are exported, the wire structs
// end in "Wire", and the imports keep the names of their packages.
func render(doc *jsonschema.Schema, opts Options) ([]base.Part, *importdecl.Map, error) {
	imports, err := newImports(opts.Package)
	if err != nil {
		return nil, nil, err
	}

	c := genhelper.NewBaseContext(
		genhelper.NewScope(),
		&genhelper.Package{Imports: imports, Path: opts.Package},
	)

	var steps []genStep

	if !declares(doc, "") {
		steps = definedSteps(doc, opts, "")
	}

	for ptr := range schemas(doc) {
		if declares(doc, ptr) {
			steps = append(steps, declSteps(doc, opts, ptr)...)
		}
	}

	text, err := lang.Render(c, steps...)
	if err != nil {
		return nil, nil, err
	}

	parts, err := splitDecls(text)
	if err != nil {
		return nil, nil, fmt.Errorf("jsonschema: parse declarations: %w", err)
	}

	return parts, imports, nil
}

// splitDecls returns the top-level declarations of text, each with its doc
// comment.
func splitDecls(text string) ([]base.Part, error) {
	src := "package p\n\n" + text

	fset := token.NewFileSet()

	f, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	file := fset.File(f.Pos())
	decl := func(d ast.Decl) base.Part {
		start := d.Pos()
		if doc := declDoc(d); doc != nil {
			start = doc.Pos()
		}

		return base.Decl(src[file.Offset(start):file.Offset(d.End())])
	}

	return opiter.SlicesValues(f.Decls).
		Map(decl).
		CollectSlice(), nil
}

// declDoc returns the doc comment of d, or nil.
func declDoc(d ast.Decl) *ast.CommentGroup {
	switch d := d.(type) {
	case *ast.FuncDecl:
		return d.Doc
	case *ast.GenDecl:
		return d.Doc
	}

	return nil
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
	case valueType(doc, ptr):
		return definedSteps(doc, opts, ptr)
	}

	return objectSteps(doc, opts, ptr)
}

// newImports returns the imports of a file of the package pkgName that
// knows the packages generated code imports. Spelling a name of one
// records it as used.
func newImports(pkgName string) (*importdecl.Map, error) {
	p := types.NewPackage(pkgName, pkgName)
	p.SetImports(slices.Concat(
		opiter.SlicesValues([]string{
			"bytes", "errors", "fmt", "io", "iter", "slices", "strconv", "strings",
		}).
			Map(func(path string) *types.Package { return types.NewPackage(path, path) }).
			CollectSlice(),
		[]*types.Package{
			types.NewPackage(jsontextPath, "jsontext"),
			types.NewPackage(jsonPath, "json"),
			// encoding/json is named json too, which jsonPath takes.
			types.NewPackage(jsonv1Path, "jsonv1"),
			types.NewPackage(validPath, "validator"),
			types.NewPackage(rawjsonPath, "rawjson"),
			types.NewPackage(wirePath, "wire"),
			types.NewPackage(optPath, "opt"),
			types.NewPackage(undPath, "und"),
		},
	))

	return importdecl.NewParser(p).ParseAst(nil)
}

// q spells name of the package at path, recording the import.
func q(path, name string) genExpr {
	return func(c genCtx) (string, error) { return c.Package().Qual(path, name) }
}

// docComment returns the doc comment of a declaration: the lines of
// commentLines of text.
func docComment(text string) genExpr {
	return lang.Comment(lang.Expr("%s", commentLines(text)))
}

// validatedDoc returns the doc comment of a type the validator generator
// gives a Validate: docComment, an empty comment line and the
// //kama:validate directive of the args. The args name the validators of the value of the
// type, and hook=False for a struct, whose hooks nothing writes.
func validatedDoc(text string, args []expr.Arg) genExpr {
	return lang.Comment(
		lang.Expr("%s", commentLines(text)),
		lang.Expr(""),
		lang.Expr("%s", directiveLines(args)),
	)
}

// checkedReturn returns the statement of a decoder returning err, the
// problems decoding recorded, and with Options.Validate those
// ValidateShallow of v reports outside the values that failed to decode.
func checkedReturn(opts Options, err, v genExpr) genStep {
	if !opts.Validate {
		return lang.Stmt("return %s", err)
	}

	return lang.Stmt("return kamaJsonschemaChecked(%s, %s.ValidateShallow(false))", err, v)
}

// decodedArgs returns args followed by decoded when the decoders validate.
// decoded tells the validator generator that a decoded value of the type
// is valid, so ValidateShallow of a type holding one skips it.
func decodedArgs(opts Options, args []expr.Arg) []expr.Arg {
	if !opts.Validate {
		return args
	}

	return append(slices.Clone(args), ext.Decoded())
}

// validateDirective is the directive of the validator generator.
const validateDirective = "//kama:validate"

// directiveLines returns the //kama:validate lines of args, one item per
// line so that a long combinator reads on a line of its own. The validator
// generator parses each line on its own and joins their items. No args
// give one line without args.
func directiveLines(args []expr.Arg) string {
	if len(args) == 0 {
		return validateDirective
	}

	return strings.Join(
		opiter.SlicesValues(args).
			Map(func(a expr.Arg) string { return validateDirective + " " + a.String() }).
			CollectSlice(),
		"\n",
	)
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

	commented := opiter.SlicesValues(lines).
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
// nullable value: the name of the type it declares or of a root of raw JSON
// text, else a built-in type.
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
		return q(jsontextPath, "Value")(c)
	case nullableUnion(doc, r):
		return goTypeSeen(c, doc, opts, valueMember(doc, r), seen)
	case declares(doc, r) || r == "" && raw(doc, r):
		return typeName(doc, opts, r)
	}

	return builtinTypeSeen(c, doc, opts, r, s, seen)
}

// builtinType spells the built-in Go type of the schema at ptr, which a
// type it declares is defined over.
func builtinType(c genCtx, doc *jsonschema.Schema, opts Options, ptr string) (string, error) {
	r, s, err := resolved(doc, ptr)
	if err != nil {
		return "", err
	}

	return builtinTypeSeen(c, doc, opts, r, s, []string{r})
}

// builtinTypeSeen is builtinType for the schema s at r, reached through
// the schemas of seen.
func builtinTypeSeen(
	c genCtx,
	doc *jsonschema.Schema,
	opts Options,
	r string,
	s *jsonschema.Schema,
	seen []string,
) (string, error) {
	k, err := kinds(doc, r)
	if err != nil {
		return "", err
	}

	if raw(doc, r) {
		return q(jsontextPath, "Value")(c)
	}

	nonNull := opiter.SlicesValues(k).
		Filter(func(k string) bool { return k != "null" }).
		CollectSlice()
	if len(nonNull) != 1 {
		return "", unsupported(r, "admits no single kind")
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
		if s.Items != nil && !bridged(doc, r, s, "items") {
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
	t, err := goTypeSeen(c, doc, opts, ptr, seen)
	if err != nil || !nullable(doc, ptr) {
		return t, err
	}

	return lang.Expr("%s[%s]", q(optPath, "Option"), t)(c)
}
