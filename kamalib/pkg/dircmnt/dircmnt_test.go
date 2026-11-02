package dircmnt

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"gotest.tools/v3/assert"
)

func parse(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments|parser.AllErrors)
	assert.NilError(t, err)
	return fset, f
}

func decorate(t *testing.T, src string) *dst.File {
	t.Helper()

	fset, f := parse(t, src)
	df, err := decorator.NewDecorator(fset).DecorateFile(f)
	assert.NilError(t, err)
	return df
}

func TestParse(t *testing.T) {
	cases := []struct {
		text string
		want Comment
		ok   bool
	}{
		{"//kama:ignore", Comment{"kama", "ignore", ""}, true},
		{
			"//go:generate go run ./gen -o x.go",
			Comment{"go", "generate", "go run ./gen -o x.go"},
			true,
		},
		{"//kama:ignore  a\tb ", Comment{"kama", "ignore", " a\tb "}, true},
		{"//kama:ignore\ta", Comment{"kama", "ignore", "a"}, true},
		{"//kama:a:b c:d", Comment{"kama", "a:b", "c:d"}, true},
		{"//Kama:Ignore", Comment{"Kama", "Ignore", ""}, true},
		{"//ka-ma:x", Comment{"ka-ma", "x", ""}, true},
		{"//nolint", Comment{"", "nolint", ""}, true},
		{"//line x.go:10", Comment{"", "line", "x.go:10"}, true},
		{"//kama:", Comment{}, false},
		{"//kama: ignore", Comment{}, false},
		{"//:ignore", Comment{}, false},
		{"//:", Comment{}, false},
		{"// kama:ignore", Comment{}, false},
		{"//\tkama:ignore", Comment{}, false},
		{"//\u00a0kama:ignore", Comment{}, false},
		{"//", Comment{}, false},
		{"/*kama:ignore*/", Comment{}, false},
		{"// Note: not a directive", Comment{}, false},
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			got, ok := Parse(c.text)
			assert.Equal(t, ok, c.ok)
			assert.Equal(t, got, c.want)
		})
	}
}

func TestAst(t *testing.T) {
	_, f := parse(t, `package x

//kama:ignore

// Doc text.
//
//kama:ignore
// more text
//go:generate echo
//kama:generated

/*
kama:ignore
*/

// kama:ignore
`)

	want := [][]Comment{
		{{"kama", "ignore", ""}},
		{{"kama", "ignore", ""}, {"go", "generate", "echo"}, {"kama", "generated", ""}},
		nil,
		nil,
	}

	assert.Equal(t, len(f.Comments), len(want))
	for i, cg := range f.Comments {
		assert.DeepEqual(t, Ast(cg), want[i])
	}
	assert.Assert(t, Ast(nil) == nil)
}

func TestDst(t *testing.T) {
	df := decorate(t, `package x

//kama:ignore
type A struct{}

// aaaa
//kama:ignore
//go:noinline
// bbbb
func (a A) M() {}

/* block */
//kama:generated
type B struct{}

// not a directive
type C struct{}

//kama:ignore

// foo
//kama:generated
type D struct{}

//kama:ignore

type E struct{}
`)

	want := [][]Comment{
		{{"kama", "ignore", ""}},
		{{"kama", "ignore", ""}, {"go", "noinline", ""}},
		{{"kama", "generated", ""}},
		nil,
		// Comments before an empty line are not attached to the node.
		{{"kama", "generated", ""}},
		nil,
	}

	assert.Equal(t, len(df.Decls), len(want))
	for i, decl := range df.Decls {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			assert.DeepEqual(t, Dst(*decl.Decorations()), want[i])
		})
	}
}

const fieldSrc = "package x\n" +
	"type A struct {\n" +
	"\t//kama:skipped\n" +
	"\n" +
	"\t//kama:a1\n" +
	"\t/* block */ A /* 5 */ string /* 6 */ `json:\"a\"` //kama:a2\n" +
	"\t/* block */\n" +
	"\t//kama:b1\n" +
	"\tB string /* block */ //kama:b2\n" +
	"\t//kama:c1\n" +
	"\t// not a directive\n" +
	"\tC int //kama:c2\n" +
	"\t// kama:d0\n" +
	"\tD int //kama:d1\n" +
	"}\n"

func TestAstField(t *testing.T) {
	_, f := parse(t, fieldSrc)
	fields := f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.TypeSpec).Type.(*ast.StructType).Fields.List

	want := map[string][]Comment{
		// go/ast attaches no doc comment to a field whose line starts with a
		// block comment, unlike dst.
		"A": {{"kama", "a2", ""}},
		// Only the first trailing comment counts, and here it is a block.
		"B": {{"kama", "b1", ""}},
		"C": {{"kama", "c1", ""}, {"kama", "c2", ""}},
		"D": {{"kama", "d1", ""}},
	}

	for _, fl := range fields {
		name := fl.Names[0].Name
		assert.DeepEqual(t, AstField(fl), want[name])
	}
}

func TestDstField(t *testing.T) {
	df := decorate(t, fieldSrc)
	fields := df.Decls[0].(*dst.GenDecl).Specs[0].(*dst.TypeSpec).Type.(*dst.StructType).Fields.List

	want := map[string][]Comment{
		"A": {{"kama", "a1", ""}, {"kama", "a2", ""}},
		"B": {{"kama", "b1", ""}},
		"C": {{"kama", "c1", ""}, {"kama", "c2", ""}},
		"D": {{"kama", "d1", ""}},
	}

	for _, fl := range fields {
		name := fl.Names[0].Name
		assert.DeepEqual(t, DstField(fl.Decs), want[name])
	}
}
