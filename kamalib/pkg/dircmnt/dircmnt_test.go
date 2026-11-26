package dircmnt

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"gotest.tools/v3/assert"
)

func parse(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments|parser.AllErrors)
	assert.NilError(t, err)

	return fset, f
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
