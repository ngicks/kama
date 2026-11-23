package base_test

import (
	"go/types"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
)

const markerSrc = `package x

// T is marked.
//
//kama:generated kama-x
type T int

// M is marked.
//
//kama:generated kama-x
func (T) M() {}

// F is marked.
//
//kama:generated kama-x
func F() {}

// V is marked.
//
//kama:generated kama-x
var V, W int

const (
	// C is marked.
	//
	//kama:generated kama-x
	C = 1
	D = 2
)

// Other is marked by another generator.
//
//kama:generated kama-other
type Other int

type Plain int
`

func TestGenerated(t *testing.T) {
	pkgs := base.SourcePackages(gentest.Load(t, map[string]string{"a.go": markerSrc}))
	scope := pkgs[0].Types.Scope()
	method, _, _ := types.LookupFieldOrMethod(scope.Lookup("T").Type(), false, pkgs[0].Types, "M")

	reg := genmark.NewRegistry()
	assert.NilError(t, reg.Register("kama-x"))

	for _, obj := range []types.Object{
		scope.Lookup("T"), method, scope.Lookup("F"), scope.Lookup("V"), scope.Lookup("W"),
		scope.Lookup("C"),
	} {
		assert.Assert(t, base.Generated(pkgs, reg, obj), obj.Name())
	}

	for _, obj := range []types.Object{
		scope.Lookup("D"), scope.Lookup("Other"), scope.Lookup("Plain"),
		types.Universe.Lookup("int"),
	} {
		assert.Assert(t, !base.Generated(pkgs, reg, obj), obj.Name())
	}

	// An object of a package other than pkgs is not generated.
	assert.Assert(t, !base.Generated(nil, reg, scope.Lookup("T")))
}

func TestUnownedDecls(t *testing.T) {
	pkgs := base.SourcePackages(gentest.Load(t, map[string]string{"a.go": markerSrc}))

	reg := genmark.NewRegistry()
	assert.NilError(t, reg.Register("kama-x"))

	var names []string
	for _, obj := range base.UnownedDecls(
		pkgs[0], reg, []string{"Plain", "T", "missing", "Other", "F", "D"},
	) {
		names = append(names, obj.Name())
	}

	assert.DeepEqual(t, names, []string{"Plain", "Other", "D"})
}

func TestDeclDocs(t *testing.T) {
	pkgs := base.SourcePackages(gentest.Load(t, map[string]string{"a.go": markerSrc}))
	scope := pkgs[0].Types.Scope()
	texts := func(name string) []string {
		t.Helper()

		var out []string
		for _, doc := range base.DeclDocs(pkgs[0], scope.Lookup(name)) {
			out = append(out, doc.Text())
		}

		return out
	}

	assert.DeepEqual(t, texts("T"), []string{"T is marked.\n", ""})
	assert.DeepEqual(t, texts("F"), []string{"F is marked.\n"})
	assert.DeepEqual(t, texts("W"), []string{"V is marked.\n", ""})
	assert.DeepEqual(t, texts("C"), []string{"", "C is marked.\n"})
	assert.DeepEqual(t, texts("D"), []string{"", ""})

	local := types.NewVar(scope.Lookup("T").Pos()+1, pkgs[0].Types, "local", types.Typ[types.Int])
	assert.Assert(t, base.DeclDocs(pkgs[0], local) == nil)
}
