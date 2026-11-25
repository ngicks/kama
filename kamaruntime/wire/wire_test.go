package wire

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"reflect"
	"testing"

	"github.com/ngicks/kama/kamaruntime/validator"
)

func TestNonNull(t *testing.T) {
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
		{"null", validator.ErrNull},
	} {
		var n Int64

		nilError(t, json.Unmarshal([]byte(tc.in), &n), tc.in)

		_, err := n.Value()
		errorIs(t, err, tc.kind, tc.in)
	}

	var n Int64

	err := json.Unmarshal([]byte(`"1"`), &n)
	_, ok := errors.AsType[*json.SemanticError](err)
	equal(t, ok, true, "%T: %v", err, err)

	var s NonNull[string]

	nilError(t, json.Unmarshal([]byte(`null`), &s))

	_, err = s.Value()
	errorIs(t, err, validator.ErrNull)
}

func TestNonNullSlice(t *testing.T) {
	var s NonNullSlice[string]

	nilError(t, json.Unmarshal([]byte(`["a",null,"c",null]`), &s))

	got, err := s.Value()
	deepEqual(t, got, []string{"a", "", "c", ""})
	errorIs(t, err, validator.ErrNull)
	deepEqual(t, validator.Pointers(err), []string{"/1", "/3"})

	again, _ := s.Value()
	equal(t, &again[0], &got[0])

	nilError(t, json.Unmarshal([]byte(`null`), &s))

	got, err = s.Value()
	equal(t, got == nil, true)
	errorIs(t, err, validator.ErrNull)
	deepEqual(t, validator.Pointers(err), []string{""})

	nilError(t, json.Unmarshal([]byte(`[]`), &s))

	got, err = s.Value()
	nilError(t, err)
	equal(t, got != nil && len(got) == 0, true)

	err = json.Unmarshal([]byte(`{}`), &s)
	_, ok := errors.AsType[*json.SemanticError](err)
	equal(t, ok, true, "%T: %v", err, err)

	var ints NonNullSlice[int64]

	nilError(t, json.Unmarshal([]byte(`[1.0,1.5,1e2,null]`), &ints))

	gotInts, err := ints.Value()
	deepEqual(t, gotInts, []int64{1, 0, 100, 0})
	deepEqual(t, validator.Pointers(err), []string{"/1", "/3"})
}

func TestNonNullMap(t *testing.T) {
	var m NonNullMap[string, int64]

	nilError(t, json.Unmarshal([]byte(`{"a":1,"b":null,"c":2.0,"d":2.5}`), &m))

	got, err := m.Value()
	deepEqual(t, got, map[string]int64{"a": 1, "b": 0, "c": 2, "d": 0})
	deepEqual(t, validator.Pointers(err), []string{"/b", "/d"})

	nilError(t, json.Unmarshal([]byte(`null`), &m))

	_, err = m.Value()
	errorIs(t, err, validator.ErrNull)
}

// holder decodes null itself, as a generated type or an opt.Option does.
type holder struct {
	null bool
}

func (h *holder) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	h.null = dec.PeekKind() == jsontext.KindNull

	return dec.SkipValue()
}

func TestElementDecidesNull(t *testing.T) {
	var hs NonNullSlice[holder]

	nilError(t, json.Unmarshal([]byte(`[null,1]`), &hs))

	got, err := hs.Value()
	nilError(t, err)
	deepEqual(t, got, []holder{{null: true}, {}})

	var raws NonNullSlice[jsontext.Value]

	nilError(t, json.Unmarshal([]byte(`[null,1]`), &raws))

	gotRaws, err := raws.Value()
	nilError(t, err)
	deepEqual(t, gotRaws, []jsontext.Value{jsontext.Value("null"), jsontext.Value("1")})
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

// TestKeepsDecoding decodes past every recorded value, so that an error after them in the
// document is reported as well.
func TestKeepsDecoding(t *testing.T) {
	var v struct {
		A NonNullSlice[string]
		B int
	}

	err := json.Unmarshal([]byte(`{"A":[null,null],"B":"x"}`), &v)
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
	nilError(t, err)
	// The name decodes through the unmarshalers too, as the key of a map does.
	deepEqual(t, got, map[string][]string{"caller": {"caller", "caller"}})

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

	err = json.Unmarshal([]byte(`["a",1]`), &s)
	se, ok := errors.AsType[*json.SemanticError](err)
	equal(t, ok, true, "%T: %v", err, err)
	equal(t, string(se.JSONPointer), "/1")
	equal(t, reflect.TypeFor[string](), se.GoType)
}
