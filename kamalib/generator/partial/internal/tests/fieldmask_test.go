package tests

import (
	"slices"
	"testing"

	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/partial/internal/fixture/model"
)

func fullMasked() model.Masked {
	return model.Masked{
		Name:    "n",
		Count:   1,
		On:      true,
		Ptr:     new("p"),
		Renamed: "r",
		Inner: model.MaskInner{
			A: "a", B: new(2), Deep: model.MaskDeep{X: 3, Y: []string{"y"}},
		},
		InnerPtr:  &model.MaskInner{A: "pa", Deep: model.MaskDeep{X: 4}},
		Plain:     model.MaskPlain{P: "p"},
		E:         "e",
		EDeep:     model.MaskDeep{X: 5},
		MaskAudit: &model.MaskAudit{Note: "note"},
		Items:     []model.MaskInner{{A: "i"}},
		ByKey:     map[string]model.MaskInner{"k": {A: "k"}},
		Box:       model.MaskBox[int]{V: 6, Ptr: new(7)},
	}
}

func masked(t *testing.T, v model.Masked, paths ...string) model.MaskedPartial {
	t.Helper()

	var p model.MaskedPartial
	assert.NilError(t, p.FromValueWithMask(v, paths))

	return p
}

func TestFieldMask(t *testing.T) {
	var zero model.MaskedPartial
	assert.Assert(t, zero.FieldMask() == nil)

	var full model.MaskedPartial
	full.FromValue(fullMasked())
	got := full.FieldMask()
	assert.Assert(t, slices.IsSorted(got), "%v", got)
	assert.DeepEqual(t, got, slices.Sorted(slices.Values([]string{
		"Name", "Count", "On", "Ptr", "alias",
		"Inner.A", "Inner.B", "Inner.Deep.X", "Inner.Deep.Y",
		// A nil pointer and a nil slice are null, which sets the field.
		"InnerPtr.A", "InnerPtr.B", "InnerPtr.Deep.X", "InnerPtr.Deep.Y",
		"Plain",
		"E", "e_deep.X", "e_deep.Y",
		"Note",
		"Items", "ByKey",
		"Box.V", "Box.Ptr",
	})))

	cases := []struct {
		name string
		p    model.MaskedPartial
		want []string
	}{
		{
			name: "zero values are defined",
			p: model.MaskedPartial{
				Name: und.Defined(""), Count: und.Defined(0), On: und.Defined(false),
			},
			want: []string{"Count", "Name", "On"},
		},
		{
			name: "null",
			p:    model.MaskedPartial{Ptr: und.Null[string](), Renamed: und.Null[string]()},
			want: []string{"Ptr", "alias"},
		},
		{
			name: "null branch",
			p: model.MaskedPartial{
				Inner:    und.Null[model.MaskInnerPartial](),
				InnerPtr: und.Null[model.MaskInnerPartial](),
			},
			want: []string{"Inner", "InnerPtr"},
		},
		{
			name: "defined branch setting nothing",
			p:    model.MaskedPartial{Inner: und.Defined(model.MaskInnerPartial{})},
		},
		{
			name: "nested branch",
			p: model.MaskedPartial{Inner: und.Defined(model.MaskInnerPartial{
				A:    und.Defined("a"),
				Deep: und.Defined(model.MaskDeepPartial{X: und.Null[int]()}),
			})},
			want: []string{"Inner.A", "Inner.Deep.X"},
		},
		{
			name: "target without field masks is a leaf",
			p:    model.MaskedPartial{Plain: und.Defined(model.MaskPlainPartial{})},
			want: []string{"Plain"},
		},
		{
			name: "embedded",
			p: model.MaskedPartial{
				MaskEmbeddedPartial: model.MaskEmbeddedPartial{
					EDeep: und.Defined(model.MaskDeepPartial{Y: und.Defined([]string{})}),
				},
				MaskAuditPartial: model.MaskAuditPartial{Note: und.Defined("")},
			},
			want: []string{"Note", "e_deep.Y"},
		},
		{
			name: "collections are leaves",
			p: model.MaskedPartial{
				Items: und.Defined([]model.MaskInner{{A: "a"}}),
				ByKey: und.Null[map[string]model.MaskInner](),
			},
			want: []string{"ByKey", "Items"},
		},
		{
			name: "generic branch",
			p: model.MaskedPartial{
				Box: und.Defined(model.MaskBoxPartial[int]{Ptr: und.Null[int]()}),
			},
			want: []string{"Box.Ptr"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := c.p
			assert.DeepEqual(t, c.p.FieldMask(), c.want)
			equal(t, c.p, before)
		})
	}

	// A defined nil pointer of a field closing a cycle converts back to nil
	// as null does.
	tree := model.MaskTreePartial{Child: und.Defined[*model.MaskTreePartial](nil)}
	assert.DeepEqual(t, tree.FieldMask(), []string{"Child"})
	tree = model.MaskTreePartial{Child: und.Defined(&model.MaskTreePartial{
		Child: und.Defined(&model.MaskTreePartial{Name: und.Defined("leaf")}),
	})}
	assert.DeepEqual(t, tree.FieldMask(), []string{"Child.Child.Name"})
}

func TestFromValueWithMask(t *testing.T) {
	v := fullMasked()

	var whole model.MaskedPartial
	whole.FromValue(v)

	t.Run("empty", func(t *testing.T) {
		equal(t, masked(t, v), model.MaskedPartial{})
		equal(t, masked(t, v, []string{}...), model.MaskedPartial{})
	})
	t.Run("leaves", func(t *testing.T) {
		equal(t, masked(t, v, "Name", "Ptr", "alias", "Plain", "Items", "Name"),
			model.MaskedPartial{
				Name: whole.Name, Ptr: whole.Ptr, Renamed: whole.Renamed, Plain: whole.Plain,
				Items: whole.Items,
			})
		equal(t, masked(t, model.Masked{}, "Count", "On", "Ptr", "Items", "ByKey"),
			model.MaskedPartial{
				Count: und.Defined(0), On: und.Defined(false), Ptr: und.Null[string](),
				Items: und.Null[[]model.MaskInner](), ByKey: und.Null[map[string]model.MaskInner](),
			})
	})
	t.Run("branch as a whole", func(t *testing.T) {
		equal(t, masked(t, v, "Inner", "InnerPtr", "Box"), model.MaskedPartial{
			Inner: whole.Inner, InnerPtr: whole.InnerPtr, Box: whole.Box,
		})
		equal(t, masked(t, model.Masked{}, "InnerPtr"), model.MaskedPartial{
			InnerPtr: und.Null[model.MaskInnerPartial](),
		})
	})
	t.Run("child paths", func(t *testing.T) {
		equal(t, masked(t, v, "Inner.A", "Inner.Deep.X", "InnerPtr.B", "Box.V"),
			model.MaskedPartial{
				Inner: und.Defined(model.MaskInnerPartial{
					A:    und.Defined("a"),
					Deep: und.Defined(model.MaskDeepPartial{X: und.Defined(3)}),
				}),
				InnerPtr: und.Defined(model.MaskInnerPartial{B: und.Null[int]()}),
				Box:      und.Defined(model.MaskBoxPartial[int]{V: und.Defined(6)}),
			})
	})
	t.Run("parent dominates child", func(t *testing.T) {
		want := model.MaskedPartial{Inner: whole.Inner}
		equal(t, masked(t, v, "Inner.A", "Inner"), want)
		equal(t, masked(t, v, "Inner", "Inner.A"), want)
		equal(t, masked(t, v, "Inner.Deep", "Inner.Deep.X"), model.MaskedPartial{
			Inner: und.Defined(model.MaskInnerPartial{
				Deep: und.Defined(model.MaskDeepPartial{
					X: und.Defined(3), Y: und.Defined([]string{"y"}),
				}),
			}),
		})
	})
	t.Run("nil pointer branch", func(t *testing.T) {
		equal(t, masked(t, model.Masked{}, "InnerPtr.A", "InnerPtr.Deep.X"), model.MaskedPartial{
			InnerPtr: und.Null[model.MaskInnerPartial](),
		})
	})
	t.Run("nil pointer branch whole and child paths", func(t *testing.T) {
		want := model.MaskedPartial{InnerPtr: und.Null[model.MaskInnerPartial]()}
		equal(t, masked(t, model.Masked{}, "InnerPtr", "InnerPtr.A"), want)
		equal(t, masked(t, model.Masked{}, "InnerPtr.Deep.X", "InnerPtr"), want)

		var p model.MaskTreePartial

		paths := []string{"Child.Name", "Child", "Child.Child.Name"}
		assert.NilError(t, p.FromValueWithMask(model.MaskTree{Name: "a"}, paths))
		equal(t, p, model.MaskTreePartial{Child: und.Null[*model.MaskTreePartial]()})
	})
	t.Run("embedded", func(t *testing.T) {
		equal(t, masked(t, v, "E", "e_deep.X", "Note"), model.MaskedPartial{
			E:     und.Defined("e"),
			EDeep: und.Defined(model.MaskDeepPartial{X: und.Defined(5)}),
			Note:  und.Defined("note"),
		})
		// FromValue gives the zero embedded partial value for a nil pointer.
		equal(t, masked(t, model.Masked{}, "Note"), model.MaskedPartial{})
	})
	t.Run("generic target", func(t *testing.T) {
		var p model.MaskPairPartial[string, int]

		err := p.FromValueWithMask(model.MaskPair[string, int]{
			Key: "k", Box: model.MaskBox[int]{V: 1}, Map: map[string]int{"a": 1},
		}, []string{"Box.Ptr", "Map"})
		assert.NilError(t, err)
		equal(t, p, model.MaskPairPartial[string, int]{
			Box: und.Defined(model.MaskBoxPartial[int]{Ptr: und.Null[int]()}),
			Map: und.Defined(map[string]int{"a": 1}),
		})
	})
	t.Run("cycle", func(t *testing.T) {
		var p model.MaskTreePartial

		tree := model.MaskTree{Name: "a", Child: &model.MaskTree{Name: "b"}}
		assert.NilError(t, p.FromValueWithMask(tree, []string{"Child.Name", "Child.Child.Name"}))
		equal(t, p, model.MaskTreePartial{Child: und.Defined(&model.MaskTreePartial{
			Name: und.Defined("b"), Child: und.Null[*model.MaskTreePartial](),
		})})
	})
}

func TestFromValueWithMaskErrors(t *testing.T) {
	cases := []struct {
		name  string
		v     model.Masked
		paths []string
		want  string
	}{
		{name: "empty path", paths: []string{"Name", ""}, want: "empty path"},
		{name: "leading dot", paths: []string{".Name"}, want: ".Name: empty segment"},
		{name: "trailing dot", paths: []string{"Inner."}, want: "Inner.: empty segment"},
		{name: "double dot", paths: []string{"Inner..A"}, want: "Inner..A: empty segment"},
		{name: "unknown", paths: []string{"Nope"}, want: "Nope: unknown field Nope"},
		{
			name: "unknown child", paths: []string{"Inner.Deep.Nope"},
			want: "Inner.Deep.Nope: unknown field Nope",
		},
		{
			name: "child of a leaf", paths: []string{"Name.X"},
			want: "Name.X: field Name has no subfields",
		},
		{
			name: "child of a target without field masks", paths: []string{"Plain.P"},
			want: "Plain.P: field Plain has no subfields",
		},
		{
			name: "child of a collection", paths: []string{"Items.A"},
			want: "Items.A: field Items has no subfields",
		},
		{
			name: "field renamed", paths: []string{"Renamed"},
			want: "Renamed: unknown field Renamed",
		},
		{
			name: "embedded field name", paths: []string{"MaskEmbedded"},
			want: "MaskEmbedded: unknown field MaskEmbedded",
		},
		{
			name: "unknown in an embedded partial", paths: []string{"e_deep.Nope"},
			want: "e_deep.Nope: unknown field Nope",
		},
		{
			name: "child of a leaf of a nil embedded pointer", paths: []string{"Note.X"},
			want: "Note.X: field Note has no subfields",
		},
		{
			name: "unknown in a nil pointer branch", paths: []string{"InnerPtr.Nope"},
			want: "InnerPtr.Nope: unknown field Nope",
		},
		{
			name:  "invalid child of a nil pointer branch given whole",
			paths: []string{"InnerPtr", "InnerPtr.Nope"}, want: "InnerPtr.Nope: unknown field Nope",
		},
		{
			name: "invalid child after its parent", v: fullMasked(),
			paths: []string{"Inner", "Inner.Nope"}, want: "Inner.Nope: unknown field Nope",
		},
		{
			name: "invalid child before its parent", v: fullMasked(),
			paths: []string{"Inner.Nope", "Inner"}, want: "Inner.Nope: unknown field Nope",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var p model.MaskedPartial
			p.FromValue(fullMasked())
			before := p
			err := p.FromValueWithMask(c.v, c.paths)
			assert.Error(t, err, c.want)
			equal(t, p, before)
		})
	}
}

func TestFieldMaskRoundTrip(t *testing.T) {
	sorted := func(paths ...string) []string { return slices.Sorted(slices.Values(paths)) }
	leaves := []string{"Name", "Count", "On", "Ptr", "alias", "Plain", "Items", "ByKey"}
	// A branch given whole gives the paths of every field of its value. A
	// nil pointer branch gives null, which gives its own path.
	inner := []string{"Inner.A", "Inner.B", "Inner.Deep.X", "Inner.Deep.Y"}

	cases := []struct {
		name string
		mask []string
		// zero and full are the FieldMask of the partial value the mask
		// selects from the zero Masked and from fullMasked.
		zero, full []string
	}{
		{name: "nil"},
		{name: "leaves", mask: leaves, zero: sorted(leaves...), full: sorted(leaves...)},
		{
			name: "whole and child paths",
			mask: []string{"Inner", "InnerPtr.A", "Box.V"},
			zero: sorted(slices.Concat([]string{"InnerPtr", "Box.V"}, inner)...),
			full: sorted(slices.Concat([]string{"InnerPtr.A", "Box.V"}, inner)...),
		},
		{
			name: "embedded",
			mask: []string{"Inner.Deep", "Inner.Deep.Y", "E", "e_deep", "Note"},
			// A nil embedded pointer leaves Note undefined.
			zero: sorted("Inner.Deep.X", "Inner.Deep.Y", "E", "e_deep.X", "e_deep.Y"),
			full: sorted("Inner.Deep.X", "Inner.Deep.Y", "E", "e_deep.X", "e_deep.Y", "Note"),
		},
		{
			name: "pointer branch whole and child paths",
			mask: []string{"InnerPtr", "InnerPtr.Deep.X", "Box"},
			zero: sorted("InnerPtr", "Box.V", "Box.Ptr"),
			full: sorted(
				"InnerPtr.A",
				"InnerPtr.B",
				"InnerPtr.Deep.X",
				"InnerPtr.Deep.Y",
				"Box.V",
				"Box.Ptr",
			),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var p model.MaskedPartial
			assert.NilError(t, p.FromValueWithMask(model.Masked{}, c.mask))
			assert.DeepEqual(t, p.FieldMask(), c.zero)
			assert.NilError(t, p.FromValueWithMask(fullMasked(), c.mask))
			assert.DeepEqual(t, p.FieldMask(), c.full)
		})
	}

	// A mask of leaves of a value without nil pointers comes back as given.
	mask := []string{"Count", "Name", "Plain", "alias", "e_deep.X", "Inner.B", "Note"}

	var p model.MaskedPartial
	assert.NilError(t, p.FromValueWithMask(fullMasked(), mask))
	assert.DeepEqual(t, p.FieldMask(), slices.Sorted(slices.Values(mask)))
}

func TestFieldMaskApplyTo(t *testing.T) {
	dst := fullMasked()
	src := model.Masked{
		Name:      "changed",
		Inner:     model.MaskInner{A: "changed", B: new(20)},
		InnerPtr:  &model.MaskInner{Deep: model.MaskDeep{X: 40}},
		MaskAudit: &model.MaskAudit{Note: "changed"},
		Count:     100,
	}

	var p model.MaskedPartial
	assert.NilError(t, p.FromValueWithMask(src, []string{
		"Name", "Inner.A", "InnerPtr.Deep.X", "Note",
	}))

	want := fullMasked()
	want.Name = "changed"
	want.Inner.A = "changed"
	want.InnerPtr.Deep.X = 40
	want.Note = "changed"
	equal(t, p.ApplyTo(dst), want)
	equal(t, dst, fullMasked())
}

func TestFieldMaskMutualRecursion(t *testing.T) {
	v := model.MaskPing{
		Name: "a",
		Pong: &model.MaskPong{Count: 1, Ping: &model.MaskPing{Name: "b"}},
	}

	var p model.MaskPingPartial
	assert.NilError(t, p.FromValueWithMask(v, []string{"Pong.Ping.Name", "Pong.Count"}))
	equal(t, p, model.MaskPingPartial{Pong: und.Defined(&model.MaskPongPartial{
		Count: und.Defined(1),
		Ping:  und.Defined(&model.MaskPingPartial{Name: und.Defined("b")}),
	})})
	assert.DeepEqual(t, p.FieldMask(), []string{"Pong.Count", "Pong.Ping.Name"})

	assert.NilError(t, p.FromValueWithMask(v, []string{"Pong.Ping", "Pong.Ping.Pong.Count"}))
	equal(t, p, model.MaskPingPartial{Pong: und.Defined(&model.MaskPongPartial{
		Ping: und.Defined(&model.MaskPingPartial{
			Name: und.Defined("b"), Pong: und.Null[*model.MaskPongPartial](),
		}),
	})})
	assert.DeepEqual(t, p.FieldMask(), []string{"Pong.Ping.Name", "Pong.Ping.Pong"})

	assert.NilError(t, p.FromValueWithMask(model.MaskPing{}, []string{"Pong.Ping.Name", "Pong"}))
	equal(t, p, model.MaskPingPartial{Pong: und.Null[*model.MaskPongPartial]()})

	before := p
	err := p.FromValueWithMask(model.MaskPing{}, []string{"Pong", "Pong.Ping.Nope"})
	assert.Error(t, err, "Pong.Ping.Nope: unknown field Nope")
	equal(t, p, before)
}

func TestFieldMaskEmbeddedPointer(t *testing.T) {
	var p model.MaskHolderPartial
	assert.NilError(t, p.FromValueWithMask(
		model.MaskHolder{MaskEmbedded: &model.MaskEmbedded{E: "e", EDeep: model.MaskDeep{X: 1}}},
		[]string{"E", "e_deep.X"},
	))
	equal(t, p, model.MaskHolderPartial{
		E:     und.Defined("e"),
		EDeep: und.Defined(model.MaskDeepPartial{X: und.Defined(1)}),
	})
	assert.DeepEqual(t, p.FieldMask(), []string{"E", "e_deep.X"})

	// FromValue gives the zero embedded partial value for a nil pointer.
	assert.NilError(t, p.FromValueWithMask(model.MaskHolder{}, []string{"E", "e_deep.X"}))
	equal(t, p, model.MaskHolderPartial{})

	t.Run("unknown in a nil embedded pointer", func(t *testing.T) {
		p := model.MaskHolderPartial{E: und.Defined("kept")}
		before := p
		err := p.FromValueWithMask(model.MaskHolder{}, []string{"e_deep.Nope"})
		assert.Error(t, err, "e_deep.Nope: unknown field Nope")
		equal(t, p, before)
	})
}
