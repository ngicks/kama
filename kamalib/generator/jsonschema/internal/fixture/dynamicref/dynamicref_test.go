package dynamicref

import (
	"encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

// check decodes each instance of ok into a T and each of bad into a T,
// which must fail with the error kind given.
func check[T any](t *testing.T, ok []string, bad map[string]error) {
	t.Helper()

	for _, text := range ok {
		var v T
		assert.NilError(t, json.Unmarshal([]byte(text), &v), text)
	}

	for text, kind := range bad {
		var v T
		assert.ErrorIs(t, json.Unmarshal([]byte(text), &v), kind, text)
	}
}

func TestStrictTree(t *testing.T) {
	// The $dynamicRef of tree resolves to the root, so every child is
	// strict too.
	check[StrictTree](t,
		[]string{`{}`, `{"data": 1, "children": [{"children": [{"data": "x"}]}]}`},
		map[string]error{
			`{"daat": 1}`:                                 validator.ErrOr,
			`{"children": [{"daat": 1}]}`:                 validator.ErrOr,
			`{"children": [{"children": [{"daat": 1}]}]}`: validator.ErrOr,
			`{"children": {}}`:                            validator.ErrKind,
		},
	)
}

func TestNames(t *testing.T) {
	// names declares the $dynamicAnchor item, the outermost one its
	// reference to list enters.
	check[Names](t, []string{`[]`, `["a", "b"]`}, map[string]error{
		`[1]`: validator.ErrKind,
		`{}`:  validator.ErrKind,
	})

	var items []ListItem
	assert.NilError(t, json.Unmarshal([]byte(`[1, "a", null]`), &items))
}
