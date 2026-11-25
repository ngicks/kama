package wiretypes

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"slices"
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

// decodeErr returns the error Unmarshal reports for doc, unwrapped from the
// json.SemanticError of the root.
func decodeErr(doc string) error {
	var r Root

	err := json.Unmarshal([]byte(doc), &r)
	if se, ok := errors.AsType[*json.SemanticError](err); ok && se.Err != nil {
		return se.Err
	}

	return err
}

// TestNested decodes integers written with a fraction of zero into
// containers nested in containers, in an opt.Option inside a slice, in the
// members no property declares and in union members.
func TestNested(t *testing.T) {
	var r Root

	assert.NilError(t, json.Unmarshal([]byte(`{
		"grid":[[1.0,2],[]],"groups":{"g":["a"]},"maybe":[1e1,null],
		"matrix":[[1.5]],"color":"red","either":3.0,"list":[4.0],"n":5.0
	}`), &r))

	assert.DeepEqual(t, r.Grid.Value(), [][]int64{{1, 2}, {}})
	assert.DeepEqual(t, r.Groups.Value(), map[string][]string{"g": {"a"}})
	assert.Assert(t, opt.EqualOptions(
		r.Maybe.Value(), []opt.Option[int64]{opt.Some(int64(10)), opt.None[int64]()},
	))
	assert.DeepEqual(t, r.Matrix.Value(), Matrix{{1.5}})
	assert.Equal(t, r.Color.Value(), ColorRed)
	assert.Equal(t, r.Either.Value().Kind, RootEitherKind(RootEitherInteger{Integer: 3}))
	assert.DeepEqual(t, r.List.Value().Kind, RootListKind(RootListArray{Array: []int64{4}}))
	assert.DeepEqual(t, r.AdditionalProperties, map[string]int64{"n": 5})
}

// TestNestedNulls reports every null and every integer with a fraction in
// a nested position at once, at its path.
func TestNestedNulls(t *testing.T) {
	err := decodeErr(`{
		"grid":[[1,null],null,[1.5]],"groups":{"g":[null],"h":null},"maybe":[2.5,null],
		"n":null,"m":1.5
	}`)

	pointers := validator.Pointers(err)
	slices.Sort(pointers)
	assert.DeepEqual(t, pointers, []string{
		"/grid/0/1", "/grid/1", "/grid/2/0", "/groups/g/0", "/groups/h", "/m", "/maybe/0", "/n",
	})
}

// TestDefinedNull rejects null for a defined type and an enum, whose own
// decoders convert, and for an element of the defined type. A defined type
// of raw elements keeps a null element.
func TestDefinedNull(t *testing.T) {
	for _, c := range []struct {
		doc, ptr string
		is       error
	}{
		{`{"matrix":null}`, "/matrix", validator.ErrNull},
		{`{"matrix":[[1],null,[null]]}`, "/matrix/1", validator.ErrNull},
		{`{"color":null}`, "/color", validator.ErrNull},
		{`{"raws":null}`, "/raws", validator.ErrNull},
	} {
		err := decodeErr(c.doc)
		assert.ErrorIs(t, err, c.is, c.doc)
		assert.Assert(t, slices.Contains(validator.Pointers(err), c.ptr), "%s: %v", c.doc, err)
	}

	var r Root

	assert.NilError(t, json.Unmarshal([]byte(`{"raws":[null]}`), &r))
	assert.DeepEqual(t, r.Raws.Value(), Raws{jsontext.Value("null")})

	err := decodeErr(`{"matrix":[[1],null,[null]]}`)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/matrix/1", "/matrix/2/0"})
}

// TestUnionMemberConversion fails a union member whose conversion fails,
// which leaves the value to no member.
func TestUnionMemberConversion(t *testing.T) {
	err := decodeErr(`{"either":1.5,"list":[null]}`)
	// The member Array reports its null below the union.
	assert.DeepEqual(t, validator.Pointers(err), []string{"/either", "/list", "/list/0"})
	assert.ErrorIs(t, validator.Join(validator.At(err, "/either")...), validator.ErrOr)
	assert.ErrorIs(t, validator.Join(validator.At(err, "/list")...), validator.ErrOneOf)
}
