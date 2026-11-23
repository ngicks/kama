package rule

import (
	"testing"

	"gotest.tools/v3/assert"
)

// named is Required registered under another name.
type named struct {
	Required
	name string
}

func (n named) Name() string { return n.name }

func TestRegistry(t *testing.T) {
	assert.DeepEqual(t, DefaultRegistry().Names(), []string{
		"cmp", "const", "format", "length", "nil", "null", "regexp", "required", "zero",
	})

	reg := NewRegistry(Required{}, named{name: "a"})
	assert.DeepEqual(t, reg.Names(), []string{"a", "required"})

	v, ok := reg.Lookup("a")
	assert.Assert(t, ok)
	assert.Equal(t, v, Validator(named{name: "a"}))

	_, ok = reg.Lookup("b")
	assert.Assert(t, !ok)

	clone := reg.Clone()
	clone.Register(named{name: "b"})
	assert.DeepEqual(t, clone.Names(), []string{"a", "b", "required"})
	assert.DeepEqual(t, reg.Names(), []string{"a", "required"})

	// A later validator of a name replaces the earlier one.
	reg.Register(named{name: "required"})
	v, _ = reg.Lookup("required")
	assert.Equal(t, v, Validator(named{name: "required"}))
}
