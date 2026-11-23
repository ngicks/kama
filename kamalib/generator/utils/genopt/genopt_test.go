package genopt

import (
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

func TestOptionLookup(t *testing.T) {
	type typeOption = TypeOption[leaf, leaf]

	o := Option[leaf, leaf, leaf]{Packages: map[string]PackageOption[leaf, leaf, leaf]{
		"m/b": {Types: map[string]typeOption{
			"B": {Type: leaf{Value: "b"}},
		}},
		"m/a": {Types: map[string]typeOption{
			"Z": {Type: leaf{Value: "z"}},
			"A": {
				Type:   leaf{Value: "a"},
				Fields: opt.Some(map[string]leaf{"F": {Value: "f"}}),
			},
		}},
	}}
	a := typegraph.NewQualifiedType("m/a", "A")

	to, ok := o.Type(a)
	assert.Assert(t, ok)
	assert.Equal(t, to.Type.Value, "a")

	_, ok = o.Type(typegraph.NewQualifiedType("m/c", "A"))
	assert.Assert(t, !ok)

	assert.Equal(t, o.Field(a, "F").Value, "f")
	assert.Equal(t, o.Field(a, "G").Value, "")
	assert.Equal(t, o.Field(typegraph.NewQualifiedType("m/b", "B"), "F").Value, "")

	var got []string
	for id, to := range o.Types() {
		got = append(got, id.String()+"="+to.Type.Value)
	}

	assert.DeepEqual(t, got, []string{"m/a.A=a", "m/a.Z=z", "m/b.B=b"})

	// Breaking out of the loop stops the iteration without a panic.
	for range o.Types() {
		break
	}
}
