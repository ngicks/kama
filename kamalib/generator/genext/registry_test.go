package genext

import (
	"go/types"
	"testing"

	"gotest.tools/v3/assert"
)

// support is a type support claiming nothing.
type support struct{ name, tag string }

func (s support) Name() string { return s.name }

func TestRegistry(t *testing.T) {
	tagged := func(r *Registry) []string {
		var out []string
		for s := range r.TypeSupports() {
			out = append(out, s.Name()+s.(support).tag)
		}

		return out
	}

	reg := NewRegistry(support{name: "a"}, support{name: "b"})

	clone := reg.Clone()
	clone.RegisterTypeSupport(support{name: "c"})
	assert.DeepEqual(t, tagged(reg), []string{"a", "b"})

	// A later support of a name replaces the earlier one in its place.
	reg.RegisterTypeSupport(support{name: "a", tag: "2"})
	assert.DeepEqual(t, tagged(reg), []string{"a2", "b"})
	assert.DeepEqual(t, tagged(clone), []string{"a", "b", "c"})

	_, _, _, ok := HeldValue(reg, types.Typ[types.Int], nil)
	assert.Assert(t, !ok)

	held, _, _, ok := HeldValue(nil, types.NewPointer(types.Typ[types.Int]), nil)
	assert.Assert(t, ok)
	assert.Equal(t, held, types.Type(types.Typ[types.Int]))
}

func TestRegisterTypeSupport(t *testing.T) {
	RegisterTypeSupport(support{name: "genext-test-support"})
	assert.DeepEqual(t, names(DefaultRegistry()), []string{"genext-test-support"})

	// DefaultRegistry returns a clone, which a change never leaks out of.
	DefaultRegistry().RegisterTypeSupport(support{name: "genext-test-clone"})
	assert.DeepEqual(t, names(DefaultRegistry()), []string{"genext-test-support"})

	defer func() {
		err, ok := recover().(error)
		assert.Assert(t, ok)
		assert.ErrorContains(t, err, "type support genext-test-support is registered twice")
	}()

	RegisterTypeSupport(support{name: "genext-test-support"})
}

func names(r *Registry) []string {
	var out []string
	for s := range r.TypeSupports() {
		out = append(out, s.Name())
	}

	return out
}
