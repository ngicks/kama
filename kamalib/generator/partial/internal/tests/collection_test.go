package tests

import (
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"

	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/partial/internal/fixture/model"
	mergemodel "github.com/ngicks/kama/kamalib/generator/partial/internal/fixturemerge/model"
)

func fullShelf() model.Shelf {
	return model.Shelf{
		Books:    []model.Book{{Title: "a", Pages: new(1)}, {}},
		BookRefs: []*model.Book{{Title: "b"}, nil},
		ByID:     map[string]model.Book{"a": {Title: "a"}},
		RefsByID: map[string]*model.Book{"b": {Title: "b"}, "nil": nil},
		List:     model.Books{{Title: "l", Pages: new(2)}, {}},
		RefList:  model.BookRefs{nil, {Title: "r"}},
		Map:      model.BookMap{"m": {Title: "m"}},
		RefMap:   model.BookRefMap{"r": {Title: "r"}, "nil": nil},
		ListPtr:  &model.Books{{Title: "p"}},
		RefMapPtr: &model.BookRefMap{
			"p": {Pages: new(3)}, "nil": nil,
		},
		Sections: model.Sections{
			{
				Name:  "s",
				Books: model.Books{{Title: "s"}},
				Refs:  model.BookRefMap{"s": {Title: "s"}, "nil": nil},
			},
			{},
		},
		Pages: model.Pages[string]{{Body: "x"}, {}},
	}
}

func TestCollectionRoundTrip(t *testing.T) {
	roundTrip[model.Shelf, model.ShelfPartial](t, model.Shelf{})
	roundTrip[model.Shelf, model.ShelfPartial](t, fullShelf())
	roundTrip[model.Shelf, model.ShelfPartial](t, model.Shelf{
		Books:     []model.Book{},
		BookRefs:  []*model.Book{},
		ByID:      map[string]model.Book{},
		RefsByID:  map[string]*model.Book{},
		List:      model.Books{},
		RefList:   model.BookRefs{},
		Map:       model.BookMap{},
		RefMap:    model.BookRefMap{},
		ListPtr:   &model.Books{},
		RefMapPtr: &model.BookRefMap{},
		Sections:  model.Sections{{Books: model.Books{}, Refs: model.BookRefMap{}}},
		Pages:     model.Pages[string]{},
	})
	// A pointer to a nil collection stays apart from a nil pointer.
	roundTrip[model.Shelf, model.ShelfPartial](t, model.Shelf{
		ListPtr:   new(model.Books(nil)),
		RefMapPtr: new(model.BookRefMap(nil)),
	})
	roundTrip[model.Catalog, model.CatalogPartial](t, model.Catalog{})
	roundTrip[model.Catalog, model.CatalogPartial](t, model.Catalog{
		Books:    []model.Book{{Title: "a"}},
		BookRefs: []*model.Book{nil},
		ByID:     map[string]model.Book{},
		RefsByID: map[string]*model.Book{"nil": nil},
		List:     model.Books{},
		RefMap:   model.BookRefMap{"nil": nil},
		Map:      model.BookMap{"m": {}},
		ListPtr:  new(model.Books(nil)),
		Sections: model.Sections{{Name: "s"}},
	})

	roundTrip[model.Books, model.BooksPartial](t, nil)
	roundTrip[model.Books, model.BooksPartial](t, model.Books{})
	roundTrip[model.BookRefs, model.BookRefsPartial](t, model.BookRefs{nil, {Title: "b"}})
	roundTrip[model.BookMap, model.BookMapPartial](t, nil)
	roundTrip[model.BookRefMap, model.BookRefMapPartial](t, model.BookRefMap{"nil": nil})
	roundTrip[model.Sections, model.SectionsPartial](t, fullShelf().Sections)
	roundTrip[model.Pages[int], model.PagesPartial[int]](t, model.Pages[int]{{Body: 1}})
	roundTrip[model.Pages[int], model.PagesPartial[int]](t, nil)

	roundTrip[mergemodel.Deck, mergemodel.DeckPartial](t, mergemodel.Deck{})
	roundTrip[mergemodel.Deck, mergemodel.DeckPartial](t, mergemodel.Deck{
		Cards:      []mergemodel.Card{{Name: "c", Rank: new(1)}},
		CardRefs:   []*mergemodel.Card{nil, {}},
		ByName:     map[string]mergemodel.Card{},
		RefsByName: map[string]*mergemodel.Card{"nil": nil},
		Kept:       []*mergemodel.Card{},
		List:       mergemodel.Cards{{}},
		RefList:    mergemodel.CardRefs{nil},
		Map:        mergemodel.CardMap{"m": {}},
		RefMap:     mergemodel.CardRefMap{"nil": nil},
		ListPtr:    new(mergemodel.Cards(nil)),
		MapPtr:     &mergemodel.CardMap{},
		Hands:      mergemodel.Hands{{Cards: mergemodel.Cards{}}},
		Piles:      mergemodel.Piles[int]{{Top: 1}},
	})
}

func TestCollectionFromValueStates(t *testing.T) {
	v := fullShelf()
	var p model.ShelfPartial
	p.FromValue(v)

	// An unnamed slice or map of targets is wrapped as a whole and shares
	// its storage with v.
	equal(t, p.Books, und.Defined(v.Books))
	assert.Assert(t, &p.Books.Value()[0] == &v.Books[0])
	equal(t, p.RefsByID, und.Defined(v.RefsByID))
	assert.Assert(t, p.RefsByID.Value()["b"] == v.RefsByID["b"])

	// A collection target converts element by element, and a nil element
	// pointer becomes null.
	equal(t, p.RefList, und.Defined(model.BookRefsPartial{
		und.Null[model.BookPartial](),
		und.Defined(model.BookPartial{Title: und.Defined("r"), Pages: und.Null[int]()}),
	}))
	assert.Assert(t, p.RefMap.Value()["nil"].IsNull())

	// A nil pointer to a collection target becomes null, and a pointer to
	// a nil collection a defined nil partial.
	p.FromValue(model.Shelf{RefMapPtr: new(model.BookRefMap(nil))})
	assert.Assert(t, p.ListPtr.IsNull())
	equal(t, p.RefMapPtr, und.Defined(model.BookRefMapPartial(nil)))
	assert.Assert(t, p.List.IsNull())
	assert.Assert(t, p.Books.IsNull())

	p.FromValue(model.Shelf{ListPtr: &model.Books{}})
	equal(t, p.ListPtr, und.Defined(model.BooksPartial{}))
}

func TestCollectionToValueStates(t *testing.T) {
	book := und.Defined(model.BookPartial{Title: und.Defined("b")})
	v := model.ShelfPartial{
		List: und.Defined(model.BooksPartial{
			und.Undefined[model.BookPartial](), und.Null[model.BookPartial](), book,
		}),
		RefList: und.Defined(model.BookRefsPartial{
			und.Undefined[model.BookPartial](), und.Null[model.BookPartial](), book,
		}),
		Map: und.Defined(model.BookMapPartial{"u": und.Undefined[model.BookPartial]()}),
		RefMap: und.Defined(model.BookRefMapPartial{
			"u": und.Undefined[model.BookPartial](), "n": und.Null[model.BookPartial](),
		}),
		ListPtr:   und.Null[model.BooksPartial](),
		RefMapPtr: und.Defined(model.BookRefMapPartial{}),
	}.ToValue()

	// An undefined or null element gives the zero value of the element,
	// which is nil for a pointer element.
	equal(t, v.List, model.Books{{}, {}, {Title: "b"}})
	equal(t, v.RefList, model.BookRefs{nil, nil, {Title: "b"}})
	equal(t, v.Map, model.BookMap{"u": {}})
	equal(t, v.RefMap, model.BookRefMap{"u": nil, "n": nil})
	assert.Assert(t, v.ListPtr == nil)
	equal(t, v.RefMapPtr, &model.BookRefMap{})
	assert.Assert(t, v.Sections == nil)
}

func TestCollectionMergeDefault(t *testing.T) {
	book := func(title string) und.Und[model.BookPartial] {
		return und.Defined(model.BookPartial{Title: und.Defined(title)})
	}
	pages := und.Defined(model.BookPartial{Pages: und.Defined(1)})

	// Unnamed collections overwrite.
	for _, r := range states([]*model.Book{{Title: "r"}}) {
		l := und.Defined([]*model.Book{{Title: "l"}})
		got := model.ShelfPartial{BookRefs: l}.Merge(model.ShelfPartial{BookRefs: r})
		if r.IsUndefined() {
			equal(t, got.BookRefs, l)
		} else {
			equal(t, got.BookRefs, r)
		}
	}

	// Collection targets merge recursively, and their own Merge overwrites
	// without merging the elements, so a defined r replaces p unless it is
	// nil.
	for _, l := range states(model.BooksPartial{book("l")}) {
		for rn, r := range states(model.BooksPartial{pages}) {
			got := model.ShelfPartial{List: l, ListPtr: l}.
				Merge(model.ShelfPartial{List: r, ListPtr: r})
			want := r
			if rn == "undefined" {
				want = l
			}
			equal(t, got.List, want)
			equal(t, got.ListPtr, want)
		}
	}
	l := model.ShelfPartial{
		Map:    und.Defined(model.BookMapPartial{"a": book("l")}),
		RefMap: und.Defined(model.BookRefMapPartial{"a": book("l")}),
	}
	got := l.Merge(model.ShelfPartial{
		Map:    und.Defined(model.BookMapPartial{"b": book("r")}),
		RefMap: und.Defined(model.BookRefMapPartial(nil)),
	})
	equal(t, got.Map, und.Defined(model.BookMapPartial{"b": book("r")}))
	equal(t, got.RefMap, l.RefMap)

	// The nested collections of the elements are not merged either.
	sections := func(b und.Und[model.BookPartial]) und.Und[model.SectionsPartial] {
		return und.Defined(model.SectionsPartial{und.Defined(model.SectionPartial{
			Books: und.Defined(model.BooksPartial{b}),
		})})
	}
	gotSections := model.ShelfPartial{Sections: sections(book("l"))}.
		Merge(model.ShelfPartial{Sections: sections(pages)})
	equal(t, gotSections.Sections, sections(pages))

	pagesOf := func(body string) und.Und[model.PagesPartial[string]] {
		return und.Defined(model.PagesPartial[string]{
			und.Defined(model.PagePartial[string]{Body: und.Defined(body)}),
		})
	}
	gotPages := model.ShelfPartial{
		Pages: pagesOf("l"),
	}.Merge(
		model.ShelfPartial{Pages: pagesOf("r")},
	)
	equal(t, gotPages.Pages, pagesOf("r"))
}

func TestCollectionMergeAppendDirective(t *testing.T) {
	book := func(title string) model.Book { return model.Book{Title: title} }
	bookP := func(title string) und.Und[model.BookPartial] {
		return und.Defined(model.BookPartial{Title: und.Defined(title)})
	}

	for ln, l := range states([]model.Book{book("l")}) {
		for rn, r := range states([]model.Book{book("r")}) {
			got := model.CatalogPartial{Books: l}.Merge(model.CatalogPartial{Books: r}).Books
			switch {
			case rn == "undefined":
				equal(t, got, l)
			case ln == "defined" && rn == "defined":
				equal(t, got, und.Defined([]model.Book{book("l"), book("r")}))
			default:
				equal(t, got, r)
			}
		}
	}

	for ln, l := range states(model.BooksPartial{bookP("l")}) {
		for rn, r := range states(model.BooksPartial{bookP("r")}) {
			got := model.CatalogPartial{List: l, ListPtr: l}.
				Merge(model.CatalogPartial{List: r, ListPtr: r})
			switch {
			case rn == "undefined":
				equal(t, got.List, l)
				equal(t, got.ListPtr, l)
			case ln == "defined" && rn == "defined":
				want := und.Defined(model.BooksPartial{bookP("l"), bookP("r")})
				equal(t, got.List, want)
				equal(t, got.ListPtr, want)
			default:
				equal(t, got.List, r)
				equal(t, got.ListPtr, r)
			}
		}
	}

	// Appending keeps the element pointers of both sides.
	lb, rb := &model.Book{Title: "l"}, &model.Book{Title: "r"}
	gotRefs := model.CatalogPartial{BookRefs: und.Defined([]*model.Book{lb})}.
		Merge(model.CatalogPartial{BookRefs: und.Defined([]*model.Book{nil, rb})}).BookRefs.Value()
	assert.Equal(t, len(gotRefs), 3)
	assert.Assert(t, gotRefs[0] == lb && gotRefs[1] == nil && gotRefs[2] == rb)

	gotMaps := model.CatalogPartial{
		ByID:     und.Defined(map[string]model.Book{"a": book("l"), "b": book("l")}),
		RefsByID: und.Defined(map[string]*model.Book{"a": lb}),
		RefMap:   und.Defined(model.BookRefMapPartial{"a": bookP("l"), "b": bookP("l")}),
	}.Merge(model.CatalogPartial{
		ByID:     und.Defined(map[string]model.Book{"b": book("r")}),
		RefsByID: und.Defined(map[string]*model.Book{"b": nil}),
		RefMap:   und.Defined(model.BookRefMapPartial{"b": und.Null[model.BookPartial]()}),
	})
	equal(t, gotMaps.ByID, und.Defined(map[string]model.Book{"a": book("l"), "b": book("r")}))
	equal(t, gotMaps.RefsByID, und.Defined(map[string]*model.Book{"a": lb, "b": nil}))
	equal(t, gotMaps.RefMap, und.Defined(model.BookRefMapPartial{
		"a": bookP("l"), "b": und.Null[model.BookPartial](),
	}))

	sections := func(name string) model.SectionsPartial {
		return model.SectionsPartial{und.Defined(model.SectionPartial{Name: und.Defined(name)})}
	}
	gotSections := model.CatalogPartial{Sections: und.Defined(sections("l"))}.
		Merge(model.CatalogPartial{Sections: und.Defined(sections("r"))})
	equal(t, gotSections.Sections, und.Defined(append(sections("l"), sections("r")...)))

	// The overwrite directive replaces the map target with a defined nil,
	// which the default recursive strategy of Shelf.Map ignores.
	m := und.Defined(model.BookMapPartial{"a": bookP("l")})
	nilMap := und.Defined(model.BookMapPartial(nil))
	equal(t, model.CatalogPartial{Map: m}.Merge(model.CatalogPartial{Map: nilMap}).Map, nilMap)
	equal(t, model.ShelfPartial{Map: m}.Merge(model.ShelfPartial{Map: nilMap}).Map, m)
}

func TestCollectionMergeAppendDefault(t *testing.T) {
	card := func(name string) und.Und[mergemodel.CardPartial] {
		return und.Defined(mergemodel.CardPartial{Name: und.Defined(name)})
	}
	lc, rc := &mergemodel.Card{Name: "l"}, &mergemodel.Card{Name: "r"}
	l := mergemodel.DeckPartial{
		Cards:      und.Defined([]mergemodel.Card{*lc}),
		CardRefs:   und.Defined([]*mergemodel.Card{lc}),
		ByName:     und.Defined(map[string]mergemodel.Card{"a": *lc, "b": *lc}),
		RefsByName: und.Defined(map[string]*mergemodel.Card{"a": lc}),
		Kept:       und.Defined([]*mergemodel.Card{lc}),
		List:       und.Defined(mergemodel.CardsPartial{card("l")}),
		RefList:    und.Defined(mergemodel.CardRefsPartial{card("l")}),
		Map:        und.Defined(mergemodel.CardMapPartial{"a": card("l"), "b": card("l")}),
		RefMap:     und.Defined(mergemodel.CardRefMapPartial{"a": card("l")}),
		ListPtr:    und.Defined(mergemodel.CardsPartial{card("l")}),
		MapPtr:     und.Defined(mergemodel.CardMapPartial{"a": card("l")}),
		Hands: und.Defined(mergemodel.HandsPartial{und.Defined(mergemodel.HandPartial{
			Cards: und.Defined(mergemodel.CardsPartial{card("l")}),
		})}),
		Piles: und.Defined(mergemodel.PilesPartial[int]{
			und.Defined(mergemodel.PilePartial[int]{Top: und.Defined(1)}),
		}),
	}
	r := mergemodel.DeckPartial{
		Cards:      und.Defined([]mergemodel.Card{*rc}),
		CardRefs:   und.Defined([]*mergemodel.Card{rc}),
		ByName:     und.Defined(map[string]mergemodel.Card{"b": *rc}),
		RefsByName: und.Defined(map[string]*mergemodel.Card{"b": rc}),
		Kept:       und.Defined([]*mergemodel.Card{rc}),
		List:       und.Defined(mergemodel.CardsPartial{card("r")}),
		RefList:    und.Defined(mergemodel.CardRefsPartial{und.Null[mergemodel.CardPartial]()}),
		Map:        und.Defined(mergemodel.CardMapPartial{"b": card("r")}),
		RefMap:     und.Defined(mergemodel.CardRefMapPartial{"b": card("r")}),
		ListPtr:    und.Defined(mergemodel.CardsPartial{card("r")}),
		MapPtr:     und.Defined(mergemodel.CardMapPartial{"b": card("r")}),
		Hands: und.Defined(mergemodel.HandsPartial{und.Defined(mergemodel.HandPartial{
			Cards: und.Defined(mergemodel.CardsPartial{card("r")}),
		})}),
		Piles: und.Defined(mergemodel.PilesPartial[int]{
			und.Defined(mergemodel.PilePartial[int]{Top: und.Defined(2)}),
		}),
	}
	got := l.Merge(r)

	// Unnamed collections append, unless a directive says otherwise.
	equal(t, got.Cards, und.Defined([]mergemodel.Card{*lc, *rc}))
	equal(t, got.CardRefs, und.Defined([]*mergemodel.Card{lc, rc}))
	equal(t, got.ByName, und.Defined(map[string]mergemodel.Card{"a": *lc, "b": *rc}))
	equal(t, got.RefsByName, und.Defined(map[string]*mergemodel.Card{"a": lc, "b": rc}))
	equal(t, got.Kept, r.Kept)
	// Collection targets overwrite as partial values, unless a directive
	// says otherwise.
	equal(t, got.List, r.List)
	equal(t, got.ListPtr, r.ListPtr)
	// Under recursive, the own Merge of a collection target appends or sets
	// the entries of r, without merging the elements.
	equal(t, got.RefList, und.Defined(mergemodel.CardRefsPartial{
		card("l"), und.Null[mergemodel.CardPartial](),
	}))
	equal(t, got.Map, und.Defined(mergemodel.CardMapPartial{"a": card("l"), "b": card("r")}))
	equal(t, got.MapPtr, und.Defined(mergemodel.CardMapPartial{"a": card("l"), "b": card("r")}))
	equal(t, got.Hands, und.Defined(append(
		mergemodel.HandsPartial{}, l.Hands.Value()[0], r.Hands.Value()[0],
	)))
	equal(t, got.Piles, und.Defined(append(
		mergemodel.PilesPartial[int]{}, l.Piles.Value()[0], r.Piles.Value()[0],
	)))
	equal(t, got.RefMap, und.Defined(mergemodel.CardRefMapPartial{"a": card("l"), "b": card("r")}))

	// A defined nil r keeps p under recursive.
	gotNil := l.Merge(mergemodel.DeckPartial{
		RefList: und.Defined(mergemodel.CardRefsPartial(nil)),
		List:    und.Defined(mergemodel.CardsPartial(nil)),
	})
	equal(t, gotNil.RefList, l.RefList)
	equal(t, gotNil.List, und.Defined(mergemodel.CardsPartial(nil)))
}

func TestCollectionApplyTo(t *testing.T) {
	v := fullShelf()
	p := model.ShelfPartial{
		List: und.Defined(model.BooksPartial{
			und.Defined(model.BookPartial{Title: und.Defined("new")}),
		}),
		RefMap:  und.Null[model.BookRefMapPartial](),
		ListPtr: und.Null[model.BooksPartial](),
		Books:   und.Defined([]model.Book{{Title: "replaced"}}),
	}
	got := p.ApplyTo(v)

	want := fullShelf()
	// The element is replaced, not merged, so Pages becomes nil.
	want.List = model.Books{{Title: "new"}}
	want.RefMap = nil
	want.ListPtr = nil
	want.Books = []model.Book{{Title: "replaced"}}
	equal(t, got, want)
	equal(t, v, fullShelf())

	var same model.ShelfPartial
	same.FromValue(v)
	equal(t, same.ApplyTo(model.Shelf{}), v)

	// The partial form of a pointer to a nil collection is a defined nil,
	// which recursive merging ignores, so it cannot clear the target.
	var fromNil model.ShelfPartial
	fromNil.FromValue(model.Shelf{ListPtr: new(model.Books(nil))})
	equal(t, fromNil.ApplyTo(model.Shelf{ListPtr: &model.Books{{}}}).ListPtr, &model.Books{{}})

	c := model.CatalogPartial{
		Books:    und.Defined([]model.Book{{Title: "r"}}),
		List:     und.Defined(model.BooksPartial{und.Null[model.BookPartial]()}),
		RefsByID: und.Defined(map[string]*model.Book{"b": nil}),
	}
	gotCatalog := c.ApplyTo(model.Catalog{
		Books:    []model.Book{{Title: "l"}},
		List:     model.Books{{Title: "l"}},
		RefsByID: map[string]*model.Book{"a": {Title: "a"}, "b": {}},
	})
	equal(t, gotCatalog, model.Catalog{
		Books:    []model.Book{{Title: "l"}, {Title: "r"}},
		List:     model.Books{{Title: "l"}, {}},
		RefsByID: map[string]*model.Book{"a": {Title: "a"}, "b": nil},
	})

	d := mergemodel.DeckPartial{
		CardRefs: und.Defined([]*mergemodel.Card{nil}),
		Map:      und.Defined(mergemodel.CardMapPartial{"b": und.Null[mergemodel.CardPartial]()}),
	}
	gotDeck := d.ApplyTo(mergemodel.Deck{
		CardRefs: []*mergemodel.Card{{Name: "l"}},
		Map:      mergemodel.CardMap{"a": {Name: "a"}, "b": {Name: "b"}},
	})
	equal(t, gotDeck, mergemodel.Deck{
		CardRefs: []*mergemodel.Card{{Name: "l"}, nil},
		Map:      mergemodel.CardMap{"a": {Name: "a"}, "b": {}},
	})
}

func TestCollectionJSON(t *testing.T) {
	var p model.ShelfPartial
	p.FromValue(fullShelf())
	b, err := jsonv2.Marshal(p)
	assert.NilError(t, err)
	var back model.ShelfPartial
	assert.NilError(t, jsonv2.Unmarshal(b, &back))
	equal(t, back.ToValue(), fullShelf())

	// An undefined element has no encoding of its own: it marshals as
	// null and comes back null.
	p = model.ShelfPartial{
		List: und.Defined(model.BooksPartial{und.Undefined[model.BookPartial]()}),
		RefMap: und.Defined(model.BookRefMapPartial{
			"u": und.Undefined[model.BookPartial](),
		}),
	}
	b, err = jsonv2.Marshal(p)
	assert.NilError(t, err)
	assert.Equal(t, string(b), `{"list":[null],"ref_map":{"u":null}}`)
	back = model.ShelfPartial{}
	assert.NilError(t, jsonv2.Unmarshal(b, &back))
	equal(t, back, model.ShelfPartial{
		List:   und.Defined(model.BooksPartial{und.Null[model.BookPartial]()}),
		RefMap: und.Defined(model.BookRefMapPartial{"u": und.Null[model.BookPartial]()}),
	})

	// The partial form of a pointer to a nil collection is a defined nil
	// partial, which encoding/json/v2 marshals as empty, so it comes back
	// as a pointer to an empty collection.
	p = model.ShelfPartial{}
	p.FromValue(model.Shelf{
		ListPtr:   new(model.Books(nil)),
		RefMapPtr: new(model.BookRefMap(nil)),
	})
	b, err = jsonv2.Marshal(p)
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(b), `"list_ptr":[],"ref_map_ptr":{}`), string(b))
	back = model.ShelfPartial{}
	assert.NilError(t, jsonv2.Unmarshal(b, &back))
	got := back.ToValue()
	equal(t, got.ListPtr, &model.Books{})
	equal(t, got.RefMapPtr, &model.BookRefMap{})
}
