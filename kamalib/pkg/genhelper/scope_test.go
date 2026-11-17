package genhelper_test

import (
	"go/types"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
)

const (
	linkDestPath = "example.com/dest"
	linkVPath    = "example.com/v"
)

func newImports(t *testing.T) *importdecl.Map {
	t.Helper()

	pkg := types.NewPackage(linkDestPath, "dest")
	pkg.SetImports([]*types.Package{types.NewPackage(linkVPath, "v")})
	m, err := importdecl.NewParser(pkg).ParseAst(nil)
	assert.NilError(t, err)

	return m
}

type fakeImports struct {
	idents   map[string]bool
	reserved map[string]bool
}

func newFakeImports(idents ...string) *fakeImports {
	f := &fakeImports{idents: map[string]bool{}, reserved: map[string]bool{}}
	for _, ident := range idents {
		f.idents[ident] = true
	}

	return f
}

func (f *fakeImports) HasIdent(ident string) bool { return f.idents[ident] }

func (f *fakeImports) Reserve(idents ...string) {
	for _, ident := range idents {
		f.reserved[ident] = true
	}
}

func TestLinkImports(t *testing.T) {
	t.Run("an import requested later avoids a local", func(t *testing.T) {
		m := newImports(t)
		s := genhelper.NewScope()
		s.LinkImports(m)

		assert.Equal(t, s.Push().Declare("v"), "v")

		got, _ := m.Ident(linkVPath)
		assert.Equal(t, got, "v_2")
	})

	t.Run("a local declared later avoids an import", func(t *testing.T) {
		m := newImports(t)
		s := genhelper.NewScope()
		s.LinkImports(m)

		got, _ := m.Ident(linkVPath)
		assert.Equal(t, got, "v")

		fn := s.Push()
		assert.Equal(t, fn.Declare("v"), "v2")
		assert.Equal(t, fn.Use("w"), "w")
		name, declared := fn.Push().LookupOrDeclare("v")
		assert.Equal(t, name, "v2")
		assert.Assert(t, !declared)
		name, declared = s.Push().LookupOrDeclare("v")
		assert.Equal(t, name, "v2")
		assert.Assert(t, declared)
	})

	t.Run("sibling scopes reuse names", func(t *testing.T) {
		m := newImports(t)
		s := genhelper.NewScope()
		s.LinkImports(m)

		assert.Equal(t, s.Push().Declare("x"), "x")
		assert.Equal(t, s.Push().Declare("x"), "x")
		assert.Equal(t, s.Push().Use("x"), "x")
	})

	t.Run("linking pushes the names the tree holds", func(t *testing.T) {
		m := newImports(t)
		s := genhelper.NewScope()
		fn := s.Push()
		assert.Equal(t, fn.Push().Declare("v"), "v")

		fn.LinkImports(m)
		got, _ := m.Ident(linkVPath)
		assert.Equal(t, got, "v_2")
	})

	t.Run("Shadow may take an import identifier", func(t *testing.T) {
		m := newImports(t)
		s := genhelper.NewScope()
		s.LinkImports(m)
		got, _ := m.Ident(linkVPath)
		assert.Equal(t, got, "v")

		name, err := s.Push().Shadow("v")
		assert.NilError(t, err)
		assert.Equal(t, name, "v")
	})

	t.Run("NewBaseContext links the scope", func(t *testing.T) {
		m := newImports(t)
		c := genhelper.NewBaseContext(
			genhelper.NewScope(),
			&genhelper.Package{Imports: m, Path: linkDestPath},
		)

		assert.Equal(t, c.Scope().Push().Declare("v"), "v")

		got, _ := m.Ident(linkVPath)
		assert.Equal(t, got, "v_2")
	})

	t.Run("every held name is reserved", func(t *testing.T) {
		f := newFakeImports("imp")
		s := genhelper.NewScope()
		s.Reserve("r")
		before := s.Push()
		before.Declare("a")

		s.LinkImports(f)
		assert.DeepEqual(t, f.reserved, map[string]bool{"r": true, "a": true})

		fn := s.Push()
		fn.Reserve("r2")
		fn.Declare("imp")
		fn.Use("u")
		fn.LookupOrDeclare("l")
		_, err := fn.Push().Shadow("imp")
		assert.NilError(t, err)
		before.Declare("b")
		assert.DeepEqual(t, f.reserved, map[string]bool{
			"r": true, "a": true, "r2": true, "imp2": true, "u": true,
			"l": true, "imp": true, "b": true,
		})
	})

	t.Run("relinking replaces the link and nil unlinks", func(t *testing.T) {
		first := newFakeImports("x")
		second := newFakeImports()
		s := genhelper.NewScope()
		s.LinkImports(first)
		assert.Equal(t, s.Push().Declare("x"), "x2")

		s.LinkImports(second)
		assert.DeepEqual(t, second.reserved, map[string]bool{"x2": true})
		assert.Equal(t, s.Push().Declare("x"), "x")
		assert.DeepEqual(t, second.reserved, map[string]bool{"x2": true, "x": true})

		s.LinkImports(nil)
		s.Push().Declare("y")
		assert.Assert(t, !second.reserved["y"])
	})
}
