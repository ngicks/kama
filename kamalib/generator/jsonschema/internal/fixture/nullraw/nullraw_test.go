package nullraw

import (
	"encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

// TestNullReachesTheValidators decodes null into properties whose schema
// admits null by its type and rejects it by another keyword. The Go type
// of each holds null itself, so its validators reject it.
func TestNullReachesTheValidators(t *testing.T) {
	ok := []string{
		`{"r": 1}`,
		`{"r": 1, "a": "s", "c": "x", "e": "s", "p": null, "q": "z", "l": ["a"], "m": {"k": "v"}}`,
		`{"r": 1, "s": "a", "d": null}`,
		`{"r": 1, "d": -1}`,
	}
	for _, text := range ok {
		var v Root
		assert.NilError(t, json.Unmarshal([]byte(text), &v), text)
	}

	bad := map[string]error{
		`{}`:                         validation.ErrRequired,
		`{"r": null}`:                validation.ErrNot,
		`{"r": 1, "a": null}`:        validation.ErrNot,
		`{"r": 1, "c": null}`:        validation.ErrConst,
		`{"r": 1, "c": "y"}`:         validation.ErrConst,
		`{"r": 1, "e": null}`:        validation.ErrOr,
		`{"r": 1, "s": null}`:        validation.ErrKind,
		`{"r": 1, "q": null}`:        validation.ErrNot,
		`{"r": 1, "l": [null]}`:      validation.ErrNot,
		`{"r": 1, "m": {"k": null}}`: validation.ErrNot,
	}
	for text, kind := range bad {
		var v Root
		assert.ErrorIs(t, json.Unmarshal([]byte(text), &v), kind, text)
	}
}

// TestAbsentAndNull keeps an absent optional member apart from null.
func TestAbsentAndNull(t *testing.T) {
	var v Root
	assert.NilError(t, json.Unmarshal([]byte(`{"r": 2}`), &v))
	assert.Assert(t, v.A.IsNone())
	assert.Equal(t, string(v.R), "2")
}
