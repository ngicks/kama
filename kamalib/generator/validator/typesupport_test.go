package validator

import (
	"go/token"
	"go/types"
	"slices"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

// bagSupport is the type support of the test container sub.Bag, whose All
// yields its elements by index, and of the test holder sub.Box, which holds
// a value when Has reports true.
type bagSupport struct{ path string }

func (bagSupport) Name() string { return "bag" }

func (s bagSupport) Held(t types.Type, v ext.Gen) (string, types.Type, ext.Gen, ext.Gen, bool) {
	if !s.is(t, "Box") {
		return "", nil, nil, nil, false
	}

	return "t", t.(*types.Named).TypeArgs().At(0),
		lang.Expr("%s.Has()", v), lang.Expr("%s.Get()", v), true
}

func (s bagSupport) Elements(
	t types.Type,
	v ext.Gen,
) (string, types.Type, ext.Gen, ext.Segment, bool) {
	if !s.is(t, "Bag") {
		return "", nil, nil, 0, false
	}

	return "e", t.(*types.Named).TypeArgs().At(0),
		lang.Expr("%s.All()", v), ext.SegmentIndex, true
}

func (s bagSupport) is(t types.Type, name string) bool {
	named, ok := types.Unalias(t).(*types.Named)

	return ok && named.Obj().Pkg() != nil &&
		named.Obj().Pkg().Path() == s.path && named.Obj().Name() == name
}

const typeSupportSub = `package sub

import "iter"

type Bag[T any] struct{ items []T }

func (b Bag[T]) All() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, v := range b.items {
			if !yield(i, v) {
				return
			}
		}
	}
}

type Box[T any] struct {
	v  T
	ok bool
}

func (b Box[T]) Has() bool { return b.ok }

func (b Box[T]) Get() T { return b.v }
`

// TestTypeSupport registers a container and a holder of a third party
// package in a private registry. The nested walk ranges over the elements
// of the container and guards the value of the holder, and a rule rejecting
// the holder applies to the value it holds.
func TestTypeSupport(t *testing.T) {
	files := map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:validate\n//kama:validate hook=False\n" +
			"type A struct {\n" +
			"\tB sub.Bag[Inner]\n" +
			"\tH sub.Box[Inner]\n" +
			"\t//kama:validate length(ge=1)\n" +
			"\tS sub.Box[string]\n" +
			"}\n\n" +
			"//kama:validate\n//kama:validate hook=False\n" +
			"type Inner struct {\n\t//kama:validate required\n\tP *int\n}\n",
		"sub/sub.go": typeSupportSub,
	}

	reg := ext.DefaultRegistry().Clone()
	reg.RegisterTypeSupport(bagSupport{path: modulePath(t) + "/sub"})

	f, opts := stage(t, files)
	out, err := generateInto(t, f, opts, Options{Registry: reg})
	assert.NilError(t, err)
	assert.NilError(t, gentest.Build(f, opts))

	assertContains(t, out["a.kama_validate.go"],
		"for i, e := range v.B.All() {",
		check(`validator.Field(validator.Index(e.Validate(stopOnFirstError), i), "B")`),
		"if v.H.Has() {",
		check(`validator.Field(v.H.Get().Validate(stopOnFirstError), "H")`),
		"if v.S.Has() {",
		"kamaValidateLength(len(v.S.Get())",
	)

	// Without the support the walk ends at the types, and length rejects
	// the holder.
	_, err = generate(t, files, testRun{})
	assert.ErrorIs(t, err, ErrUnsupported)
}

// TestRoles lists the roles of the built-in kinds and of the types of
// github.com/ngicks/option, and reaches the value of each role.
func TestRoles(t *testing.T) {
	pkgs := gentest.Load(t, map[string]string{
		"a.go": `package x

import (
	"github.com/ngicks/option/either"
	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/tuple"
	"github.com/ngicks/option/und"
)

type A struct {
	S  []int
	R  [2]int
	M  map[string]int
	P  *int
	O  opt.Option[int]
	U  und.Und[int]
	E  elastic.Elastic[int]
	EI either.Either[int, string]
	RS either.Result[int]
	UE either.UntaggedEither[int, string]
	T3 tuple.Tuple3[int, string, bool]
}
`,
	})

	reg := ext.DefaultRegistry()
	st := pkgs[0].Types.Scope().Lookup("A").Type().Underlying().(*types.Struct)

	want := map[string][]string{
		"S": {"e"}, "R": {"e"}, "M": {"k", "v"}, "P": {"e"},
		"O": {"t"}, "U": {"t"}, "E": {"t"},
		"EI": {"l", "r"}, "RS": {"ok", "err"}, "UE": {"l", "r"},
		"T3": {"v1", "v2", "v3"},
	}

	reserved := append([]string{"hook", "pre", "post"}, reg.Names()...)

	for field := range st.Fields() {
		roles := ext.Roles(reg, field.Type())
		assert.DeepEqual(t, roles, want[field.Name()])

		for _, role := range roles {
			assert.Assert(t, token.IsIdentifier(role), role)
			assert.Assert(t, !slices.Contains(reserved, role), role)

			held, _, _, _, _, _, ok := ext.RoleValue(
				reg, field.Type(), lang.Expr("x"), role, lang.Expr("p"), lang.Expr("e"),
			)
			assert.Assert(t, ok, "%s %s", field.Name(), role)
			assert.Assert(t, held != nil, "%s %s", field.Name(), role)
		}

		_, _, _, _, _, _, ok := ext.RoleValue(reg, field.Type(), nil, "nope", nil, nil)
		assert.Assert(t, !ok)
	}

	m := st.Field(2).Type()
	key, _, over, value, segment, label, _ := ext.RoleValue(
		reg, m, lang.Expr("x"), "k", lang.Expr("p"), lang.Expr("e"),
	)
	assert.Equal(t, key, types.Type(types.Typ[types.String]))
	assert.Assert(t, over != nil)
	assert.Equal(t, segment, ext.SegmentKey)
	assert.Equal(t, render(t, value), "p")
	assert.Equal(t, render(t, label), "p")

	tup := st.Field(10).Type()
	_, guard, over, value, segment, label, _ := ext.RoleValue(
		reg,
		tup,
		lang.Expr("x"),
		"v2",
		nil,
		nil,
	)
	assert.Assert(t, guard == nil && over == nil)
	assert.Equal(t, segment, ext.SegmentField)
	assert.Equal(t, render(t, value), "x.V2")
	assert.Equal(t, render(t, label), `"V2"`)
}

// render renders g in a context of its own.
func render(t *testing.T, g ext.Gen) string {
	t.Helper()

	text, err := g(genhelper.NewBaseContext(genhelper.NewScope(), nil))
	assert.NilError(t, err)

	return text
}
