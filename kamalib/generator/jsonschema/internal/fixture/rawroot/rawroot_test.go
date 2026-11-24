package rawroot

import (
	"encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

func TestLoose(t *testing.T) {
	for _, ok := range []string{
		`1`, `1.5`, `"abc"`, `null`, `true`, `[1, "1"]`, `{}`, `{"a": 1, "b": 2}`, `{"b": 1}`,
	} {
		var v Loose
		assert.NilError(t, json.Unmarshal([]byte(ok), &v), ok)
		assert.Equal(t, string(v), ok)
	}

	for text, kind := range map[string]error{
		`0.5`:                      validation.ErrCompare,
		`"abcd"`:                   validation.ErrLength,
		`[]`:                       validation.ErrLength,
		`[1, 1.0]`:                 validation.ErrUnique,
		`{"x": 1, "y": 2, "z": 3}`: validation.ErrLength,
		`{"a": 1}`:                 validation.ErrRequired,
	} {
		var v Loose
		assert.ErrorIs(t, json.Unmarshal([]byte(text), &v), kind, text)
	}
}

func TestMixed(t *testing.T) {
	for _, ok := range []string{`1.0`, `"a"`, `null`, `{"b": []}`} {
		var v Mixed
		assert.NilError(t, json.Unmarshal([]byte(ok), &v), ok)
	}

	for _, bad := range []string{`2`, `"b"`, `{"b": [1]}`, `false`} {
		var v Mixed
		assert.ErrorIs(t, json.Unmarshal([]byte(bad), &v), validation.ErrConst, bad)
	}
}

func TestPair(t *testing.T) {
	for _, ok := range []string{`[1, 2]`, `{"a": 1, "b": 2, "c": 3}`} {
		var v Pair
		assert.NilError(t, json.Unmarshal([]byte(ok), &v), ok)
	}

	var v Pair
	assert.ErrorIs(t, json.Unmarshal([]byte(`"a"`), &v), validation.ErrKind)
	assert.ErrorIs(t, json.Unmarshal([]byte(`[1, 2, 3]`), &v), validation.ErrLength)
}
