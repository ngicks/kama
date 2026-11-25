package rawjson

import (
	"encoding/json/jsontext"
	"errors"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

type named jsontext.Value

func TestInt64(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"1", 1},
		{"1.0", 1},
		{"-0.0", 0},
		{"1e2", 100},
		{"1.5E+1", 15},
		{"0.001e3", 1},
		{"-12.000", -12},
		{"9007199254740993.0", 9007199254740993},
		{"9223372036854775807.0", 9223372036854775807},
		{"-9223372036854775808", -9223372036854775808},
		{"0e99999999999999999999", 0},
		{"-0.000E-99999999999999999999", 0},
		{"100e-2", 1},
	} {
		got, ok := Int64(tc.in)
		assert.Assert(t, ok, tc.in)
		assert.Equal(t, got, tc.want, tc.in)
	}

	for _, in := range []string{
		"1.5", "1e-1", "0.5e0", "1e400", "9223372036854775808",
		"1e99999999999999999999", "1e-99999999999999999999", "a", "",
	} {
		_, ok := Int64(in)
		assert.Assert(t, !ok, in)
	}
}

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
		{`1e9999999999`, []string{"integer"}, true},
		{`1.5e9999999999`, []string{"integer"}, true},
		{`1e-9999999999`, []string{"integer"}, false},
		{`12345e-4`, []string{"integer"}, false},
		{`12340e-1`, []string{"integer"}, true},
	} {
		err := Kind(named(tc.v), tc.kinds...)
		assert.Equal(t, err == nil, tc.ok, "%s %v: %v", tc.v, tc.kinds, err)

		if !tc.ok {
			assert.ErrorIs(t, err, validator.ErrKind)
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
		{`1e9999999999`, "<=", "10", false},
		{`1e9999999999`, ">", "1e9999999998", true},
		{`-1e9999999999`, "<", "-1e9999999998", true},
		{`1e-9999999999`, ">", "0", true},
		{`1e-9999999999`, "<", "1e-9999999998", true},
		{`-1e-9999999999`, "<", "0", true},
		{`0.000e9999999999`, "==", "0", true},
		{`12.5e-1`, "==", "1.25", true},
		{`1.25`, "==", "125e-2", true},
		{`1.2500000000000000000000000000000000001`, ">", "1.25", true},
		{`99999999999999999999999999999999999999e9999999999`, "<", "1e9999999999999", true},
		{`1`, "==", "not a number", false},
	} {
		err := Compare(jsontext.Value(tc.v), tc.op, tc.num)
		assert.Equal(t, err == nil, tc.ok, "%s %s %s: %v", tc.v, tc.op, tc.num, err)
	}

	assert.ErrorIs(t, Compare(jsontext.Value(`1`), ">", "1"), validator.ErrCompare)
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
		{`1e9999999999`, "7", false},
		{`-1e9999999999`, "7", false},
		{`1e-9999999999`, "7", false},
		{`1e10001`, "7", false},
		{`7e9999999999`, "7", true},
		{`1e9999999999`, "2", true},
		{`1e9999999999`, "1e9999999998", true},
		{`1e9999999998`, "1e9999999999", false},
		{`0`, "1e-9999999999", true},
		{`3e-9999999999`, "1e-9999999999", true},
		{`3e-9999999999`, "2e-9999999999", false},
		{`35e-1`, "0.7", true},
		{`1`, "0", false},
	} {
		err := MultipleOf(jsontext.Value(tc.v), tc.num)
		assert.Equal(t, err == nil, tc.ok, "%s %s: %v", tc.v, tc.num, err)
	}

	assert.ErrorIs(t, MultipleOf(jsontext.Value(`3`), "2"), validator.ErrMultiple)
}

func TestLengths(t *testing.T) {
	assert.NilError(t, Runes(jsontext.Value(`"💩💩"`), "<=", 2))
	assert.ErrorIs(t, Runes(jsontext.Value(`"abc"`), "<=", 2), validator.ErrLength)
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
	assert.ErrorIs(t, Pattern(jsontext.Value(`"ab"`), re), validator.ErrPattern)
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
		{`1e9999999999`, `0`, false},
		{`1e9999999999`, `10e9999999998`, true},
		{`[0, 1e9999999999]`, `[0.0, 1E+9999999999]`, true},
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
	assert.NilError(t, Unique(jsontext.Value(`[0, 1e9999999999]`)))

	assert.NilError(t, Const(jsontext.Value(`1.0`), `"a"`, `1`))
	assert.NilError(t, Const(jsontext.Value(`{"b":null,"a":[]}`), `{"a":[],"b":null}`))

	err := Const(jsontext.Value(`2`), `"a"`, `1`)
	assert.ErrorIs(t, err, validator.ErrConst)
	assert.ErrorContains(t, err, `want one of "a", 1, got 2`)
}

func TestRequired(t *testing.T) {
	v := jsontext.Value(`{"a":null}`)
	assert.NilError(t, Required(v, false, "a"))
	assert.NilError(t, Required(jsontext.Value(`[]`), false, "a"))

	err := Required(v, false, "a", "b", "c")
	assert.ErrorIs(t, err, validator.ErrRequired)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/b", "/c"})
	assert.DeepEqual(t, validator.Pointers(Required(v, true, "b", "c")), []string{"/b"})
	assert.Assert(t, !errors.Is(Required(v, false), validator.ErrRequired))
}

// TestHugeExponentCost pins that the cost of a number grows with its text, not with its
// exponent: a short text must not make the process allocate in proportion to the exponent.
func TestHugeExponentCost(t *testing.T) {
	long := "1" + strings.Repeat("3", 500)

	values := []string{`1e9999999999`, `-1e9999999999`, `1e-9999999999`, long + "e-9999999999"}

	check := func() {
		for _, v := range values {
			_ = Compare(jsontext.Value(v), "<", "1e999999999999999999999")
			_ = MultipleOf(jsontext.Value(v), "7")
			_ = MultipleOf(jsontext.Value(v), long+"e-99999999999")
			_ = Kind(jsontext.Value(v), "integer")
			_ = Equal(jsontext.Value(v), jsontext.Value(`1e9999999998`))
		}

		_ = Unique(jsontext.Value(`[0, 1e9999999999, 1e-9999999999, 1e9999999999]`))
		_ = Const(jsontext.Value(`1e9999999999`), `1e9999999998`)
	}

	start := time.Now()
	allocs := testing.AllocsPerRun(10, check)
	elapsed := time.Since(start)

	var before, after runtime.MemStats

	runtime.ReadMemStats(&before)
	check()
	runtime.ReadMemStats(&after)

	assert.Assert(t, elapsed < time.Second, "11 runs took %s", elapsed)
	assert.Assert(
		t,
		after.TotalAlloc-before.TotalAlloc < 1<<20,
		"one run allocated %d bytes in %.0f allocations",
		after.TotalAlloc-before.TotalAlloc,
		allocs,
	)
}
