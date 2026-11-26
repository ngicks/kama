package ext

import (
	"errors"
	"go/types"
	"slices"
	"testing"

	"gotest.tools/v3/assert"
)

// named is a validator registered under name.
type named struct{ name string }

func (n named) Name() string { return n.name }

func (named) Parse(string, Resolver) (Rule, error) { return nil, errors.New("unused") }

func TestRegistry(t *testing.T) {
	reg := NewRegistry(named{name: "required"}, named{name: "a"})
	assert.DeepEqual(t, reg.Names(), []string{"a", "required"})

	v, ok := reg.Lookup("a")
	assert.Assert(t, ok)
	assert.Equal(t, v, Validator(named{name: "a"}))

	_, ok = reg.Lookup("b")
	assert.Assert(t, !ok)

	_, err := reg.Resolve("b", "")
	assert.ErrorIs(t, err, ErrUnknown)
	assert.ErrorIs(t, err, ErrArgs)

	clone := reg.Clone()
	clone.Register(named{name: "b"})
	assert.DeepEqual(t, clone.Names(), []string{"a", "b", "required"})
	assert.DeepEqual(t, reg.Names(), []string{"a", "required"})

	// A later validator of a name replaces the earlier one.
	reg.Register(named{name: "a"})
	assert.DeepEqual(t, reg.Names(), []string{"a", "required"})
}

func TestRegister(t *testing.T) {
	Register(named{name: "ext-test-register"})
	assert.Assert(t, slices.Contains(DefaultRegistry().Names(), "ext-test-register"))

	// DefaultRegistry returns a clone, which a change never leaks out of.
	DefaultRegistry().Register(named{name: "ext-test-clone"})
	assert.Assert(t, !slices.Contains(DefaultRegistry().Names(), "ext-test-clone"))

	defer func() {
		r := recover()
		err, ok := r.(error)
		assert.Assert(t, ok, "recovered %v", r)
		assert.ErrorContains(t, err, "validator ext-test-register is registered twice")
	}()

	Register(named{name: "ext-test-register"})
}

// support is a type support claiming nothing.
type support struct{ name, tag string }

func (s support) Name() string { return s.name }

func TestRegistryTypeSupport(t *testing.T) {
	names := func(r *Registry) []string {
		var out []string
		for s := range r.TypeSupports() {
			out = append(out, s.Name()+s.(support).tag)
		}

		return out
	}

	reg := NewRegistry()
	reg.RegisterTypeSupport(support{name: "a"})
	reg.RegisterTypeSupport(support{name: "b"})

	clone := reg.Clone()
	clone.RegisterTypeSupport(support{name: "c"})
	assert.DeepEqual(t, names(reg), []string{"a", "b"})

	// A later support of a name replaces the earlier one in its place.
	reg.RegisterTypeSupport(support{name: "a", tag: "2"})
	assert.DeepEqual(t, names(reg), []string{"a2", "b"})
	assert.DeepEqual(t, names(clone), []string{"a", "b", "c"})

	_, _, _, ok := HeldValue(reg, types.Typ[types.Int], nil)
	assert.Assert(t, !ok)

	held, _, _, ok := HeldValue(nil, types.NewPointer(types.Typ[types.Int]), nil)
	assert.Assert(t, ok)
	assert.Equal(t, held, types.Type(types.Typ[types.Int]))

	RegisterTypeSupport(support{name: "ext-test-support"})

	defer func() {
		err, ok := recover().(error)
		assert.Assert(t, ok)
		assert.ErrorContains(t, err, "type support ext-test-support is registered twice")
	}()

	RegisterTypeSupport(support{name: "ext-test-support"})
}
