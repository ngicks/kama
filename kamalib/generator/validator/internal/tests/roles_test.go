package tests

import (
	"errors"
	"testing"

	"github.com/ngicks/option/either"
	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/tuple"
	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamaruntime/validator"
)

func validRoles() model.Roles {
	return model.Roles{
		Tags:    []string{"a", "abc"},
		Counts:  map[string]int{"a": 0, "b": 2},
		Note:    opt.Some("abc"),
		Limit:   und.Defined(1),
		Codes:   elastic.FromValues("a", "b", "c"),
		Choice:  either.Left[string, int]("x"),
		Outcome: either.Ok(1),
		Pair:    tuple.Tuple2[string, int]{V1: "a", V2: 9},
		Groups:  []map[string]int{{"a": 1}},
	}
}

func TestRolesValid(t *testing.T) {
	for _, stop := range []bool{true, false} {
		assert.NilError(t, validRoles().Validate(stop))
	}

	r := validRoles()
	r.Codes = elastic.Null[string]()
	r.Choice = either.Right[string](0)
	zero, five := 0, 5
	p := &five
	r.Deep = &p
	r.Wrapped = &opt.Option[int]{}
	assert.NilError(t, r.Validate(false))

	var nilInner *int

	r.Deep = &nilInner
	assert.NilError(t, r.Validate(false))

	p = &zero
	r.Deep = &p
	assert.ErrorIs(t, r.Validate(false), validator.ErrOr)
}

func TestRolesInvalid(t *testing.T) {
	zero := 0
	inner := &zero
	wrapped := opt.Some(0)

	r := model.Roles{
		Tags:    []string{"abcd"},
		Counts:  map[string]int{"B": 1, "a": -1},
		Note:    opt.Some("abcd"),
		Limit:   und.Defined(0),
		Codes:   elastic.FromValues("a", "abcd"),
		Choice:  either.Left[string, int](""),
		Outcome: either.Err[int](errors.New("failed")),
		Pair:    tuple.Tuple2[string, int]{V1: "", V2: 10},
		Groups:  []map[string]int{{}, {"a": -1}},
		Deep:    &inner,
		Wrapped: &wrapped,
	}

	err := r.Validate(false)

	for _, tc := range []struct {
		pointer string
		kind    error
	}{
		{"/tags/0", validator.ErrLength},
		{"/counts/B", validator.ErrPattern},
		{"/counts/a", validator.ErrCompare},
		{"/note", validator.ErrLength},
		{"/limit", validator.ErrCompare},
		{"/codes", validator.ErrOr},
		{"/codes/1", validator.ErrLength},
		{"/choice/left", validator.ErrLength},
		{"/outcome/err", validator.ErrNil},
		{"/pair/V1", validator.ErrLength},
		{"/pair/V2", validator.ErrCompare},
		{"/groups/0", validator.ErrLength},
		{"/groups/1/a", validator.ErrCompare},
		{"/deep", validator.ErrOr},
		{"/wrapped", validator.ErrCompare},
	} {
		at := validator.At(err, tc.pointer)
		assert.Assert(t, len(at) > 0, "%s in %v", tc.pointer, validator.Pointers(err))
		assert.Assert(t, slicesAny(at, tc.kind), "%s: %v", tc.pointer, at)
	}

	assert.Equal(t, len(validator.Pointers(r.Validate(true))), 1)
}

func TestBagRoles(t *testing.T) {
	assert.NilError(t, model.Bag[int]{Items: []int{1}, Main: opt.Some(2)}.Validate(false, nil))

	err := model.Bag[string]{Items: []string{"a", ""}, Main: opt.Some("")}.Validate(false, nil)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/items/1", "/main"})
}

// slicesAny reports whether one of errs is kind.
func slicesAny(errs []error, kind error) bool {
	for _, e := range errs {
		if errors.Is(e, kind) {
			return true
		}
	}

	return false
}
