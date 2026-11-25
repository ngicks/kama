package rawroot

import (
	"encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
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
		`0.5`:                      validator.ErrCompare,
		`"abcd"`:                   validator.ErrLength,
		`[]`:                       validator.ErrLength,
		`[1, 1.0]`:                 validator.ErrUnique,
		`{"x": 1, "y": 2, "z": 3}`: validator.ErrLength,
		`{"a": 1}`:                 validator.ErrRequired,
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
		assert.ErrorIs(t, json.Unmarshal([]byte(bad), &v), validator.ErrConst, bad)
	}
}

func TestPair(t *testing.T) {
	for _, ok := range []string{`[1, 2]`, `{"a": 1, "b": 2, "c": 3}`} {
		var v Pair
		assert.NilError(t, json.Unmarshal([]byte(ok), &v), ok)
	}

	var v Pair
	assert.ErrorIs(t, json.Unmarshal([]byte(`"a"`), &v), validator.ErrKind)
	assert.ErrorIs(t, json.Unmarshal([]byte(`[1, 2, 3]`), &v), validator.ErrLength)
}
