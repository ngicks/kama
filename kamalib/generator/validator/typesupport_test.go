package validator

import (
	"go/types"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
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
			"//kama:validate\n//kama:validate hook=off\n" +
			"type A struct {\n" +
			"\tB sub.Bag[Inner]\n" +
			"\tH sub.Box[Inner]\n" +
			"\t//kama:validate length=\">=1\"\n" +
			"\tS sub.Box[string]\n" +
			"}\n\n" +
			"//kama:validate\n//kama:validate hook=off\n" +
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
