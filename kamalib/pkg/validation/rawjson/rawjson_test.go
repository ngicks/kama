package rawjson

import (
	"encoding/json/jsontext"
	"errors"
	"regexp"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

type named jsontext.Value

func TestKind(t *testing.T) {
	for _, tc := range []struct {
		v     string
		kinds []string
		ok    bool
	}{
		{`null`, []string{"null"}, true},
		{`1.0`, []string{"integer"}, true},
		{`1e2`, []string{"integer"}, true},
		{`1.5`, []string{"integer"}, false},
		{`1.5`, []string{"number"}, true},
		{`"a"`, []string{"array", "object"}, false},
		{`{}`, []string{"array", "object"}, true},
		{`true`, []string{"boolean"}, true},
	} {
		err := Kind(named(tc.v), tc.kinds...)
		assert.Equal(t, err == nil, tc.ok, "%s %v: %v", tc.v, tc.kinds, err)

		if !tc.ok {
			assert.ErrorIs(t, err, validation.ErrKind)
		}
	}

	assert.ErrorContains(t, Kind(jsontext.Value(`"a"`), "array", "object"),
		"want array or object, got string")
}

func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		v, op, num string
		ok         bool
	}{
		{`3`, ">=", "3", true},
		{`3.0`, ">", "3", false},
		{`2.9999999999999999999`, "<", "3", true},
		{`"a"`, ">", "3", true},
		{`1e20000`, ">", "1e19999", true},
		{`-1e20000`, "<", "0", true},
	} {
		err := Compare(jsontext.Value(tc.v), tc.op, tc.num)
		assert.Equal(t, err == nil, tc.ok, "%s %s %s: %v", tc.v, tc.op, tc.num, err)
	}

	assert.ErrorIs(t, Compare(jsontext.Value(`1`), ">", "1"), validation.ErrCompare)
	assert.ErrorContains(t, Compare(jsontext.Value(`1`), ">", "1"), "want > 1, got 1")
}

func TestMultipleOf(t *testing.T) {
	for _, tc := range []struct {
		v, num string
		ok     bool
	}{
		{`0.0075`, "0.0001", true},
		{`0.00751`, "0.0001", false},
		{`35`, "1.5", false},
		{`4.5`, "1.5", true},
		{`1e308`, "0.123456789", false},
		{`12391239123`, "1e-8", true},
		{`"x"`, "2", true},
	} {
		err := MultipleOf(jsontext.Value(tc.v), tc.num)
		assert.Equal(t, err == nil, tc.ok, "%s %s: %v", tc.v, tc.num, err)
	}

	assert.ErrorIs(t, MultipleOf(jsontext.Value(`3`), "2"), validation.ErrMultiple)
}

func TestLengths(t *testing.T) {
	assert.NilError(t, Runes(jsontext.Value(`"💩💩"`), "<=", 2))
	assert.ErrorIs(t, Runes(jsontext.Value(`"abc"`), "<=", 2), validation.ErrLength)
	assert.NilError(t, Runes(jsontext.Value(`100`), "<=", 2))

	assert.NilError(t, Items(jsontext.Value(`[1, 2]`), "<=", 2))
	assert.ErrorContains(t, Items(jsontext.Value(`[1, 2, 3]`), "<=", 2), "want length <= 2, got 3")
	assert.NilError(t, Items(jsontext.Value(`{"a":1,"b":2,"c":3}`), "<=", 2))

	assert.ErrorContains(t, Members(jsontext.Value(`{"a":1,"b":2,"c":3}`), "<=", 2),
		"want member count <= 2, got 3")
	assert.NilError(t, Members(jsontext.Value(`[1, 2, 3]`), "<=", 2))
}

func TestPattern(t *testing.T) {
	re := regexp.MustCompile("^a+$")
	assert.NilError(t, Pattern(jsontext.Value(`"aa"`), re))
	assert.ErrorIs(t, Pattern(jsontext.Value(`"ab"`), re), validation.ErrPattern)
	assert.NilError(t, Pattern(jsontext.Value(`true`), re))
}

func TestEqual(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		eq   bool
	}{
		{`1`, `1.0`, true},
		{`1`, `true`, false},
		{`0`, `false`, false},
		{`"a"`, `"a"`, true},
		{`{"a":1,"b":[1,2]}`, `{"b":[1.0,2],"a":1}`, true},
		{`{"a":1}`, `{"a":1,"b":2}`, false},
		{`[1,2]`, `[2,1]`, false},
		{`null`, `null`, true},
		{`[]`, `{}`, false},
	} {
		assert.Equal(
			t,
			Equal(jsontext.Value(tc.a), jsontext.Value(tc.b)),
			tc.eq,
			"%s %s",
			tc.a,
			tc.b,
		)
	}
}

func TestUniqueConst(t *testing.T) {
	assert.NilError(t, Unique(jsontext.Value(`[1, "1", [1], {"a":1}]`)))
	assert.ErrorContains(t, Unique(jsontext.Value(`[{"a":1,"b":2}, 0, {"b":2,"a":1}]`)),
		"want unique elements, got equal elements at 0 and 2")
	assert.NilError(t, Unique(jsontext.Value(`"aa"`)))

	assert.NilError(t, Const(jsontext.Value(`1.0`), `"a"`, `1`))
	assert.NilError(t, Const(jsontext.Value(`{"b":null,"a":[]}`), `{"a":[],"b":null}`))

	err := Const(jsontext.Value(`2`), `"a"`, `1`)
	assert.ErrorIs(t, err, validation.ErrConst)
	assert.ErrorContains(t, err, `want one of "a", 1, got 2`)
}

func TestRequired(t *testing.T) {
	v := jsontext.Value(`{"a":null}`)
	assert.NilError(t, Required(v, false, "a"))
	assert.NilError(t, Required(jsontext.Value(`[]`), false, "a"))

	err := Required(v, false, "a", "b", "c")
	assert.ErrorIs(t, err, validation.ErrRequired)
	assert.DeepEqual(t, validation.Pointers(err), []string{"/b", "/c"})
	assert.DeepEqual(t, validation.Pointers(Required(v, true, "b", "c")), []string{"/b"})
	assert.Assert(t, !errors.Is(Required(v, false), validation.ErrRequired))
}
