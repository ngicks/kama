package typequery

import (
	"go/token"
	"go/types"
	"testing"

	"gotest.tools/v3/assert"
)

func newGeneric(t *testing.T, pkgPath, name string) *types.Named {
	t.Helper()

	pkg := types.NewPackage(pkgPath, pkgPath)
	tp := types.NewTypeParam(
		types.NewTypeName(token.NoPos, pkg, "T", nil),
		types.Universe.Lookup("any").Type(),
	)
	named := types.NewNamed(
		types.NewTypeName(token.NoPos, pkg, name, nil),
		types.NewStruct(nil, nil),
		nil,
	)
	named.SetTypeParams([]*types.TypeParam{tp})

	return named
}

func instantiate(t *testing.T, generic *types.Named, arg types.Type) types.Type {
	t.Helper()

	inst, err := types.Instantiate(nil, generic, []types.Type{arg}, true)
	assert.NilError(t, err)

	return inst
}

func newAlias(rhs types.Type) *types.Alias {
	pkg := types.NewPackage("example.com/local", "local")
	return types.NewAlias(types.NewTypeName(token.NoPos, pkg, "A", nil), rhs)
}

func TestIsNamed(t *testing.T) {
	option := instantiate(t, newGeneric(t, OptPath, "Option"), types.Typ[types.Int])

	type testCase struct {
		name    string
		t       types.Type
		pkgPath string
		typName string
		want    bool
	}

	for _, tc := range []testCase{
		{"instance", option, OptPath, "Option", true},
		{"alias", newAlias(option), OptPath, "Option", true},
		{"other path", option, UndPath, "Option", false},
		{"other name", option, OptPath, "Und", false},
		{"pointer", types.NewPointer(option), OptPath, "Option", false},
		{"universe", types.Universe.Lookup("error").Type(), "", "error", false},
		{"basic", types.Typ[types.Int], "", "int", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsNamed(tc.t, tc.pkgPath, tc.typName))
		})
	}
}

func TestTypeArg(t *testing.T) {
	und := instantiate(t, newGeneric(t, UndPath, "Und"), types.Typ[types.String])

	assert.Equal(t, types.Typ[types.String], TypeArg(und))
	assert.Equal(t, types.Typ[types.String], TypeArg(newAlias(und)))
}

func TestBasicInfo(t *testing.T) {
	pkg := types.NewPackage("example.com/local", "local")
	named := types.NewNamed(
		types.NewTypeName(token.NoPos, pkg, "S", nil),
		types.Typ[types.String],
		nil,
	)
	param := types.NewTypeParam(
		types.NewTypeName(token.NoPos, pkg, "T", nil),
		types.NewInterfaceType(nil, []types.Type{types.Typ[types.Int]}),
	)

	type testCase struct {
		name string
		t    types.Type
		want types.BasicInfo
	}

	for _, tc := range []testCase{
		{"basic", types.Typ[types.Int], types.IsInteger},
		{"named", named, types.IsString},
		{"alias", newAlias(types.Typ[types.Bool]), types.IsBoolean},
		{"slice", types.NewSlice(types.Typ[types.Int]), 0},
		{"type parameter", param, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, BasicInfo(tc.t))
		})
	}
}

func TestIsStructSliceOrMap(t *testing.T) {
	pkg := types.NewPackage("example.com/local", "local")
	named := func(name string, underlying types.Type) types.Type {
		return types.NewNamed(types.NewTypeName(token.NoPos, pkg, name, nil), underlying, nil)
	}

	type testCase struct {
		name string
		t    types.Type
		want bool
	}

	for _, tc := range []testCase{
		{"struct", named("S", types.NewStruct(nil, nil)), true},
		{"slice", named("L", types.NewSlice(types.Typ[types.Int])), true},
		{"map", named("M", types.NewMap(types.Typ[types.String], types.Typ[types.Int])), true},
		{"array", named("A", types.NewArray(types.Typ[types.Int], 2)), false},
		{"pointer", named("P", types.NewPointer(types.Typ[types.Int])), false},
		{"basic", named("B", types.Typ[types.Int]), false},
		{"literal slice", types.NewSlice(types.Typ[types.Int]), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsStructSliceOrMap(tc.t))
		})
	}
}
