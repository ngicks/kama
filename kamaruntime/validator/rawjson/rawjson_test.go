package rawjson

import (
	"encoding/json/jsontext"
	"errors"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ngicks/kama/kamaruntime/validator"
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
		{`1e9999999999`, []string{"integer"}, true},
		{`1.5e9999999999`, []string{"integer"}, true},
		{`1e-9999999999`, []string{"integer"}, false},
		{`12345e-4`, []string{"integer"}, false},
		{`12340e-1`, []string{"integer"}, true},
	} {
		err := Kind(named(tc.v), tc.kinds...)
		equal(t, err == nil, tc.ok, "%s %v: %v", tc.v, tc.kinds, err)

		if !tc.ok {
			errorIs(t, err, validator.ErrKind)
		}
	}

	errorContains(t, Kind(jsontext.Value(`"a"`), "array", "object"),
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
		equal(t, err == nil, tc.ok, "%s %s %s: %v", tc.v, tc.op, tc.num, err)
	}

	errorIs(t, Compare(jsontext.Value(`1`), ">", "1"), validator.ErrCompare)
	errorContains(t, Compare(jsontext.Value(`1`), ">", "1"), "want > 1, got 1")
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
		equal(t, err == nil, tc.ok, "%s %s: %v", tc.v, tc.num, err)
	}

	errorIs(t, MultipleOf(jsontext.Value(`3`), "2"), validator.ErrMultiple)
}

func TestLengths(t *testing.T) {
	nilError(t, Runes(jsontext.Value(`"💩💩"`), "<=", 2))
	errorIs(t, Runes(jsontext.Value(`"abc"`), "<=", 2), validator.ErrLength)
	nilError(t, Runes(jsontext.Value(`100`), "<=", 2))

	nilError(t, Items(jsontext.Value(`[1, 2]`), "<=", 2))
	errorContains(t, Items(jsontext.Value(`[1, 2, 3]`), "<=", 2), "want length <= 2, got 3")
	nilError(t, Items(jsontext.Value(`{"a":1,"b":2,"c":3}`), "<=", 2))

	errorContains(t, Members(jsontext.Value(`{"a":1,"b":2,"c":3}`), "<=", 2),
		"want member count <= 2, got 3")
	nilError(t, Members(jsontext.Value(`[1, 2, 3]`), "<=", 2))
}

func TestPattern(t *testing.T) {
	re := regexp.MustCompile("^a+$")
	nilError(t, Pattern(jsontext.Value(`"aa"`), re))
	errorIs(t, Pattern(jsontext.Value(`"ab"`), re), validator.ErrPattern)
	nilError(t, Pattern(jsontext.Value(`true`), re))
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
		equal(
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
	nilError(t, Unique(jsontext.Value(`[1, "1", [1], {"a":1}]`)))
	errorContains(t, Unique(jsontext.Value(`[{"a":1,"b":2}, 0, {"b":2,"a":1}]`)),
		"want unique elements, got equal elements at 0 and 2")
	nilError(t, Unique(jsontext.Value(`"aa"`)))
	nilError(t, Unique(jsontext.Value(`[0, 1e9999999999]`)))

	nilError(t, Const(jsontext.Value(`1.0`), `"a"`, `1`))
	nilError(t, Const(jsontext.Value(`{"b":null,"a":[]}`), `{"a":[],"b":null}`))

	err := Const(jsontext.Value(`2`), `"a"`, `1`)
	errorIs(t, err, validator.ErrConst)
	errorContains(t, err, `want one of "a", 1, got 2`)
}

func TestRequired(t *testing.T) {
	v := jsontext.Value(`{"a":null}`)
	nilError(t, Required(v, false, "a"))
	nilError(t, Required(jsontext.Value(`[]`), false, "a"))

	err := Required(v, false, "a", "b", "c")
	errorIs(t, err, validator.ErrRequired)
	deepEqual(t, validator.Pointers(err), []string{"/b", "/c"})
	deepEqual(t, validator.Pointers(Required(v, true, "b", "c")), []string{"/b"})
	assertTrue(t, !errors.Is(Required(v, false), validator.ErrRequired))
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

	assertTrue(t, elapsed < time.Second, "11 runs took %s", elapsed)
	assertTrue(
		t,
		after.TotalAlloc-before.TotalAlloc < 1<<20,
		"one run allocated %d bytes in %.0f allocations",
		after.TotalAlloc-before.TotalAlloc,
		allocs,
	)
}
