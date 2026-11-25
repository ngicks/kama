package subschema

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

func TestTree(t *testing.T) {
	check[Tree](t,
		[]string{`1`, `"a"`, `{"name": "a"}`, `{"name": "a", "children": [{"name": "b"}]}`},
		map[string]error{
			`{}`:                                    validator.ErrRequired,
			`{"name": 1}`:                           validator.ErrKind,
			`{"name": "a", "children": {}}`:         validator.ErrKind,
			`{"name": "a", "children": [{}]}`:       validator.ErrRequired,
			`{"name": "a", "children": [{"x": 1}]}`: validator.ErrRequired,
		},
	)
}

func TestSmall(t *testing.T) {
	// minimum holds on a string, so not fails on it.
	check[Small](t, []string{`4.5`, `-1`}, map[string]error{
		`5`:   validator.ErrNot,
		`"a"`: validator.ErrNot,
	})
}

func TestAddress(t *testing.T) {
	check[Address](t,
		[]string{`{"country": "us", "zip": 1}`, `{"country": "ca", "postal": 1}`, `1`},
		map[string]error{
			// if holds without country, so then requires zip.
			`{"postal": 1}`:                  validator.ErrRequired,
			`{"country": "us"}`:              validator.ErrRequired,
			`{"country": "ca"}`:              validator.ErrRequired,
			`{"country": "mx", "postal": 1}`: validator.ErrConst,
			`{"zip": 1}`:                     validator.ErrRequired,
		},
	)
}

func TestLabels(t *testing.T) {
	check[Labels](t,
		[]string{`{}`, `{"x-a": "s", "n": 1}`},
		map[string]error{
			`{"x-a": 1}`: validator.ErrKind,
			`{"n": "s"}`: validator.ErrKind,
		},
	)

	var v Labels
	assert.Assert(t, json.Unmarshal([]byte(`[]`), &v) != nil)
}

func TestPoint(t *testing.T) {
	check[Point](t,
		[]string{`[]`, `[1]`, `[1, 2.5]`, `[1, 2, "a", "b"]`},
		map[string]error{`["a"]`: validator.ErrKind, `[1, 2, 3]`: validator.ErrKind},
	)
}

func TestTags(t *testing.T) {
	check[Tags](t,
		[]string{`["go"]`, `["go", "c", "go"]`},
		map[string]error{
			`[]`:                 validator.ErrContains,
			`["c"]`:              validator.ErrContains,
			`["go", "go", "go"]`: validator.ErrContains,
		},
	)
}

func TestKeys(t *testing.T) {
	check[Keys](
		t,
		[]string{`{}`, `{"ab": 1}`},
		map[string]error{`{"aB": 1}`: validator.ErrPattern},
	)
}

func TestPayment(t *testing.T) {
	check[Payment](t,
		[]string{`{}`, `{"billing": 1}`, `{"card": 1, "billing": 2}`},
		map[string]error{`{"card": 1}`: validator.ErrRequired},
	)
}

func TestStrict(t *testing.T) {
	check[Strict](t,
		[]string{`{}`, `{"a": 1, "b": "x"}`, `"s"`},
		map[string]error{`{"a": 1, "c": 2}`: validator.ErrOr, `{"a": "1"}`: validator.ErrKind},
	)
}

func TestHolder(t *testing.T) {
	check[Holder](t,
		[]string{`{}`, `{"node": {"v": 1, "kids": [{"kids": []}]}}`, `{"node": {"up": 1}}`},
		map[string]error{
			`{"node": 1}`:                         validator.ErrDecode,
			`{"node": {"kids": [{"v": "x"}]}}`:    validator.ErrDecode,
			`{"node": {"up": {"name": 1}}}`:       validator.ErrDecode,
			`{"node": {"up": {"children": [1]}}}`: validator.ErrDecode,
		},
	)
}

func TestMixed(t *testing.T) {
	check[Mixed](t,
		[]string{`1.0`, `"a"`, `null`, `[1]`},
		map[string]error{`2`: validator.ErrConst, `[1, 1]`: validator.ErrConst},
	)
}
