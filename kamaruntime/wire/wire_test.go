package wire

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"github.com/ngicks/kama/kamaruntime/validator"
)

func TestInt64(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"1", 1}, {"1.0", 1}, {"1e2", 100}, {"-0", 0},
		{"-9223372036854775808", -9223372036854775808},
	} {
		var n Int64

		nilError(t, json.Unmarshal([]byte(tc.in), &n), tc.in)

		got, err := n.Value()
		nilError(t, err, tc.in)
		equal(t, got, tc.want, tc.in)
	}

	for _, tc := range []struct {
		in   string
		kind error
	}{
		{"1.5", validator.ErrKind},
		{"9223372036854775808", validator.ErrKind},
		{"1e19", validator.ErrKind},
		{`"1"`, validator.ErrKind},
		{`{"a":1}`, validator.ErrKind},
		{"null", validator.ErrNull},
	} {
		var n Int64

		nilError(t, json.Unmarshal([]byte(tc.in), &n), tc.in)

		_, err := n.Value()
		errorIs(t, err, tc.kind, tc.in)
	}
}

func TestScalarKinds(t *testing.T) {
	for _, tc := range []struct {
		in, msg string
		value   func([]byte) error
	}{
		{`1`, "want string, got number 1", func(b []byte) error {
			var s String
			nilError(t, json.Unmarshal(b, &s))
			_, err := s.Value()

			return err
		}},
		{`[1,2]`, "want string, got array", func(b []byte) error {
			var s String
			nilError(t, json.Unmarshal(b, &s))
			_, err := s.Value()

			return err
		}},
		{`"x"`, `want number, got string "x"`, func(b []byte) error {
			var f Float64
			nilError(t, json.Unmarshal(b, &f))
			_, err := f.Value()

			return err
		}},
		{`1e400`, "want number in the range of float64, got 1e400", func(b []byte) error {
			var f Float64
			nilError(t, json.Unmarshal(b, &f))
			_, err := f.Value()

			return err
		}},
		{`{}`, "want boolean, got object", func(b []byte) error {
			var v Bool
			nilError(t, json.Unmarshal(b, &v))
			_, err := v.Value()

			return err
		}},
		{`true`, "want integer, got boolean true", func(b []byte) error {
			var n Int64
			nilError(t, json.Unmarshal(b, &n))
			_, err := n.Value()

			return err
		}},
	} {
		err := tc.value([]byte(tc.in))
		errorIs(t, err, validator.ErrKind, tc.in)
		equal(t, strings.HasSuffix(err.Error(), tc.msg), true, "%s: %v", tc.in, err)
	}

	var s String

	nilError(t, json.Unmarshal([]byte(`"ok"`), &s))

	got, err := s.Value()
	nilError(t, err)
	equal(t, got, "ok")

	var b Bool

	nilError(t, json.Unmarshal([]byte(`null`), &b))

	_, err = b.Value()
	errorIs(t, err, validator.ErrNull)
}

func TestExcerpt(t *testing.T) {
	long := `"` + strings.Repeat("é", 40) + `"`

	var n Int64

	nilError(t, json.Unmarshal([]byte(long), &n))

	_, err := n.Value()
	msg := err.Error()
	equal(t, strings.HasSuffix(msg, "..."), true, msg)
	equal(t, strings.Contains(msg, strings.Repeat("é", 20)), false, msg)
	equal(t, strings.ContainsRune(msg, '�'), false, msg)
}

func TestNonNullSlice(t *testing.T) {
	var s NonNullSlice[string]

	nilError(t, json.Unmarshal([]byte(`["a",null,"c",1,{}]`), &s))

	got, err := s.Value()
	deepEqual(t, got, []string{"a", "", "c", "", ""})
	errorIs(t, err, validator.ErrNull)
	errorIs(t, err, validator.ErrKind)
	deepEqual(t, validator.Pointers(err), []string{"/1", "/3", "/4"})

	again, _ := s.Value()
	equal(t, &again[0], &got[0])

	for _, in := range []string{`null`, `{"a":1}`, `"a"`} {
		nilError(t, json.Unmarshal([]byte(in), &s), in)

		got, err = s.Value()
		equal(t, got == nil, true, in)
		deepEqual(t, validator.Pointers(err), []string{""})
	}

	nilError(t, json.Unmarshal([]byte(`[]`), &s))

	got, err = s.Value()
	nilError(t, err)
	equal(t, got != nil && len(got) == 0, true)

	var ints NonNullSlice[int64]

	nilError(t, json.Unmarshal([]byte(`[1.0,1.5,1e2,null]`), &ints))

	gotInts, err := ints.Value()
	deepEqual(t, gotInts, []int64{1, 0, 100, 0})
	deepEqual(t, validator.Pointers(err), []string{"/1", "/3"})
}

func TestNonNullMap(t *testing.T) {
	var m NonNullMap[string, int64]

	nilError(t, json.Unmarshal([]byte(`{"a":1,"b":null,"c":2.0,"d":2.5,"e":"x"}`), &m))

	got, err := m.Value()
	deepEqual(t, got, map[string]int64{"a": 1, "b": 0, "c": 2, "d": 0, "e": 0})
	deepEqual(t, validator.Pointers(err), []string{"/b", "/d", "/e"})

	for _, in := range []string{`null`, `[1]`} {
		nilError(t, json.Unmarshal([]byte(in), &m), in)

		_, err = m.Value()
		deepEqual(t, validator.Pointers(err), []string{""})
	}

	err = json.Unmarshal([]byte(`{"a":1,"a":2}`), &m)
	_, ok := errors.AsType[*jsontext.SyntacticError](err)
	equal(t, ok, true, "%T: %v", err, err)
}

// checked decodes as a generated type does: it reads its whole value, keeps what it read and
// returns an error of package validator for a value other than 1.
type checked struct {
	raw string
}

func (c *checked) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	raw, err := dec.ReadValue()
	if err != nil {
		return err
	}

	c.raw = string(raw)
	if c.raw != "1" {
		return validator.Field(validator.New(validator.ErrConst, "want 1"), "inner")
	}

	return nil
}

func TestDecoded(t *testing.T) {
	var v NonNullMap[string, Decoded[checked]]

	nilError(t, json.Unmarshal([]byte(`{"a":2,"b":1}`), &v))

	m, err := MapOf[string](Decoded[checked].Value)(v)
	deepEqual(t, m, map[string]checked{"a": {raw: "2"}, "b": {raw: "1"}})
	deepEqual(t, validator.Pointers(err), []string{"/a/inner"})

	var b NonNullSlice[checked]

	nilError(t, json.Unmarshal([]byte(`[1,3]`), &b))

	got, err := b.Value()
	deepEqual(t, got, []checked{{raw: "1"}, {raw: "3"}})
	deepEqual(t, validator.Pointers(err), []string{"/1/inner"})

	var n Decoded[checked]

	nilError(t, json.Unmarshal([]byte(`null`), &n))

	one, err := n.Value()
	equal(t, one.raw, "null")
	errorIs(t, err, validator.ErrConst)
}

// failing returns an error of no package validator after reading its value.
type failing struct{}

func (*failing) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if err := dec.SkipValue(); err != nil {
		return err
	}

	return errors.New("foreign")
}

// early returns an error of package validator before reading its value.
type early struct{}

func (*early) UnmarshalJSONFrom(*jsontext.Decoder) error {
	return validator.New(validator.ErrKind, "early")
}

func TestDecodedFails(t *testing.T) {
	var f Decoded[failing]

	err := json.Unmarshal([]byte(`1`), &f)
	equal(t, err != nil && strings.Contains(err.Error(), "foreign"), true, "%v", err)

	var e NonNullSlice[early]

	err = json.Unmarshal([]byte(`[1]`), &e)
	equal(t, err != nil && strings.Contains(err.Error(), "early"), true, "%v", err)

	var s NonNullSlice[string]

	err = json.Unmarshal([]byte(`["a",`), &s)
	_, ok := errors.AsType[*jsontext.SyntacticError](err)
	equal(t, ok, true, "%T: %v", err, err)
}

func TestNested(t *testing.T) {
	var s NonNullSlice[NonNullSlice[string]]

	nilError(t, json.Unmarshal([]byte(`[["a",null],null,["b"]]`), &s))

	got, err := SliceOf(NonNullSlice[string].Value)(s)
	deepEqual(t, got, [][]string{{"a", ""}, nil, {"b"}})
	deepEqual(t, validator.Pointers(err), []string{"/0/1", "/1"})

	var m NonNullMap[string, NonNullSlice[int64]]

	nilError(t, json.Unmarshal([]byte(`{"k":[1,null],"n":null}`), &m))

	gotMap, err := MapOf[string](NonNullSlice[int64].Value)(m)
	deepEqual(t, gotMap, map[string][]int64{"k": {1, 0}, "n": nil})
	deepEqual(t, validator.Pointers(err), []string{"/k/1", "/n"})
}

func TestMaxErrors(t *testing.T) {
	const n = 3 * MaxErrors

	in := "[" + strings.Repeat(`"x",`, n-1) + `"x"]`

	var s NonNullSlice[int64]

	nilError(t, json.Unmarshal([]byte(in), &s))

	got, err := s.Value()
	equal(t, len(got), n)
	equal(t, len(validator.Under(err, "")), MaxErrors+1)

	omitted := validator.At(err, "")
	equal(t, len(omitted), 1)
	errorIs(t, omitted[0], validator.ErrOmitted)
	equal(t, strings.Contains(omitted[0].Error(), "200 more"), true, "%v", omitted[0])

	var nested NonNullSlice[NonNullSlice[int64]]

	nilError(t, json.Unmarshal([]byte(`[`+in+`]`), &nested))

	_, err = SliceOf(NonNullSlice[int64].Value)(nested)
	equal(t, len(validator.Under(err, "")), MaxErrors+1)
}

// TestKeepsDecoding decodes past every recorded value, so that an error after them in the
// document is reported as well.
func TestKeepsDecoding(t *testing.T) {
	var v struct {
		A NonNullSlice[string]
		B int
	}

	err := json.Unmarshal([]byte(`{"A":[null,2],"B":"x"}`), &v)
	se, ok := errors.AsType[*json.SemanticError](err)
	equal(t, ok, true, "%T: %v", err, err)
	equal(t, string(se.JSONPointer), "/B")

	_, err = v.A.Value()
	deepEqual(t, validator.Pointers(err), []string{"/0", "/1"})
}

func TestCallerOptions(t *testing.T) {
	opts := json.WithUnmarshalers(json.UnmarshalFromFunc(
		func(dec *jsontext.Decoder, s *string) error {
			if _, err := dec.ReadValue(); err != nil {
				return err
			}

			*s = "caller"

			return nil
		},
	))

	var m NonNullMap[string, NonNullSlice[string]]

	nilError(t, json.Unmarshal([]byte(`{"k":["a",1]}`), &m, opts))

	got, err := MapOf[string](NonNullSlice[string].Value)(m)
	// The name decodes through the unmarshalers too, as the key of a map does. A value of
	// another kind is recorded before the unmarshaler sees it.
	deepEqual(t, got, map[string][]string{"caller": {"caller", ""}})
	deepEqual(t, validator.Pointers(err), []string{"/caller/1"})

	// The container reads the slice itself, so an unmarshaler of the slice does not see it.
	slices := json.WithUnmarshalers(json.UnmarshalFromFunc(
		func(dec *jsontext.Decoder, s *[]string) error {
			*s = []string{"slice"}
			return dec.SkipValue()
		},
	))

	var s NonNullSlice[string]

	nilError(t, json.Unmarshal([]byte(`["a"]`), &s, slices))

	gotSlice, err := s.Value()
	nilError(t, err)
	deepEqual(t, gotSlice, []string{"a"})

	points := 0
	caller := json.WithUnmarshalers(json.UnmarshalFromFunc(
		func(dec *jsontext.Decoder, c *checked) error {
			points++
			c.raw = "caller"

			return dec.SkipValue()
		},
	))

	var d Decoded[checked]

	nilError(t, json.Unmarshal([]byte(`5`), &d, caller))

	gotChecked, err := d.Value()
	nilError(t, err)
	equal(t, gotChecked.raw, "caller")
	equal(t, points, 1)
}
