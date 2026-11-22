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
	"github.com/ngicks/kama/kamalib/pkg/validation"
)

func validRules() model.Rules {
	name := "c"

	return model.Rules{
		Null:      und.Null[int](),
		Slug:      "abc",
		SlugBytes: []byte("abc"),
		Items:     []int{1},
		Code:      opt.Some("ab"),
		Level:     99,
		Name:      &name,
		Ratio:     0.5,
		Score:     11,
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
	one, name, defined := 1, "b", und.Defined(1)

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
		Level:     100,
		Name:      &name,
		Ratio:     0.25,
		Score:     0,
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
	assert.NilError(t, r.Validate(false))
}

func TestRulesInvalid(t *testing.T) {
	err := invalidRules().Validate(false)

	cases := []struct {
		pointer string
		kind    error
		msg     string
	}{
		{"/zero", validation.ErrZero, "want the zero value, got 1"},
		{"/zero_ptr", validation.ErrZero, "want the zero value, got a non-nil value"},
		{"/nil", validation.ErrNil, "want nil"},
		{"/null", validation.ErrNull, "want null"},
		{"/null_ptr", validation.ErrNull, "want null"},
		{"/slug", validation.ErrPattern, `want a match of "^[a-z]+$", got "ABC"`},
		{"/slug_bytes", validation.ErrPattern, `got "a-b"`},
		{"/items", validation.ErrLength, "want length <= 3, got 4"},
		{"/code", validation.ErrLength, "want length == 2, got 3"},
		{"/level", validation.ErrCompare, "want < 100, got 100"},
		{"/name", validation.ErrCompare, `want > "b", got "b"`},
		{"/ratio", validation.ErrCompare, "want >= 0.5, got 0.25"},
		{"/score", validation.ErrCompare, "want > 10, got 0"},
		{"/agreed", validation.ErrCompare, "want == true, got false"},
		{"/color", validation.ErrConst, `want one of "red", "dark blue", got "green"`},
		{"/pick", validation.ErrConst, "want one of 1, 2, 3, got 4"},
		{"/date", validation.ErrFormat, `want format date (2006-01-02), got "2026/11/22"`},
		{"/at", validation.ErrFormat, "want format date-time"},
		{"/url", validation.ErrFormat, "want format url"},
		{"/elastic", validation.ErrNull, "want null"},
	}

	var want []string

	for _, tc := range cases {
		want = append(want, tc.pointer)

		at := validation.At(err, tc.pointer)
		assert.Equal(t, len(at), 1, tc.pointer)
		assert.ErrorIs(t, at[0], tc.kind)
		assert.ErrorContains(t, at[0], tc.msg)
	}

	assert.DeepEqual(t, validation.Pointers(err), slices.Sorted(slices.Values(want)))

	first := invalidRules().Validate(true)
	assert.DeepEqual(t, validation.Pointers(first), []string{"/zero"})
	assert.ErrorIs(t, first, validation.ErrZero)
}

// TestRulesLengthBounds fails the lower bound of a length with several
// comparisons.
func TestRulesLengthBounds(t *testing.T) {
	r := validRules()
	r.Items = nil

	at := validation.At(r.Validate(false), "/items")
	assert.Equal(t, len(at), 1)
	assert.ErrorContains(t, at[0], "want length >= 1, got 0")
}

// TestRulesStopWithinDirective fails both comparisons of one cmp
// directive. stopOnFirstError stops at the first of them too.
func TestRulesStopWithinDirective(t *testing.T) {
	r := validRules()
	r.Score = 5

	all := validation.At(r.Validate(false), "/score")
	assert.Equal(t, len(all), 2)
	assert.ErrorContains(t, all[0], "want > 10, got 5")
	assert.ErrorContains(t, all[1], "want != 5, got 5")

	first := validation.At(r.Validate(true), "/score")
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
		assert.DeepEqual(t, validation.Pointers(o.Validate(true)), []string{"/by_name/a/name"})
	}

	var multi interface{ Unwrap() []error }

	assert.Assert(t, errors.As(o.Validate(false), &multi))

	var got []string

	for _, err := range multi.Unwrap() {
		got = append(got, validation.Pointers(err)...)
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
	assert.DeepEqual(t, validation.Pointers(o.Validate(false)), []string{"/by_ratio/0.5/name"})

	o = model.Ordered{ByRatio: map[float64]model.Tag{math.NaN(): {}}}
	assert.DeepEqual(t, validation.Pointers(o.Validate(false)), []string{"/by_ratio/NaN/name"})
}

// TestStampPromotedIsZero compares a type promoting IsZero through an
// embedded pointer with its zero value, as calling IsZero would panic.
func TestStampPromotedIsZero(t *testing.T) {
	now := time.Unix(1, 0)

	err := model.Stamped{}.Validate(false)
	assert.DeepEqual(t, validation.Pointers(err), []string{"/at"})
	assert.ErrorIs(t, err, validation.ErrRequired)

	// A non-nil pointer to the zero time is not the zero value of Stamp.
	assert.NilError(t, model.Stamped{At: model.Stamp{Time: &time.Time{}}}.Validate(false))

	err = model.Stamped{At: model.Stamp{Time: &now}, Zero: model.Stamp{Time: &now}}.Validate(false)
	assert.DeepEqual(t, validation.Pointers(err), []string{"/zero"})
	assert.ErrorIs(t, err, validation.ErrZero)
}
