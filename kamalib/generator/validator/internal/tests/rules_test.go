package tests

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamaruntime/validator"
)

func validRules() model.Rules {
	name, quarter := "c", float32(0.75)

	return model.Rules{
		Null:      und.Null[int](),
		Slug:      "abc",
		SlugBytes: []byte("abc"),
		Items:     []int{1},
		Code:      opt.Some("ab"),
		Kana:      "あい",
		Tags:      []any{1, "1", []int{1}, []int{2}},
		Triple:    opt.Some([3]int{1, 2, 3}),
		Refs:      []*int{new(1), new(2)},
		Level:     99,
		Name:      &name,
		Ratio:     0.5,
		Score:     11,
		Step:      -9,
		Quarter:   &quarter,
		Agreed:    true,
		Color:     "dark blue",
		Pick:      3,
		Date:      "2026-11-22",
		At:        opt.Some("2026-11-22T10:00:00Z"),
		Url:       "https://example.com/a",
		Elastic:   elastic.Null[int](),
	}
}

// invalidRules breaks every validator of Rules once.
func invalidRules() model.Rules {
	one, name, defined, quarter := 1, "b", und.Defined(1), float32(0.3)

	return model.Rules{
		Zero:      1,
		ZeroPtr:   &one,
		Nil:       []int{},
		Null:      und.Defined(1),
		NullPtr:   &defined,
		Slug:      "ABC",
		SlugBytes: []byte("a-b"),
		Items:     []int{1, 2, 3, 4},
		Code:      opt.Some("abc"),
		Kana:      "abc",
		Tags:      []any{[]int{1}, "a", []int{1}},
		Triple:    opt.Some([3]int{1, 2, 1}),
		Refs:      []*int{new(1), new(1)},
		Level:     100,
		Name:      &name,
		Ratio:     0.25,
		Score:     0,
		Step:      4,
		Quarter:   &quarter,
		Agreed:    false,
		Color:     "green",
		Pick:      4,
		Date:      "2026/11/22",
		At:        opt.Some("2026-11-22"),
		Url:       "/relative",
		Elastic:   elastic.FromValue(1),
	}
}

func TestRulesValid(t *testing.T) {
	for _, stop := range []bool{true, false} {
		assert.NilError(t, validRules().Validate(stop))
	}

	// A pointer or an option holding nothing skips the validators of the
	// value it would hold.
	r := validRules()
	r.Name, r.Code, r.At, r.NullPtr = nil, opt.None[string](), opt.None[string](), nil
	r.Triple, r.Quarter = opt.None[[3]int](), nil
	assert.NilError(t, r.Validate(false))
}

func TestRulesInvalid(t *testing.T) {
	err := invalidRules().Validate(false)

	cases := []struct {
		pointer string
		kind    error
		msg     string
	}{
		{"/zero", validator.ErrZero, "want the zero value, got 1"},
		{"/zero_ptr", validator.ErrZero, "want the zero value, got a non-nil value"},
		{"/nil", validator.ErrNil, "want nil"},
		{"/null", validator.ErrNull, "want null"},
		{"/null_ptr", validator.ErrNull, "want null"},
		{"/slug", validator.ErrPattern, `want a match of "^[a-z]+$", got "ABC"`},
		{"/slug_bytes", validator.ErrPattern, `got "a-b"`},
		{"/items", validator.ErrLength, "want length <= 3, got 4"},
		{"/code", validator.ErrLength, "want length == 2, got 3"},
		{"/kana", validator.ErrLength, "want rune count == 2, got 3"},
		{"/tags", validator.ErrUnique, "want unique elements, got equal elements at 0 and 2"},
		{"/triple", validator.ErrUnique, "want unique elements, got equal elements at 0 and 2"},
		{"/refs", validator.ErrUnique, "want unique elements, got equal elements at 0 and 1"},
		{"/level", validator.ErrCompare, "want < 100, got 100"},
		{"/name", validator.ErrCompare, `want > "b", got "b"`},
		{"/ratio", validator.ErrCompare, "want >= 0.5, got 0.25"},
		{"/score", validator.ErrCompare, "want > 10, got 0"},
		{"/step", validator.ErrMultiple, "want a multiple of 3, got 4"},
		{"/quarter", validator.ErrMultiple, "want a multiple of 0.25, got 0.3"},
		{"/agreed", validator.ErrCompare, "want == true, got false"},
		{"/color", validator.ErrConst, `want one of "red", "dark blue", got "green"`},
		{"/pick", validator.ErrConst, "want one of 1, 2, 3, got 4"},
		{"/date", validator.ErrFormat, `want format date (2006-01-02), got "2026/11/22"`},
		{"/at", validator.ErrFormat, "want format date-time"},
		{"/url", validator.ErrFormat, "want format url"},
		{"/elastic", validator.ErrNull, "want null"},
	}

	var want []string

	for _, tc := range cases {
		want = append(want, tc.pointer)

		at := validator.At(err, tc.pointer)
		assert.Equal(t, len(at), 1, tc.pointer)
		assert.ErrorIs(t, at[0], tc.kind)
		assert.ErrorContains(t, at[0], tc.msg)
	}

	assert.DeepEqual(t, validator.Pointers(err), slices.Sorted(slices.Values(want)))

	first := invalidRules().Validate(true)
	assert.DeepEqual(t, validator.Pointers(first), []string{"/zero"})
	assert.ErrorIs(t, first, validator.ErrZero)
}

// TestRulesLengthBounds fails the lower bound of a length with several
// comparisons.
func TestRulesLengthBounds(t *testing.T) {
	r := validRules()
	r.Items = nil

	at := validator.At(r.Validate(false), "/items")
	assert.Equal(t, len(at), 1)
	assert.ErrorContains(t, at[0], "want length >= 1, got 0")
}

// TestRulesStopWithinDirective fails both comparisons of one cmp
// directive. stopOnFirstError stops at the first of them too.
func TestRulesStopWithinDirective(t *testing.T) {
	r := validRules()
	r.Score = 5

	all := validator.At(r.Validate(false), "/score")
	assert.Equal(t, len(all), 2)
	assert.ErrorContains(t, all[0], "want > 10, got 5")
	assert.ErrorContains(t, all[1], "want != 5, got 5")

	first := validator.At(r.Validate(true), "/score")
	assert.Equal(t, len(first), 1)
	assert.ErrorContains(t, first[0], "want > 10, got 5")
}

// TestMapOrder visits map keys in sorted order: numerically for an integer
// key type.
func TestMapOrder(t *testing.T) {
	o := model.Ordered{
		ByName:  map[string]model.Tag{"c": {}, "a": {}, "b": {Name: "b"}, "d": {}},
		ByLevel: map[model.Level]model.Tag{10: {}, 2: {}, 1: {}},
	}

	for range 10 {
		assert.DeepEqual(t, validator.Pointers(o.Validate(true)), []string{"/by_name/a/name"})
	}

	var multi interface{ Unwrap() []error }

	assert.Assert(t, errors.As(o.Validate(false), &multi))

	var got []string

	for _, err := range multi.Unwrap() {
		got = append(got, validator.Pointers(err)...)
	}

	assert.DeepEqual(t, got, []string{
		"/by_name/a/name",
		"/by_name/c/name",
		"/by_name/d/name",
		"/by_level/1/name",
		"/by_level/2/name",
		"/by_level/10/name",
	})
}

// TestMapFloatKey ranges over float keys directly, so that the value of a
// NaN key is validated.
func TestMapFloatKey(t *testing.T) {
	o := model.Ordered{ByRatio: map[float64]model.Tag{math.NaN(): {Name: "ok"}, 0.5: {}}}
	assert.DeepEqual(t, validator.Pointers(o.Validate(false)), []string{"/by_ratio/0.5/name"})

	o = model.Ordered{ByRatio: map[float64]model.Tag{math.NaN(): {}}}
	assert.DeepEqual(t, validator.Pointers(o.Validate(false)), []string{"/by_ratio/NaN/name"})
}

// TestStampPromotedIsZero compares a type promoting IsZero through an
// embedded pointer with its zero value, as calling IsZero would panic.
func TestStampPromotedIsZero(t *testing.T) {
	now := time.Unix(1, 0)

	err := model.Stamped{}.Validate(false)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/at"})
	assert.ErrorIs(t, err, validator.ErrRequired)

	// A non-nil pointer to the zero time is not the zero value of Stamp.
	assert.NilError(t, model.Stamped{At: model.Stamp{Time: &time.Time{}}}.Validate(false))

	err = model.Stamped{At: model.Stamp{Time: &now}, Zero: model.Stamp{Time: &now}}.Validate(false)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/zero"})
	assert.ErrorIs(t, err, validator.ErrZero)
}

// TestRequires reports each sibling a present field requires and lacks, at
// the sibling.
func TestRequires(t *testing.T) {
	city := "c"

	for _, valid := range []model.Shipping{
		{},
		{City: &city, Zip: und.Null[string]()},
		{Street: opt.Some("s"), City: &city, Zip: und.Defined("z"), Express: true},
	} {
		for _, stop := range []bool{true, false} {
			assert.NilError(t, valid.Validate(stop))
		}
	}

	// A null Zip is present, as a JSON member holding null is.
	err := model.Shipping{Street: opt.Some(""), Zip: und.Null[string](), Express: true}.
		Validate(false)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/city"})

	invalid := model.Shipping{Street: opt.Some(""), Express: true}
	err = invalid.Validate(false)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/city", "/zip"})
	assert.ErrorIs(t, validator.At(err, "/city")[0], validator.ErrRequired)
	assert.ErrorContains(t, validator.At(err, "/zip")[0], `want a value, as "street" has one`)
	assert.DeepEqual(t, validator.Pointers(invalid.Validate(true)), []string{"/city"})

	err = model.Shipping{Express: true}.Validate(false)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/street"})
	assert.ErrorContains(t, err, `want a value, as "express" has one`)
}

// TestMembers counts the fields present, the fields without an optional
// state and the entries of the embedded fallback.
func TestMembers(t *testing.T) {
	phone := "p"

	for _, valid := range []model.Contact{
		{Email: opt.Some("e")},
		{Phone: &phone, Extra: map[string]any{"x": 1}},
	} {
		for _, stop := range []bool{true, false} {
			assert.NilError(t, valid.Validate(stop))
		}
	}

	err := model.Contact{}.Validate(false)
	assert.DeepEqual(t, validator.Pointers(err), []string{""})
	assert.ErrorIs(t, err, validator.ErrLength)
	assert.ErrorContains(t, err, "want member count >= 2, got 1")

	err = model.Contact{Email: opt.Some("e"), Phone: &phone, Extra: map[string]any{"x": 1}}.
		Validate(false)
	assert.ErrorContains(t, err, "want member count <= 3, got 4")
}

// TestClosed reports each entry of the embedded fallback at its member.
func TestClosed(t *testing.T) {
	assert.NilError(t, model.Strict{Name: "n"}.Validate(false))

	strict := model.Strict{Extra: map[string]any{"b": 1, "a": 2}}

	err := strict.Validate(false)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/a", "/b"})
	assert.ErrorIs(t, err, validator.ErrClosed)
	assert.ErrorContains(t, err, "member is not allowed")

	assert.DeepEqual(t, validator.Pointers(strict.Validate(true)), []string{"/a"})
}

// TestBoundsOfIntegers compares integers exactly with numbers their types
// cannot represent.
func TestBoundsOfIntegers(t *testing.T) {
	for _, half := range []int{2, 3} {
		assert.NilError(t, model.Bounds{Half: half, Small: -128}.Validate(false))
	}

	for _, half := range []int{1, 4} {
		err := model.Bounds{Half: half}.Validate(false)
		assert.DeepEqual(t, validator.Pointers(err), []string{"/half"})
		assert.ErrorIs(t, err, validator.ErrCompare)
	}

	n := 0

	err := model.Bounds{Half: 2, Never: &n}.Validate(false)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/never"})
	assert.ErrorContains(t, err, "want == 0.5, got 0")
}
