package genmark

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

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

	r := NewRegistry("tool")
	for _, name := range names {
		assert.NilError(t, r.Register(name))
	}

	return r
}

func TestMarker(t *testing.T) {
	m := Marker("tool", "tool-partial")
	assert.Equal(t, m, "//tool:generated tool-partial")

	c, ok := dircmnt.Parse(m)
	assert.Assert(t, ok)
	assert.DeepEqual(
		t,
		c,
		dircmnt.Comment{ToolName: "tool", Directive: "generated", Args: "tool-partial"},
	)
}

func TestRegister(t *testing.T) {
	r := NewRegistry("tool")
	assert.NilError(t, r.Register("tool-partial"))
	assert.ErrorIs(t, r.Register("tool-partial"), ErrDuplicate)

	for _, name := range []string{"", " ", "tool partial", "tool\tpartial", "tool-partial\n"} {
		assert.ErrorIs(t, r.Register(name), ErrInvalidName, "name = %q", name)
	}
}

func TestGenerated(t *testing.T) {
	r := newRegistry(t, "tool-partial", "tool-clone", "acme:partial")

	cases := []struct {
		doc  string
		name string
		ok   bool
	}{
		{"//tool:generated tool-partial", "tool-partial", true},
		{"// Foo is generated.\n//tool:generated tool-clone", "tool-clone", true},
		{"//tool:generated  tool-partial extra", "tool-partial", true},
		{"//tool:generated acme:partial", "acme:partial", true},
		{"//tool:generated unknown\n//tool:generated tool-clone", "tool-clone", true},
		{"//tool:generated unknown", "", false},
		{"//tool:generated", "", false},
		{"//other:generated tool-partial", "", false},
		{"//tool:ignore tool-partial", "", false},
		{"// tool:generated tool-partial", "", false},
		{"/*tool:generated tool-partial*/", "", false},
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
	r := newRegistry(t, "tool-partial")
	name, ok := r.Generated(nil)
	assert.Assert(t, !ok)
	assert.Equal(t, name, "")
}

func TestGeneratedOfAnotherTool(t *testing.T) {
	a := NewRegistry("a")
	assert.NilError(t, a.Register("gen"))

	b := NewRegistry("b")
	assert.NilError(t, b.Register("gen"))

	_, f := parse(t, "package x\n\n"+Marker("b", "gen")+"\ntype Foo int\n")
	doc := f.Decls[0].(*ast.GenDecl).Doc

	_, ok := a.Generated(doc)
	assert.Assert(t, !ok)

	name, ok := b.Generated(doc)
	assert.Assert(t, ok)
	assert.Equal(t, name, "gen")
}

func TestRegistryConcurrent(t *testing.T) {
	r := NewRegistry("tool")
	_, f := parse(t, "package x\n\n"+Marker("tool", "gen-0")+"\ntype Foo int\n")
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
