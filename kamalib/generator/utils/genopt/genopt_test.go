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

// addLeaf is a leaf whose entry adds to the one of a later source when it
// sets Extra alone.
type addLeaf struct {
	Base  string
	Extra string
}

func (o addLeaf) OverlayOn(lower addLeaf) (addLeaf, bool) {
	if o.Base != "" || lower.Extra != "" {
		return addLeaf{}, false
	}

	lower.Extra = o.Extra

	return lower, true
}

func TestOverlayTypesAdds(t *testing.T) {
	type typeOption = TypeOption[addLeaf, addLeaf]

	dir := Source{Kind: SourceDirective, Name: "x.go"}
	file := Source{Kind: SourcePackageFile, Name: "kama_x.yaml"}
	lower := map[string]typeOption{
		"Add":    {Source: dir, Group: addLeaf{Base: "g"}, Type: addLeaf{Base: "a"}},
		"Fields": {Source: dir, Type: addLeaf{Base: "f"}},
		"Base":   {Source: dir, Type: addLeaf{Base: "b"}},
		"Twice":  {Source: dir, Type: addLeaf{Base: "t", Extra: "dir"}},
	}
	upper := map[string]typeOption{
		"Add": {Source: file, Type: addLeaf{Extra: "e"}, Fields: opt.Some(map[string]addLeaf{})},
		"Fields": {
			Source: file,
			Type:   addLeaf{Extra: "e"},
			Fields: opt.Some(map[string]addLeaf{"F": {Base: "x"}}),
		},
		"Base":  {Source: file, Type: addLeaf{Base: "file"}},
		"Twice": {Source: file, Type: addLeaf{Extra: "file"}},
		"New":   {Source: file, Type: addLeaf{Extra: "e"}},
	}

	out, warnings := overlayTypes(lower, "m/a", upper)

	add := out["Add"]
	assert.DeepEqual(t, add.Source, dir)
	assert.Equal(t, add.Group, addLeaf{Base: "g"})
	assert.Equal(t, add.Type, addLeaf{Base: "a", Extra: "e"})
	assert.Assert(t, add.Fields.IsNone())

	for _, name := range []string{"Fields", "Base", "Twice", "New"} {
		assert.DeepEqual(t, out[name].Source, file)
		assert.Equal(t, out[name].Type, upper[name].Type, name)
	}

	assert.Equal(t, lower["Add"].Type.Extra, "")

	got := make([]string, len(warnings))
	for i, w := range warnings {
		assert.ErrorIs(t, w, ErrOverridden)
		got[i] = w.Error()
	}

	assert.DeepEqual(t, got, []string{
		"genopt: options overridden: type m/a.Base: kama_x.yaml overrides " + dir.String(),
		"genopt: options overridden: type m/a.Fields: kama_x.yaml overrides " + dir.String(),
		"genopt: options overridden: type m/a.Twice: kama_x.yaml overrides " + dir.String(),
	})
}
