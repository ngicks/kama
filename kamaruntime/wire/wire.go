// Package wire holds types that decode a JSON value leniently: they record what a schema
// rejects of a value of a built-in Go type and go on reading, so that one decoding reports
// every such problem of an object together with the path of each.
//
// A type of the package records, instead of failing:
//   - a value of a JSON kind its Go type cannot hold, such as a number for a [String] or an
//     object for a [NonNullSlice], which it skips without keeping it;
//   - a null, which encoding/json/v2 decodes into the zero value of a string, a number, a
//     boolean, a slice or a map;
//   - a number that is no integer in the range of int64 for an [Int64], such as 1.5, and one
//     out of the range of float64 for a [Float64].
//
// The Value method of a type returns the decoded value as it is, with no copy, and an error of
// package github.com/ngicks/kama/kamaruntime/validator for every value recorded, located by
// validator.Index and validator.Key below the value. A value whose decoding recorded a problem
// holds what decoded of it: the zero value for a scalar and the elements that decoded for a
// container. That value is never to be trusted as valid.
//
// # What fails
//
// The UnmarshalJSONFrom methods return an error where reading cannot go on: a syntax error, a
// truncated input, invalid UTF-8 or a duplicate member name the options of the decoder reject,
// and an error of the reader. A container returns as well the error json/v2 returns for an
// element of a type other than the built-in ones, such as the error of a type with its own
// UnmarshalJSONFrom method or of an unmarshaler the caller passes. Such an error stops the
// container, as encoding/json/v2 stops a slice or a map, and json/v2 locates it by the JSON
// pointer of the element in a json.SemanticError.
//
// # Bounded errors
//
// A container keeps at most [MaxErrors] errors of its elements, recorded or found at
// conversion, and reports how many more it left out by one error wrapping
// validator.ErrOmitted. A document of a million wrong elements thus holds no million errors.
// Each container bounds its own errors, so nested containers hold at most MaxErrors errors per
// level of nesting each. A value of the wrong kind is described by its kind and, for a scalar,
// by the first bytes of its text, never by the whole value.
//
// # Options of the caller
//
// The types decode every value they hold through the decoder they are given, so the options of
// the caller of json.Unmarshal or json.UnmarshalDecode, its unmarshalers included, apply to the
// values they hold and to every value nested in those. The exceptions are the values the types
// read themselves: an unmarshaler for *int64 or *float64 does not see a number they hold, one
// for the slice or the map a container holds does not see that slice or map, and a value of a
// kind its Go type cannot hold or a null is recorded before any unmarshaler sees it. An
// unmarshaler for *string or *bool sees a string or a boolean. The keys of a NonNullMap decode
// through the unmarshalers, as the keys of a map do.
//
// The types decode the values they hold with the errors json/v2 reports, whatever the caller
// passes: under the legacy semantics of json v1, json/v2 would read every value whole once
// more per level of nesting before decoding it.
package wire

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"strconv"
	"unicode/utf8"

	jsonv1 "encoding/json"

	"github.com/ngicks/kama/kamaruntime/internal/decimal"
	"github.com/ngicks/kama/kamaruntime/validator"
)

// MaxErrors is the number of errors of its elements a container keeps.
const MaxErrors = 100

// String is a string decoded from a JSON string. It records null and a value of another kind.
type String struct {
	value   string
	problem error
}

// UnmarshalJSONFrom decodes the next value of dec into s.
func (s *String) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var err error

	*s = String{}
	s.problem, err = readString(dec, &s.value)

	return err
}

// Value returns the decoded string and the error of the value recorded while decoding, if any.
func (s String) Value() (string, error) {
	return s.value, s.problem
}

// Float64 is a float64 decoded from a JSON number. It records null, a value of another kind
// and a number out of the range of float64.
type Float64 struct {
	value   float64
	problem error
}

// UnmarshalJSONFrom decodes the next value of dec into f.
func (f *Float64) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var err error

	*f = Float64{}
	f.problem, err = readFloat64(dec, &f.value)

	return err
}

// Value returns the decoded number and the error of the value recorded while decoding, if any.
func (f Float64) Value() (float64, error) {
	return f.value, f.problem
}

// Bool is a bool decoded from a JSON boolean. It records null and a value of another kind.
type Bool struct {
	value   bool
	problem error
}

// UnmarshalJSONFrom decodes the next value of dec into b.
func (b *Bool) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var err error

	*b = Bool{}
	b.problem, err = readBool(dec, &b.value)

	return err
}

// Value returns the decoded boolean and the error of the value recorded while decoding, if
// any.
func (b Bool) Value() (bool, error) {
	return b.value, b.problem
}

// Int64 is an int64 decoded from any JSON number of an integer value, such as 1.0 or 1e2, as
// JSON Schema calls such a number an integer. It records null, a value of another kind, a
// fraction and a value out of the range of int64.
type Int64 struct {
	value   int64
	problem error
}

// UnmarshalJSONFrom decodes the next value of dec into n.
func (n *Int64) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var err error

	*n = Int64{}
	n.problem, err = readInt64(dec, &n.value)

	return err
}

// Value returns the decoded integer and the error of the value recorded while decoding, if
// any.
func (n Int64) Value() (int64, error) {
	return n.value, n.problem
}

// NonNullSlice is a slice decoded from a JSON array that must not be null. It decodes the
// elements one by one from the decoder and records the problem of each, keeping its index: an
// element of a built-in type decodes as the type of the package for it does, and an element of
// another type decodes through json/v2, whose error stops the slice. T is string, float64,
// bool, int64, jsontext.Value, which holds any value, or a type with its own UnmarshalJSONFrom
// method. The JSON kinds a built-in element accepts follow from T by a type switch over a
// pointer to the element, which involves no reflection.
type NonNullSlice[T any] struct {
	values  []T
	problem error
	errs    errorList
}

// UnmarshalJSONFrom decodes the next value of dec into s.
func (s *NonNullSlice[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var err error

	*s = NonNullSlice[T]{}

	if s.problem, err = begin(dec, jsontext.KindBeginArray, "array"); s.problem != nil ||
		err != nil {
		return err
	}

	s.values = []T{}

	for dec.PeekKind() != jsontext.KindEndArray {
		var v T

		problem, err := decodeValue(dec, &v)
		if err != nil {
			return err
		}

		if problem != nil {
			s.errs.add(validator.Index(problem, len(s.values)))
		}

		s.values = append(s.values, v)
	}

	_, err = dec.ReadToken()

	return err
}

// Value returns the decoded slice as it is and the errors of the values recorded while
// decoding. A value that is no array gives nil.
func (s NonNullSlice[T]) Value() ([]T, error) {
	if s.problem != nil {
		return nil, s.problem
	}

	return s.values, s.errs.err()
}

// NonNullMap is a map decoded from a JSON object that must not be null. It decodes the members
// one by one from the decoder, as NonNullSlice decodes its elements, and records the problem
// of each, keeping its name. K is a string type, as JSON names members by strings.
type NonNullMap[K ~string, V any] struct {
	values  map[K]V
	problem error
	errs    errorList
}

// UnmarshalJSONFrom decodes the next value of dec into m.
func (m *NonNullMap[K, V]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var err error

	*m = NonNullMap[K, V]{}

	if m.problem, err = begin(dec, jsontext.KindBeginObject, "object"); m.problem != nil ||
		err != nil {
		return err
	}

	m.values = map[K]V{}

	for dec.PeekKind() != jsontext.KindEndObject {
		// The name decodes as encoding/json/v2 decodes the key of a map, through the
		// unmarshalers of the caller.
		var k K
		if err := json.UnmarshalDecode(dec, &k, legacyOff); err != nil {
			return err
		}

		var v V

		problem, err := decodeValue(dec, &v)
		if err != nil {
			return err
		}

		if problem != nil {
			m.errs.add(validator.Key(problem, k))
		}

		m.values[k] = v
	}

	_, err = dec.ReadToken()

	return err
}

// Value returns the decoded map as it is and the errors of the values recorded while decoding.
// A value that is no object gives nil.
func (m NonNullMap[K, V]) Value() (map[K]V, error) {
	if m.problem != nil {
		return nil, m.problem
	}

	return m.values, m.errs.err()
}

// SliceOf returns a function converting a NonNullSlice of W into a slice of T by conv, which
// converts each element. Unlike Value, it allocates the slice it returns. It reports the errors
// of s and of every element, at most MaxErrors of them.
func SliceOf[W, T any](conv func(W) (T, error)) func(NonNullSlice[W]) ([]T, error) {
	return func(s NonNullSlice[W]) ([]T, error) {
		if s.problem != nil {
			return nil, s.problem
		}

		errs := s.errs.clone()
		out := make([]T, len(s.values))

		for i, v := range s.values {
			var err error

			if out[i], err = conv(v); err != nil {
				errs.add(validator.Index(err, i))
			}
		}

		return out, errs.err()
	}
}

// MapOf returns a function converting a NonNullMap of W into a map of T by conv, which converts
// each value. Unlike Value, it allocates the map it returns. It reports the errors of m and of
// every value, at most MaxErrors of them. The values convert in the order of the map, so those
// it leaves out are any.
func MapOf[K ~string, W, T any](
	conv func(W) (T, error),
) func(NonNullMap[K, W]) (map[K]T, error) {
	return func(m NonNullMap[K, W]) (map[K]T, error) {
		if m.problem != nil {
			return nil, m.problem
		}

		errs := m.errs.clone()
		out := make(map[K]T, len(m.values))

		for k, v := range m.values {
			var err error

			if out[k], err = conv(v); err != nil {
				errs.add(validator.Key(err, k))
			}
		}

		return out, errs.err()
	}
}

// legacyOff keeps json/v2 from validating every nested value whole before decoding it, as it
// does under the legacy semantics of json v1. Options hold no state, so the value is shared.
var legacyOff = jsonv1.ReportErrorsWithLegacySemantics(false)

// maxExcerpt is the number of bytes of the text of a scalar of the wrong kind an error quotes.
const maxExcerpt = 32

// decodeValue decodes the next value of dec into p as the type of the package for the
// built-in type T does, and any other T through json/v2.
func decodeValue[T any](dec *jsontext.Decoder, p *T) (problem, err error) {
	switch q := any(p).(type) {
	case *string:
		return readString(dec, q)
	case *float64:
		return readFloat64(dec, q)
	case *bool:
		return readBool(dec, q)
	case *int64:
		return readInt64(dec, q)
	}

	return nil, json.UnmarshalDecode(dec, p, legacyOff)
}

func readString(dec *jsontext.Decoder, p *string) (problem, err error) {
	if dec.PeekKind() != jsontext.KindString {
		return mismatch(dec, "string")
	}

	// json/v2 fails a string decoding only on input it cannot read past or for an
	// unmarshaler of the caller, which fail the decoding either way.
	return nil, json.UnmarshalDecode(dec, p)
}

func readBool(dec *jsontext.Decoder, p *bool) (problem, err error) {
	if k := dec.PeekKind(); k != jsontext.KindTrue && k != jsontext.KindFalse {
		return mismatch(dec, "boolean")
	}

	return nil, json.UnmarshalDecode(dec, p)
}

func readFloat64(dec *jsontext.Decoder, p *float64) (problem, err error) {
	if dec.PeekKind() != jsontext.KindNumber {
		return mismatch(dec, "number")
	}

	raw, err := dec.ReadValue()
	if err != nil {
		return nil, err
	}

	v, err := strconv.ParseFloat(string(raw), 64)
	if err != nil {
		return validator.New(
			validator.ErrKind, "want number in the range of float64, got %s", excerpt(raw),
		), nil
	}

	*p = v

	return nil, nil
}

func readInt64(dec *jsontext.Decoder, p *int64) (problem, err error) {
	if dec.PeekKind() != jsontext.KindNumber {
		return mismatch(dec, "integer")
	}

	raw, err := dec.ReadValue()
	if err != nil {
		return nil, err
	}

	// Most integers are written plainly, which ParseInt reads without the exact
	// decimal arithmetic a fraction or an exponent needs.
	if v, err := strconv.ParseInt(string(raw), 10, 64); err == nil {
		*p = v
		return nil, nil
	}

	v, ok := decimal.Int64(string(raw))
	if !ok {
		return validator.New(
			validator.ErrKind, "want integer in the range of int64, got %s", excerpt(raw),
		), nil
	}

	*p = v

	return nil, nil
}

// begin reads the token starting a value of kind k, an array or an object named want. A value
// of another kind is skipped and returned as problem.
func begin(dec *jsontext.Decoder, k jsontext.Kind, want string) (problem, err error) {
	if dec.PeekKind() != k {
		return mismatch(dec, want)
	}

	_, err = dec.ReadToken()

	return nil, err
}

// mismatch skips the next value of dec, which is not of the kind named want, and returns it
// as problem: a null wraps validator.ErrNull and any other value validator.ErrKind. An array or
// an object is skipped without being kept, as it may be of any size, while the error quotes
// the first bytes of the text of a scalar.
func mismatch(dec *jsontext.Decoder, want string) (problem, err error) {
	switch k := dec.PeekKind(); k {
	case jsontext.KindNull:
		return errNull(), dec.SkipValue()
	case jsontext.KindBeginArray, jsontext.KindBeginObject:
		if err := dec.SkipValue(); err != nil {
			return nil, err
		}

		return validator.New(validator.ErrKind, "want %s, got %s", want, kindName(k)), nil
	default:
		raw, err := dec.ReadValue()
		if err != nil {
			return nil, err
		}

		return validator.New(
			validator.ErrKind, "want %s, got %s %s", want, kindName(k), excerpt(raw),
		), nil
	}
}

// excerpt returns at most maxExcerpt bytes of raw, cut at a rune boundary and marked when
// cut.
func excerpt(raw jsontext.Value) string {
	if len(raw) <= maxExcerpt {
		return string(raw)
	}

	n := maxExcerpt
	for n > 0 && !utf8.RuneStart(raw[n]) {
		n--
	}

	return string(raw[:n]) + "..."
}

func kindName(k jsontext.Kind) string {
	switch k {
	case jsontext.KindNull:
		return "null"
	case jsontext.KindTrue, jsontext.KindFalse:
		return "boolean"
	case jsontext.KindString:
		return "string"
	case jsontext.KindNumber:
		return "number"
	case jsontext.KindBeginObject:
		return "object"
	case jsontext.KindBeginArray:
		return "array"
	}

	return "invalid"
}

func errNull() error {
	return validator.New(validator.ErrNull, "value is null")
}

// errorList keeps the first MaxErrors errors added to it and counts the others.
type errorList struct {
	errs    []error
	omitted int
}

func (l *errorList) add(err error) {
	switch {
	case err == nil:
	case len(l.errs) < MaxErrors:
		l.errs = append(l.errs, err)
	default:
		l.omitted++
	}
}

// clone returns a copy of l that adds to a slice of its own.
func (l errorList) clone() errorList {
	l.errs = append([]error(nil), l.errs...)
	return l
}

func (l errorList) err() error {
	if l.omitted == 0 {
		return validator.Join(l.errs...)
	}

	return validator.Join(append(slices.Clip(l.errs), validator.New(
		validator.ErrOmitted, "%d more errors of elements not reported", l.omitted,
	))...)
}
