package base_test

import (
	"errors"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

const graphSrc = `package x

type S struct{ I I }

type I int

type Al = S

//kama:generated kama-x
type G struct{}
`

func TestTypeGraph(t *testing.T) {
	pkgs := base.SourcePackages(gentest.Load(t, map[string]string{"a.go": graphSrc}))
	pkgPath := pkgs[0].PkgPath

	reg := genmark.NewRegistry(base.ToolName)
	assert.NilError(t, reg.Register("kama-x"))

	g, err := base.TypeGraph(pkgs, reg)
	assert.NilError(t, err)

	for name, want := range map[string]bool{"S": true, "I": true, "G": false} {
		_, ok := g.Get(typegraph.NewQualifiedType(pkgPath, name))
		assert.Equal(t, ok, want, name)
	}
}

func TestMarkedTypeError(t *testing.T) {
	pkgs := base.SourcePackages(gentest.Load(t, map[string]string{"a.go": graphSrc}))
	pkgPath := pkgs[0].PkgPath

	reg := genmark.NewRegistry(base.ToolName)
	assert.NilError(t, reg.Register("kama-x"))

	g, err := base.TypeGraph(pkgs, reg)
	assert.NilError(t, err)

	errUnsupported := errors.New("unsupported")
	markedErr := func(name string) error {
		id := typegraph.NewQualifiedType(pkgPath, name)
		return base.MarkedTypeError(pkgs, reg, g, id, "a.go:1:1", errUnsupported)
	}

	assert.NilError(t, markedErr("S"))
	assert.NilError(t, markedErr("G"))

	err = markedErr("I")
	assert.ErrorIs(t, err, errUnsupported)
	assert.Error(
		t,
		err,
		"unsupported: a.go:1:1: "+pkgPath+".I has underlying type int; want a struct, slice or map",
	)

	err = markedErr("Al")
	assert.ErrorIs(t, err, errUnsupported)
	assert.Error(
		t,
		err,
		"unsupported: a.go:1:1: "+pkgPath+".Al is an alias; mark the type it denotes",
	)
}
