package subschema

import (
	"encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
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
			`{}`:                                    validation.ErrRequired,
			`{"name": 1}`:                           validation.ErrKind,
			`{"name": "a", "children": {}}`:         validation.ErrKind,
			`{"name": "a", "children": [{}]}`:       validation.ErrRequired,
			`{"name": "a", "children": [{"x": 1}]}`: validation.ErrRequired,
		},
	)
}

func TestSmall(t *testing.T) {
	// minimum holds on a string, so not fails on it.
	check[Small](t, []string{`4.5`, `-1`}, map[string]error{
		`5`:   validation.ErrNot,
		`"a"`: validation.ErrNot,
	})
}

func TestAddress(t *testing.T) {
	check[Address](t,
		[]string{`{"country": "us", "zip": 1}`, `{"country": "ca", "postal": 1}`, `1`},
		map[string]error{
			// if holds without country, so then requires zip.
			`{"postal": 1}`:                  validation.ErrRequired,
			`{"country": "us"}`:              validation.ErrRequired,
			`{"country": "ca"}`:              validation.ErrRequired,
			`{"country": "mx", "postal": 1}`: validation.ErrConst,
			`{"zip": 1}`:                     validation.ErrRequired,
		},
	)
}

func TestLabels(t *testing.T) {
	check[Labels](t,
		[]string{`{}`, `{"x-a": "s", "n": 1}`},
		map[string]error{
			`{"x-a": 1}`: validation.ErrKind,
			`{"n": "s"}`: validation.ErrKind,
			`[]`:         validation.ErrKind,
		},
	)
}

func TestPoint(t *testing.T) {
	check[Point](t,
		[]string{`[]`, `[1]`, `[1, 2.5]`, `[1, 2, "a", "b"]`},
		map[string]error{`["a"]`: validation.ErrKind, `[1, 2, 3]`: validation.ErrKind},
	)
}

func TestTags(t *testing.T) {
	check[Tags](t,
		[]string{`["go"]`, `["go", "c", "go"]`},
		map[string]error{
			`[]`:                 validation.ErrContains,
			`["c"]`:              validation.ErrContains,
			`["go", "go", "go"]`: validation.ErrContains,
		},
	)
}

func TestKeys(t *testing.T) {
	check[Keys](
		t,
		[]string{`{}`, `{"ab": 1}`},
		map[string]error{`{"aB": 1}`: validation.ErrPattern},
	)
}

func TestPayment(t *testing.T) {
	check[Payment](t,
		[]string{`{}`, `{"billing": 1}`, `{"card": 1, "billing": 2}`},
		map[string]error{`{"card": 1}`: validation.ErrRequired},
	)
}

func TestStrict(t *testing.T) {
	check[Strict](t,
		[]string{`{}`, `{"a": 1, "b": "x"}`, `"s"`},
		map[string]error{`{"a": 1, "c": 2}`: validation.ErrOr, `{"a": "1"}`: validation.ErrKind},
	)
}

func TestHolder(t *testing.T) {
	check[Holder](t,
		[]string{`{}`, `{"node": {"v": 1, "kids": [{"kids": []}]}}`, `{"node": {"up": 1}}`},
		map[string]error{
			`{"node": 1}`:                         validation.ErrDecode,
			`{"node": {"kids": [{"v": "x"}]}}`:    validation.ErrDecode,
			`{"node": {"up": {"name": 1}}}`:       validation.ErrDecode,
			`{"node": {"up": {"children": [1]}}}`: validation.ErrDecode,
		},
	)
}

func TestMixed(t *testing.T) {
	check[Mixed](t,
		[]string{`1.0`, `"a"`, `null`, `[1]`},
		map[string]error{`2`: validation.ErrConst, `[1, 1]`: validation.ErrConst},
	)
}
