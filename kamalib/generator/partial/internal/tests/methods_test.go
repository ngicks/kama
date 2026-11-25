package tests

import (
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/partial/internal/fixture/model"
	"github.com/ngicks/kama/kamalib/generator/partial/internal/fixture/pkgmark"
	"github.com/ngicks/kama/kamalib/generator/partial/internal/fixture/shared"
	mergemodel "github.com/ngicks/kama/kamalib/generator/partial/internal/fixturemerge/model"
)

// equal compares with reflect.DeepEqual, which looks into the unexported
// fields of the option types and keeps nil and empty apart.
func equal(t *testing.T, got, want any) {
	t.Helper()
	assert.Assert(t, reflect.DeepEqual(got, want), "got  %#v\nwant %#v", got, want)
}

type partial[V, P any] interface {
	ToValue() V
	Merge(P) P
	ApplyTo(V) V
}

// roundTrip checks ToValue gives back what FromValue took, and that
// applying the zero partial changes nothing. Applying the partial form of v
// to v gives v under overwrite strategies only, so it is not checked here.
func roundTrip[V any, P partial[V, P], PP interface {
	*P
	FromValue(V)
}](t *testing.T, v V) {
	t.Helper()

	var p P
	PP(&p).FromValue(v)
	equal(t, p.ToValue(), v)

	var zero P
	equal(t, zero.ApplyTo(v), v)
}

func fullUser() model.User {
	return model.User{
		ID:      "id",
		Audit:   &model.Audit{CreatedAt: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)},
		Status:  "active",
		Time:    time.Date(2021, 1, 2, 3, 4, 5, 0, time.UTC),
		Name:    "name",
		Age:     3,
		Nick:    new("nick"),
		Email:   "e@example.invalid",
		Skipped: "s",
		Address: model.Address{City: "c", Geo: &model.Geo{Lat: 1, Lng: 2}},
		Geo:     &model.Geo{Lat: 3},
		Tags:    []string{"a", "b"},
		Attrs:   map[string]int{"a": 1},
		Pos:     [2]float64{1, 2},
		Geos:    []model.Geo{{Lat: 4}},
		Items:   model.Items{{Name: "n", Count: new(1)}, {}},
		Index:   model.Index{"k": {Name: "i"}, "nil": nil},
		Box:     model.Box[int]{Value: 1, Values: []int{}, Ptr: new(2)},
		Pair:    model.Pair[string, model.Geo]{Key: "k", Value: model.Geo{Lng: 5}},
		Stat:    &model.Stat[float64]{Total: 1.5},
		Opt:     opt.Some(7),
		Und:     und.Null[string](),
		Elastic: elastic.FromValues(1, 2),
		Any:     1,
	}
}

func TestRoundTrip(t *testing.T) {
	roundTrip[model.User, model.UserPartial](t, model.User{})
	roundTrip[model.User, model.UserPartial](t, fullUser())
	roundTrip[model.Config, model.ConfigPartial](t, model.Config{
		Limits: model.Limits{Max: 1}, Level: 2, Meta: model.Meta{Note: "n"},
	})
	roundTrip[model.Items, model.ItemsPartial](t, nil)
	roundTrip[model.Items, model.ItemsPartial](t, model.Items{})
	roundTrip[model.Index, model.IndexPartial](t, nil)
	roundTrip[model.Index, model.IndexPartial](t, model.Index{})
	roundTrip[model.Box[string], model.BoxPartial[string]](
		t,
		model.Box[string]{Values: []string{"x"}},
	)
	roundTrip[model.Stat[int], model.StatPartial[int]](t, model.Stat[int]{Total: 1})
	roundTrip[model.Tree, model.TreePartial](t, model.Tree{
		Value: 1,
		Left:  &model.Tree{Value: 2, Left: &model.Tree{Value: 4}},
		Kids:  model.Forest{&model.Tree{Value: 3}, nil},
	})
	roundTrip[model.Ring, model.RingPartial](t, model.Ring{
		Head: model.Elem{Name: "a", Ring: &model.Ring{Head: model.Elem{Name: "b"}}},
	})
	roundTrip[model.Merging, model.MergingPartial](t, model.Merging{})
	roundTrip[model.Merging, model.MergingPartial](t, model.Merging{
		Tags:     []string{},
		Attrs:    map[string]int{},
		Plain:    []string{"p"},
		Dict:     map[string]int{"d": 1},
		Replaced: model.Address{City: "r"},
		Nested:   model.Address{Geo: &model.Geo{}},
		Items:    model.Items{},
		Index:    model.Index{},
		Ptr:      &[]int{},
		Flagged:  1,
	})
	roundTrip[model.Embeds, model.EmbedsPartial](t, model.Embeds{})
	roundTrip[model.Embeds, model.EmbedsPartial](t, model.Embeds{
		Labels: model.Labels{}, Base: &model.Base{},
	})
	roundTrip[pkgmark.Order, pkgmark.OrderPartial](t, pkgmark.Order{
		ID:       "o",
		Total:    shared.Money{Amount: 1, Currency: "JPY"},
		Lines:    pkgmark.Lines{{SKU: "s", Qty: 1}},
		Customer: new(fullUser()),
		Draft:    pkgmark.Draft{Note: "d"},
		Hidden:   shared.Public{Secret: "x"},
	})
	roundTrip[shared.Money, shared.MoneyPartial](t, shared.Money{Amount: 1})
	roundTrip[mergemodel.Doc, mergemodel.DocPartial](t, mergemodel.Doc{
		Tags:  []string{"t"},
		Attrs: map[string]string{"a": "b"},
		Lines: mergemodel.Lines{{A: 1}},
	})
}

func TestFromValueStates(t *testing.T) {
	var p model.UserPartial
	p.FromValue(model.User{})
	// A nil pointer, slice or map and a none option become null.
	assert.Assert(t, p.Nick.IsNull())
	assert.Assert(t, p.Geo.IsNull())
	assert.Assert(t, p.Stat.IsNull())
	assert.Assert(t, p.Tags.IsNull())
	assert.Assert(t, p.Attrs.IsNull())
	assert.Assert(t, p.Items.IsNull())
	assert.Assert(t, p.Index.IsNull())
	assert.Assert(t, p.Opt.IsNull())
	// A nil embedded pointer gives a zero partial.
	equal(t, p.AuditPartial, model.AuditPartial{})
	// Other values become defined, and option types are copied.
	equal(t, p.Name, und.Defined(""))
	equal(t, p.Pos, und.Defined([2]float64{}))
	assert.Assert(t, p.Und.IsUndefined())
	assert.Assert(t, p.Elastic.IsUndefined())

	p.FromValue(model.User{Tags: []string{}, Opt: opt.Some(0), Items: model.Items{}})
	equal(t, p.Tags, und.Defined([]string{}))
	equal(t, p.Opt, und.Defined(0))
	equal(t, p.Items, und.Defined(model.ItemsPartial{}))

	var r model.TreePartial
	r.FromValue(model.Tree{Left: &model.Tree{Value: 1}})
	assert.Assert(t, r.Right.IsNull())
	equal(t, r.Left.Value().Value, und.Defined(1))
}

func TestToValueStates(t *testing.T) {
	v := model.UserPartial{
		Name:    und.Null[string](),
		Nick:    und.Null[string](),
		Opt:     und.Null[int](),
		Geo:     und.Null[model.GeoPartial](),
		Address: und.Null[model.AddressPartial](),
		Items:   und.Defined(model.ItemsPartial{und.Null[model.ItemPartial]()}),
		Index:   und.Defined(model.IndexPartial{"u": und.Undefined[model.ItemPartial]()}),
	}.ToValue()
	equal(t, v.Name, "")
	assert.Assert(t, v.Nick == nil)
	assert.Assert(t, v.Opt.IsNone())
	assert.Assert(t, v.Geo == nil)
	equal(t, v.Address, model.Address{})
	equal(t, v.Items, model.Items{{}})
	equal(t, v.Index, model.Index{"u": nil})
	assert.Assert(t, v.Audit == nil)
	assert.Assert(t, v.Tags == nil)

	v = model.UserPartial{
		CreatedAt: und.Defined(time.Time{}),
		Opt:       und.Defined(0),
	}.ToValue()
	equal(t, v.Audit, &model.Audit{})
	equal(t, v.Opt, opt.Some(0))
}

func TestOptionsOnTypeParameter(t *testing.T) {
	var p model.OptionsPartial[string]
	p.FromValue(model.Options[string]{})
	assert.Assert(t, p.Opt.IsNull())
	assert.Assert(t, p.Und.IsUndefined())
	assert.Assert(t, p.Elastic.IsUndefined())

	v := model.Options[string]{
		Opt:     opt.Some("o"),
		Und:     und.Null[string](),
		Elastic: elastic.FromOptions(opt.Some("e"), opt.None[string]()),
	}
	p.FromValue(v)
	equal(t, p.Opt, und.Defined("o"))
	equal(t, p.ToValue(), v)

	cleared := model.OptionsPartial[string]{
		Opt:     und.Null[string](),
		Und:     und.Defined("u"),
		Elastic: elastic.Null[string](),
	}
	equal(t, cleared.ApplyTo(v), model.Options[string]{
		Und:     und.Defined("u"),
		Elastic: elastic.Null[string](),
	})
	equal(t, model.OptionsPartial[string]{}.ApplyTo(v), v)
}

// states are an undefined, a null and a defined value.
func states[T any](v T) map[string]und.Und[T] {
	return map[string]und.Und[T]{
		"undefined": und.Undefined[T](),
		"null":      und.Null[T](),
		"defined":   und.Defined(v),
	}
}

func TestMergeOverwrite(t *testing.T) {
	for _, l := range states(1) {
		for rn, r := range states(2) {
			got := model.MergingPartial{Flagged: l}.Merge(model.MergingPartial{Flagged: r})

			want := r
			if rn == "undefined" {
				want = l
			}

			equal(t, got.Flagged, want)
		}
	}

	for _, r := range states([]string{"r"}) {
		l := und.Defined([]string{"l"})

		got := model.MergingPartial{Plain: l}.Merge(model.MergingPartial{Plain: r})
		if r.IsUndefined() {
			equal(t, got.Plain, l)
		} else {
			equal(t, got.Plain, r)
		}
	}

	for _, r := range states(elastic.FromValue(2)) {
		l := elastic.FromValue(1)

		var re elastic.Elastic[int]

		switch {
		case r.IsNull():
			re = elastic.Null[int]()
		case r.IsDefined():
			re = r.Value()
		}

		got := model.UserPartial{Elastic: l}.Merge(model.UserPartial{Elastic: re})
		if r.IsUndefined() {
			equal(t, got.Elastic, l)
		} else {
			equal(t, got.Elastic, re)
		}
	}
}

func TestMergeAppend(t *testing.T) {
	for ln, l := range states([]string{"l"}) {
		for rn, r := range states([]string{"r"}) {
			got := model.MergingPartial{Tags: l}.Merge(model.MergingPartial{Tags: r}).Tags

			switch {
			case rn == "undefined":
				equal(t, got, l)
			case ln == "defined" && rn == "defined":
				equal(t, got, und.Defined([]string{"l", "r"}))
			default:
				equal(t, got, r)
			}
		}
	}

	for ln, l := range states(map[string]int{"a": 1, "b": 1}) {
		for rn, r := range states(map[string]int{"b": 2, "c": 3}) {
			got := model.MergingPartial{Attrs: l}.Merge(model.MergingPartial{Attrs: r}).Attrs

			switch {
			case rn == "undefined":
				equal(t, got, l)
			case ln == "defined" && rn == "defined":
				equal(t, got, und.Defined(map[string]int{"a": 1, "b": 2, "c": 3}))
			default:
				equal(t, got, r)
			}
		}
	}

	item := func(name string) und.Und[model.ItemPartial] {
		return und.Defined(model.ItemPartial{Name: und.Defined(name)})
	}
	got := model.MergingPartial{Items: und.Defined(model.ItemsPartial{item("l")})}.
		Merge(model.MergingPartial{Items: und.Defined(model.ItemsPartial{item("r")})})
	equal(t, got.Items, und.Defined(model.ItemsPartial{item("l"), item("r")}))

	gotPtr := model.MergingPartial{Ptr: und.Defined([]int{1})}.
		Merge(model.MergingPartial{Ptr: und.Defined([]int{2})})
	equal(t, gotPtr.Ptr, und.Defined([]int{1, 2}))

	// Two empty slices stay a defined empty slice.
	gotEmpty := model.MergingPartial{Tags: und.Defined([]string{})}.
		Merge(model.MergingPartial{Tags: und.Defined([]string{})})
	equal(t, gotEmpty.Tags, und.Defined([]string{}))
}

// TestMergeAppendFresh checks a merge of two defined sides shares storage
// with neither side, even when one of them is nil or empty.
func TestMergeAppendFresh(t *testing.T) {
	for _, c := range []struct{ l, r []string }{
		{nil, []string{"r"}},
		{[]string{"l"}, nil},
		{[]string{}, []string{"r"}},
		{[]string{"l"}, []string{}},
	} {
		l, r := slices.Clone(c.l), slices.Clone(c.r)
		got := model.MergingPartial{Tags: und.Defined(l)}.
			Merge(model.MergingPartial{Tags: und.Defined(r)}).Tags.Value()
		got[0] = "x"

		equal(t, l, c.l)
		equal(t, r, c.r)
	}

	for _, c := range []struct{ l, r map[string]int }{
		{nil, map[string]int{"r": 1}},
		{map[string]int{"l": 1}, nil},
		{map[string]int{}, map[string]int{"r": 1}},
		{map[string]int{"l": 1}, map[string]int{}},
	} {
		l, r := maps.Clone(c.l), maps.Clone(c.r)
		got := model.MergingPartial{Attrs: und.Defined(l)}.
			Merge(model.MergingPartial{Attrs: und.Defined(r)}).Attrs.Value()
		got["x"] = 1

		equal(t, l, c.l)
		equal(t, r, c.r)
	}

	got := model.MergingPartial{Tags: und.Defined([]string(nil))}.
		Merge(model.MergingPartial{Tags: und.Defined([]string(nil))})
	equal(t, got.Tags, und.Defined([]string(nil)))
}

func TestMergeNested(t *testing.T) {
	l := model.AddressPartial{City: und.Defined("l")}
	r := model.AddressPartial{Geo: und.Defined(model.GeoPartial{Lat: und.Defined(1.0)})}
	merged := model.AddressPartial{City: l.City, Geo: r.Geo}

	for ln, lu := range states(l) {
		for rn, ru := range states(r) {
			got := model.MergingPartial{Nested: lu, Replaced: lu}.
				Merge(model.MergingPartial{Nested: ru, Replaced: ru})

			switch {
			case rn == "undefined":
				equal(t, got.Nested, lu)
				equal(t, got.Replaced, lu)
			case ln == "defined" && rn == "defined":
				equal(t, got.Nested, und.Defined(merged))
				equal(t, got.Replaced, ru)
			default:
				equal(t, got.Nested, ru)
				equal(t, got.Replaced, ru)
			}
		}
	}

	// The map partial merges by its own Merge, which overwrites.
	idx := func(k string) und.Und[model.IndexPartial] {
		return und.Defined(model.IndexPartial{k: und.Defined(model.ItemPartial{})})
	}
	got := model.MergingPartial{Index: idx("l")}.Merge(model.MergingPartial{Index: idx("r")})
	equal(t, got.Index, idx("r"))
	got = model.MergingPartial{Index: idx("l")}.
		Merge(model.MergingPartial{Index: und.Defined[model.IndexPartial](nil)})
	equal(t, got.Index, idx("l"))

	// Embedded partials have no undefined state and merge field by field.
	gotUser := model.UserPartial{ID: und.Defined("l")}.
		Merge(model.UserPartial{})
	equal(t, gotUser.ID, und.Defined("l"))
	gotUser = model.UserPartial{ID: und.Defined("l")}.
		Merge(model.UserPartial{ID: und.Null[string]()})
	assert.Assert(t, gotUser.ID.IsNull())
}

func TestMergeRefs(t *testing.T) {
	l := model.TreePartial{Left: und.Defined(&model.TreePartial{Value: und.Defined(1)})}
	r := model.TreePartial{Left: und.Defined(&model.TreePartial{
		Right: und.Defined(&model.TreePartial{Value: und.Defined(2)}),
	})}
	got := l.Merge(r)
	equal(t, got.Left.Value().Value, und.Defined(1))
	equal(t, got.Left.Value().Right.Value().Value, und.Defined(2))
	// The merge is a new value.
	assert.Assert(t, got.Left.Value() != l.Left.Value())
	assert.Assert(t, l.Left.Value().Right.IsUndefined())

	got = l.Merge(model.TreePartial{Left: und.Null[*model.TreePartial]()})
	assert.Assert(t, got.Left.IsNull())
	equal(t, got.ToValue().Left, (*model.Tree)(nil))
}

func TestMergeDoesNotWriteInputs(t *testing.T) {
	tags := make([]string, 1, 10)
	tags[0] = "l"
	attrs := map[string]int{"a": 1}
	items := make(model.ItemsPartial, 0, 10)
	l := model.MergingPartial{
		Tags:  und.Defined(tags),
		Attrs: und.Defined(attrs),
		Items: und.Defined(items),
	}
	r := model.MergingPartial{
		Tags:  und.Defined([]string{"r"}),
		Attrs: und.Defined(map[string]int{"b": 2}),
		Items: und.Defined(model.ItemsPartial{und.Null[model.ItemPartial]()}),
	}
	got := l.Merge(r)

	equal(t, got.Tags, und.Defined([]string{"l", "r"}))
	// Appending did not write to the spare capacity of the left slice.
	equal(t, tags[:2], []string{"l", ""})
	equal(t, items[:1], model.ItemsPartial{{}})
	equal(t, attrs, map[string]int{"a": 1})
	equal(t, r.Attrs.Value(), map[string]int{"b": 2})

	got.Tags.Value()[0] = "changed"

	equal(t, tags[0], "l")

	lines := make(mergemodel.LinesPartial, 1, 10)
	gotLines := lines.Merge(mergemodel.LinesPartial{und.Null[mergemodel.InnerPartial]()})
	equal(t, len(gotLines), 2)
	equal(t, lines[:2], mergemodel.LinesPartial{{}, {}})
}

func TestApplyTo(t *testing.T) {
	u := fullUser()
	p := model.UserPartial{
		ID:      und.Defined("new"),
		Name:    und.Defined("renamed"),
		Nick:    und.Null[string](),
		Opt:     und.Null[int](),
		Address: und.Defined(model.AddressPartial{City: und.Defined("c2")}),
		Tags:    und.Defined([]string{"x"}),
		Stat:    und.Null[model.StatPartial[float64]](),
	}
	got := p.ApplyTo(u)

	want := fullUser()
	want.ID = "new"
	want.Name = "renamed"
	want.Nick = nil
	want.Opt = opt.None[int]()
	want.Address.City = "c2"
	want.Tags = []string{"x"}
	want.Stat = nil
	equal(t, got, want)
	// ApplyTo does not write to its argument.
	equal(t, u, fullUser())

	var same model.UserPartial
	same.FromValue(u)
	equal(t, same.ApplyTo(model.User{}), u)

	var e model.EmbedsPartial
	equal(t, e.ApplyTo(model.Embeds{}), model.Embeds{})
	e.ID = und.Defined("b")
	equal(t, e.ApplyTo(model.Embeds{}), model.Embeds{Base: &model.Base{ID: "b"}})
}

func TestMergeOptions(t *testing.T) {
	inner := func(a, b int) und.Und[mergemodel.InnerPartial] {
		p := mergemodel.InnerPartial{}
		if a != 0 {
			p.A = und.Defined(a)
		}

		if b != 0 {
			p.B = und.Defined(b)
		}

		return und.Defined(p)
	}
	l := mergemodel.DocPartial{
		Tags:   und.Defined([]string{"l"}),
		Attrs:  und.Defined(map[string]string{"a": "l"}),
		Inner:  inner(1, 0),
		Merged: inner(1, 0),
		Kept:   und.Defined([]string{"l"}),
		Lines:  und.Defined(mergemodel.LinesPartial{inner(1, 0)}),
	}
	r := mergemodel.DocPartial{
		Tags:   und.Defined([]string{"r"}),
		Attrs:  und.Defined(map[string]string{"b": "r"}),
		Inner:  inner(0, 2),
		Merged: inner(0, 2),
		Kept:   und.Defined([]string{"r"}),
		Lines:  und.Defined(mergemodel.LinesPartial{inner(0, 2)}),
	}
	got := l.Merge(r)

	// Merge is append for slices and maps.
	equal(t, got.Tags, und.Defined([]string{"l", "r"}))
	equal(t, got.Attrs, und.Defined(map[string]string{"a": "l", "b": "r"}))
	// MergeNested is overwrite, unless a directive says otherwise.
	equal(t, got.Inner, inner(0, 2))
	equal(t, got.Merged, inner(1, 2))
	equal(t, got.Kept, und.Defined([]string{"r"}))
	equal(t, got.Lines, r.Lines)

	// The slice partial's own Merge appends.
	equal(t, l.Lines.Value().Merge(r.Lines.Value()),
		mergemodel.LinesPartial{inner(1, 0), inner(0, 2)})
	equal(t, mergemodel.LinesPartial(nil).Merge(nil), mergemodel.LinesPartial(nil))
}

func TestEmbeddedPointerWithoutDefinedField(t *testing.T) {
	// FromValue copies the undefined und.Und of Hollow, so its partial has
	// no defined field and ToValue cannot tell it from a nil pointer.
	var p mergemodel.DocPartial
	p.FromValue(mergemodel.Doc{Hollow: &mergemodel.Hollow{}})
	equal(t, p.HollowPartial, mergemodel.HollowPartial{})
	assert.Assert(t, p.ToValue().Hollow == nil)

	for _, h := range []mergemodel.Hollow{{Note: und.Defined("n")}, {Note: und.Null[string]()}} {
		roundTrip[mergemodel.Doc, mergemodel.DocPartial](t, mergemodel.Doc{Hollow: &h})
	}
}

func TestMergeEmbeddedOverwrite(t *testing.T) {
	l := mergemodel.DocPartial{
		ID:   und.Defined("l"),
		Rev:  und.Defined(1),
		Note: und.Defined("l"),
	}
	// An embedded partial with every field undefined counts as undefined.
	equal(t, l.Merge(mergemodel.DocPartial{}), l)
	equal(t, l.Merge(mergemodel.DocPartial{Name: und.Defined("n")}).BasePartial, l.BasePartial)

	// Any defined or null field makes the embedded partial of r replace that
	// of p whole.
	got := l.Merge(mergemodel.DocPartial{ID: und.Defined("r")})
	equal(t, got.BasePartial, mergemodel.BasePartial{ID: und.Defined("r")})
	equal(t, got.HollowPartial, l.HollowPartial)

	got = l.Merge(mergemodel.DocPartial{Rev: und.Null[int](), Note: und.Null[string]()})
	equal(t, got.BasePartial, mergemodel.BasePartial{Rev: und.Null[int]()})
	equal(t, got.HollowPartial, mergemodel.HollowPartial{Note: und.Null[string]()})

	// ApplyTo of the zero partial keeps the embedded values.
	v := mergemodel.Doc{
		ID: "i", Rev: 2,
		Hollow: &mergemodel.Hollow{Note: und.Defined("n")},
	}
	equal(t, mergemodel.DocPartial{}.ApplyTo(v), v)
}

func TestMergeAppendMapPartial(t *testing.T) {
	inner := func(a int) und.Und[mergemodel.InnerPartial] {
		return und.Defined(mergemodel.InnerPartial{A: und.Defined(a)})
	}
	l := mergemodel.IndexPartial{"a": inner(1), "b": inner(1)}
	r := mergemodel.IndexPartial{"b": inner(2), "c": inner(3)}
	want := mergemodel.IndexPartial{"a": inner(1), "b": inner(2), "c": inner(3)}

	// The map partial's own Merge sets the entries of r without merging
	// them one by one.
	equal(t, l.Merge(r), want)
	equal(t, l.Merge(nil), l)
	equal(t, mergemodel.IndexPartial(nil).Merge(r), r)
	equal(t, mergemodel.IndexPartial(nil).Merge(nil), mergemodel.IndexPartial(nil))
	equal(t, l, mergemodel.IndexPartial{"a": inner(1), "b": inner(1)})
	equal(t, r, mergemodel.IndexPartial{"b": inner(2), "c": inner(3)})

	// The field appends the defined map partials.
	got := mergemodel.DocPartial{Index: und.Defined(l)}.
		Merge(mergemodel.DocPartial{Index: und.Defined(r)})
	equal(t, got.Index, und.Defined(want))
	got = mergemodel.DocPartial{Index: und.Defined(l)}.
		Merge(mergemodel.DocPartial{Index: und.Null[mergemodel.IndexPartial]()})
	assert.Assert(t, got.Index.IsNull())
}

func TestMergeAppendOption(t *testing.T) {
	for ln, l := range states([]string{"l"}) {
		for rn, r := range states([]string{"r"}) {
			got := mergemodel.DocPartial{OptTags: l}.
				Merge(mergemodel.DocPartial{OptTags: r}).OptTags

			switch {
			case rn == "undefined":
				equal(t, got, l)
			case ln == "defined" && rn == "defined":
				equal(t, got, und.Defined([]string{"l", "r"}))
			default:
				equal(t, got, r)
			}
		}
	}

	for ln, l := range states(map[string]string{"a": "l", "b": "l"}) {
		for rn, r := range states(map[string]string{"b": "r", "c": "r"}) {
			got := mergemodel.DocPartial{OptAttrs: l}.
				Merge(mergemodel.DocPartial{OptAttrs: r}).OptAttrs

			switch {
			case rn == "undefined":
				equal(t, got, l)
			case ln == "defined" && rn == "defined":
				equal(t, got, und.Defined(map[string]string{"a": "l", "b": "r", "c": "r"}))
			default:
				equal(t, got, r)
			}
		}
	}

	got := mergemodel.DocPartial{OptTags: und.Defined([]string{"l"})}.ApplyTo(
		mergemodel.Doc{OptTags: opt.Some([]string{"v"})},
	)
	equal(t, got.OptTags, opt.Some([]string{"v", "l"}))
}

func TestBlankFields(t *testing.T) {
	// Doc declares blank fields; the methods skip them and handle the
	// fields around them.
	v := mergemodel.Doc{
		ID:    "i",
		Name:  "n",
		Index: mergemodel.Index{"k": {A: 1}},
	}
	roundTrip[mergemodel.Doc, mergemodel.DocPartial](t, v)

	var p mergemodel.DocPartial
	p.FromValue(v)
	got := p.Merge(mergemodel.DocPartial{Name: und.Defined("m")})
	equal(t, got.Name, und.Defined("m"))
	equal(t, got.Index, p.Index)
}
