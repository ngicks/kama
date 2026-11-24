package edgecases

import (
	"encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

// check decodes each instance of ok into a Root and each of bad into a
// Root, which must fail with the error kind given.
func check(t *testing.T, ok []string, bad map[string]error) {
	t.Helper()

	for _, text := range ok {
		var v Root
		assert.NilError(t, json.Unmarshal([]byte(text), &v), text)
	}

	for text, kind := range bad {
		var v Root
		assert.ErrorIs(t, json.Unmarshal([]byte(text), &v), kind, text)
	}
}

func TestPropertiesNamedLikeMethods(t *testing.T) {
	var v Root
	assert.NilError(t, json.Unmarshal([]byte(`{"validate": "a", "additionalProperties": 1}`), &v))
	assert.Equal(t, v.ValidateField.Value(), "a")
	assert.Equal(t, v.AdditionalPropertiesField.Value(), int64(1))
	assert.Equal(t, len(v.AdditionalProperties()), 0)

	out, err := json.Marshal(v)
	assert.NilError(t, err)
	assert.Equal(t, string(out), `{"validate":"a","additionalProperties":1}`)
}

// TestConstNextToEnum applies both: no value equals {"a":1} and {"a":2}.
func TestConstNextToEnum(t *testing.T) {
	check(t, nil, map[string]error{
		`{"ce": {"a": 1}}`: validation.ErrConst,
		`{"ce": {"a": 2}}`: validation.ErrConst,
	})
}

// TestUniqueAsJson compares elements that are no Go scalars as JSON
// values: member order and the spelling of a number do not matter.
func TestUniqueAsJson(t *testing.T) {
	check(t,
		[]string{
			`{"prefixUnique": [1, {"a": 1}, {"a": 2}]}`,
			`{"objectUnique": [{"x": 1}, {"x": 2}]}`,
			`{"numberUnique": [1, 2]}`,
		},
		map[string]error{
			`{"prefixUnique": [1, {"a": 1, "b": 2}, {"b": 2, "a": 1}]}`: validation.ErrUnique,
			`{"prefixUnique": [1, [1], [ 1 ]]}`:                         validation.ErrUnique,
			`{"objectUnique": [{"x": 1}, {"x": 1.0}]}`:                  validation.ErrUnique,
			`{"objectUnique": [{"y": [1]}, {"y": [1.0]}]}`:              validation.ErrUnique,
			`{"numberUnique": [1, 1.0]}`:                                validation.ErrUnique,
		},
	)
}

// TestEmptyMemberName checks the member named "" in the validators of raw
// JSON text.
func TestEmptyMemberName(t *testing.T) {
	check(t,
		[]string{
			`{"emptyName": {"": [1, 1]}}`,
			`{"requiresEmpty": {"": 1, "a": 2}}`,
			`{"requiresEmpty": 1}`,
		},
		map[string]error{
			`{"emptyName": {"": [1, 2]}}`:    validation.ErrNot,
			`{"emptyName": 1}`:               validation.ErrNot,
			`{"emptyName": {"unique": [1]}}`: validation.ErrNot,
			`{"requiresEmpty": {"a": 1}}`:    validation.ErrRequired,
			`{"requiresEmpty": {"": 1}}`:     validation.ErrRequired,
		},
	)
}

// TestBoundsBeyondTheGoType checks the bounds no value of the Go type
// reaches: they always or never hold.
func TestBoundsBeyondTheGoType(t *testing.T) {
	check(t,
		[]string{
			`{"maxHuge": 1.7976931348623157e308}`,
			`{"exclusiveHuge": -1.7976931348623157e308}`,
			`{"multipleHuge": 0}`,
			`{"multipleHugeNumber": 0}`,
			`{"multipleTiny": 0.5}`,
			`{"lengthHuge": "abc"}`,
			`{"membersHuge": {"a": 1}}`,
			`{"membersStruct": {"a": 1, "b": 2}}`,
			`{"rawMaxLength": "abc"}`,
			`{"rawContains": [1, 2]}`,
			`{"rawMinContains": "not an array"}`,
		},
		map[string]error{
			`{"minHuge": 9223372036854775807}`: validation.ErrCompare,
			`{"multipleHuge": 1000000000}`:     validation.ErrMultiple,
			`{"multipleHugeNumber": 1e300}`:    validation.ErrMultiple,
			`{"lengthHugeMin": "abc"}`:         validation.ErrLength,
			`{"rawMinContains": [1]}`:          validation.ErrContains,
		},
	)
}

// TestNumberEnumAbove2To53 pins the documented behaviour of a typed number
// enum: the value compares as a float64, which holds 18446744073709551616
// and 18446744073709551617 alike. Raw JSON text compares the numbers
// exactly.
func TestNumberEnumAbove2To53(t *testing.T) {
	check(t,
		[]string{
			`{"typedBig": 18446744073709551617}`,
			`{"typedBig": 18446744073709551616}`,
			`{"rawBig": 18446744073709551617}`,
		},
		map[string]error{
			`{"typedBig": 1}`:                  validation.ErrConst,
			`{"rawBig": 18446744073709551616}`: validation.ErrConst,
		},
	)
}
