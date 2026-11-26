package ext

import (
	"errors"
	"slices"
	"testing"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"

	"gotest.tools/v3/assert"
)

// named is a validator registered under name.
type named struct{ name string }

func (n named) Name() string { return n.name }

func (named) Parse(expr.Call, Resolver) (Rule, error) { return nil, errors.New("unused") }

func TestRegistry(t *testing.T) {
	reg := NewRegistry(named{name: "required"}, named{name: "a"})
	assert.DeepEqual(t, reg.Names(), []string{"a", "required"})

	v, ok := reg.Lookup("a")
	assert.Assert(t, ok)
	assert.Equal(t, v, Validator(named{name: "a"}))

	_, ok = reg.Lookup("b")
	assert.Assert(t, !ok)

	_, err := reg.Resolve(expr.Call{Name: "b"})
	assert.ErrorIs(t, err, ErrUnknown)
	assert.ErrorIs(t, err, ErrArgs)

	_, err = reg.Resolve(expr.Number{Text: "1"})
	assert.ErrorIs(t, err, ErrUnknown)

	_, err = reg.Resolve(expr.And{})
	assert.ErrorIs(t, err, ErrUnknown)

	var got expr.Node

	reg.RegisterOperator(expr.And{}, func(n expr.Node, _ Resolver) (Rule, error) {
		got = n
		return nil, nil
	})

	and := expr.And{Operands: []expr.Node{expr.Call{Name: "a"}, expr.Call{Name: "b"}}}
	_, err = reg.Resolve(and)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, expr.Node(and))

	clone := reg.Clone()
	clone.Register(named{name: "b"})
	clone.RegisterOperator(expr.Or{}, nil)

	_, ok = reg.Operator(expr.Or{})
	assert.Assert(t, !ok)
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
type support struct{ name string }

func (s support) Name() string { return s.name }

func TestRegistryTypeSupport(t *testing.T) {
	names := func(r *Registry) []string {
		var out []string
		for s := range r.TypeSupports() {
			out = append(out, s.Name())
		}

		return out
	}

	assert.Assert(t, slices.Contains(names(DefaultRegistry()), "ext-test-support"))

	reg := NewRegistry()
	reg.RegisterTypeSupport(support{name: "a"})

	clone := reg.Clone()
	clone.RegisterTypeSupport(support{name: "b"})
	assert.DeepEqual(t, names(reg), []string{"a"})
	assert.DeepEqual(t, names(clone), []string{"a", "b"})

	DefaultRegistry().RegisterTypeSupport(support{name: "ext-test-clone"})
	assert.Assert(t, !slices.Contains(names(DefaultRegistry()), "ext-test-clone"))
}

func init() { genext.RegisterTypeSupport(support{name: "ext-test-support"}) }
