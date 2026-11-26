package fieldtree

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
)

const genericSrc = `package p

//kama:fieldtree open
type Pair[K comparable, V any] struct {
	Key   K ` + "`json:\"key\"`" + `
	Value []V ` + "`json:\"value\"`" + `
}

//kama:fieldtree closed
type Closed struct {
	Pair Pair[string, Item] ` + "`json:\"pair\"`" + `
}

type Item struct {
	Id int ` + "`json:\"id\"`" + ` // the id
}
`

func TestGenericTree(t *testing.T) {
	texts, warnings, err := gen(t, map[string]string{"p/p.go": genericSrc}, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)
	golden.Assert(t, texts["open"], "generic_open.golden")
	golden.Assert(t, texts["closed"], "generic_closed.golden")
}

const boxSrc = `package p

// Box holds a value of T when ok is set.
type Box[T any] struct {
	v  T
	ok bool
}

func (b Box[T]) Get() T  { return b.v }
func (b Box[T]) Ok() bool { return b.ok }

// Bag holds a value too, described without a fieldtree section.
type Bag[T any] struct{ v T }

//kama:fieldtree boxed
type Boxed struct {
	Box Box[Item] ` + "`json:\"box\"`" + `
	Bag Bag[int] ` + "`json:\"bag\"`" + `
}

type Item struct {
	Id int ` + "`json:\"id\"`" + `
}
`

// TestTextFileHolder describes Box by a text extension file holding a
// fieldtree section, and Bag by one without, which falls back to a line
// built from its role.
func TestTextFileHolder(t *testing.T) {
	pkg := modulePath + gentest.InlineDir(t) + "/p"
	holder := func(reach map[string]any) map[string]any {
		entry := map[string]any{
			"holder": map[string]any{
				"role": "t", "type": "T", "guard": "{{.V}}.ok", "value": "{{.V}}.v",
			},
		}
		if reach != nil {
			entry["generator"] = map[string]any{"fieldtree": reach}
		}

		return entry
	}

	supports, warnings, err := genext.LoadText([]genext.TextDocument{{
		Name: "ext.yaml",
		Data: map[string]any{
			"version": 1,
			"types": map[string]any{
				pkg + ".Box": holder(map[string]any{
					"reach": map[string]any{"t": map[string]any{
						"access": "{{.V}}.Get",
						"note":   "set only when {{.V}}.Ok",
						"color":  "red",
					}},
				}),
				pkg + ".Bag": holder(nil),
			},
		},
	}})
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 1)
	assert.ErrorContains(t, warnings[0], "reach.t.color")

	reg := genext.DefaultRegistry()
	reg.PrependTypeSupports(supports...)

	texts, warnings, err := genWith(t, map[string]string{"p/p.go": boxSrc}, nil, reg)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)
	golden.Assert(t, texts["boxed"], "textfile.golden")
}

func TestTextFileSectionErrors(t *testing.T) {
	_, _, err := genext.LoadText([]genext.TextDocument{{
		Name: "ext.yaml",
		Data: map[string]any{
			"version": 1,
			"types": map[string]any{"example.com/p.Box": map[string]any{
				"generator": map[string]any{"fieldtree": map[string]any{
					"reach": map[string]any{"t": map[string]any{"note": "{{"}},
				}},
			}},
		},
	}})
	assert.ErrorContains(t, err, "generator.fieldtree.reach.t.access: is required")
	assert.ErrorContains(t, err, "generator.fieldtree.reach.t.note")
}

const optionsSrc = `package p

//kama:fieldtree yaml, key="yaml", label="app config", color="red"
type Yaml struct {
	Name  string ` + "`yaml:\"name\" json:\"n\"`" + `
	Inner ` + "`yaml:\"inner\"`" + `
	Plain int
	Skip  int ` + "`yaml:\"-\"`" + `
}

type Inner struct {
	A int ` + "`yaml:\"a\"`" + `
}

//kama:fieldtree "dup"
type First struct{ A int }

//kama:fieldtree dup
type Second struct{ B int }
`

func TestDirectiveOptions(t *testing.T) {
	texts, warnings, err := gen(t, map[string]string{"p/p.go": optionsSrc}, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 2, "%v", warnings)
	assert.ErrorIs(t, warnings[0], ErrUnknownKey)
	assert.ErrorContains(t, warnings[0], "color")
	assert.ErrorIs(t, warnings[1], ErrDuplicateId)
	assert.ErrorContains(t, warnings[1], `"dup" is given at`)
	golden.Assert(t, texts["yaml"], "options_yaml.golden")
	assert.Equal(t, texts["dup"], "First\n└─ .A  int  #   (A)\n")
}
