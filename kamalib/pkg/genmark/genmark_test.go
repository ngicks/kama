package genmark

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
)

func parse(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments|parser.AllErrors)
	assert.NilError(t, err)
	return fset, f
}

func newRegistry(t *testing.T, names ...string) *Registry {
	t.Helper()

	r := NewRegistry()
	for _, name := range names {
		assert.NilError(t, r.Register(name))
	}
	return r
}

func TestMarker(t *testing.T) {
	m := Marker("kama-partial")
	assert.Equal(t, m, "//kama:generated kama-partial")

	c, ok := dircmnt.Parse(m)
	assert.Assert(t, ok)
	assert.DeepEqual(
		t,
		c,
		dircmnt.Comment{ToolName: "kama", Directive: "generated", Args: "kama-partial"},
	)
}

func TestRegister(t *testing.T) {
	r := NewRegistry()
	assert.NilError(t, r.Register("kama-partial"))
	assert.ErrorIs(t, r.Register("kama-partial"), ErrDuplicate)

	for _, name := range []string{"", " ", "kama partial", "kama\tpartial", "kama-partial\n"} {
		assert.ErrorIs(t, r.Register(name), ErrInvalidName, "name = %q", name)
	}
}

func TestGenerated(t *testing.T) {
	r := newRegistry(t, "kama-partial", "kama-clone")

	cases := []struct {
		doc  string
		name string
		ok   bool
	}{
		{"//kama:generated kama-partial", "kama-partial", true},
		{"// Foo is generated.\n//kama:generated kama-clone", "kama-clone", true},
		{"//kama:generated  kama-partial extra", "kama-partial", true},
		{"//kama:generated unknown\n//kama:generated kama-clone", "kama-clone", true},
		{"//kama:generated unknown", "", false},
		{"//kama:generated", "", false},
		{"//other:generated kama-partial", "", false},
		{"//kama:ignore kama-partial", "", false},
		{"// kama:generated kama-partial", "", false},
		{"/*kama:generated kama-partial*/", "", false},
		{"// Foo is plain.", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.doc, func(t *testing.T) {
			_, f := parse(t, "package x\n\n"+tc.doc+"\ntype Foo int\n")
			name, ok := r.Generated(f.Decls[0].(*ast.GenDecl).Doc)
			assert.Equal(t, ok, tc.ok)
			assert.Equal(t, name, tc.name)
		})
	}
}

func TestGeneratedNil(t *testing.T) {
	r := newRegistry(t, "kama-partial")
	name, ok := r.Generated(nil)
	assert.Assert(t, !ok)
	assert.Equal(t, name, "")
}

func TestGeneratedDst(t *testing.T) {
	r := newRegistry(t, "kama-partial")

	src := `package x

//kama:generated kama-partial
type Foo int

//kama:generated kama-partial

type Bar int

//kama:generated unknown
type Baz int
`
	fset, f := parse(t, src)
	df, err := decorator.NewDecorator(fset).DecorateFile(f)
	assert.NilError(t, err)

	want := []bool{true, false, false}
	for i, decl := range df.Decls {
		name, ok := r.GeneratedDst(decl.(*dst.GenDecl).Decs.NodeDecs)
		assert.Equal(t, ok, want[i], "decl %d", i)
		if ok {
			assert.Equal(t, name, "kama-partial")
		}
	}
}

func TestRegistryConcurrent(t *testing.T) {
	r := NewRegistry()
	_, f := parse(t, "package x\n\n"+Marker("gen-0")+"\ntype Foo int\n")
	doc := f.Decls[0].(*ast.GenDecl).Doc

	var g errgroup.Group
	for i := range 16 {
		g.Go(func() error {
			r.Generated(doc)
			err := r.Register(fmt.Sprintf("gen-%d", i%8))
			if errors.Is(err, ErrDuplicate) {
				return nil
			}
			return err
		})
	}
	assert.NilError(t, g.Wait())

	name, ok := r.Generated(doc)
	assert.Assert(t, ok)
	assert.Equal(t, name, "gen-0")
}
