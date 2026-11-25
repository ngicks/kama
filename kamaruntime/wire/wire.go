// Package wire holds types that decode a JSON value leniently and report at conversion what a
// schema rejects but the plain Go type cannot tell: a null, which encoding/json/v2 decodes into
// the zero value of a string, a number, a boolean, a slice or a map, and a number such as 1.0 or
// 1e2 that decodes into an int64 only when it is read as an integer.
//
// A type of the package records such a value while decoding and goes on, so that a decoder
// reads past it and reports it together with the other errors of its document. Its Value method
// returns the decoded value as it is, with no copy, and an error of package
// github.com/ngicks/kama/kamaruntime/validator for every value recorded, located by
// validator.Index and validator.Key below the value. A value the Go type cannot hold at all,
// such as a string for a number, fails the decoding as encoding/json/v2 fails it.
//
// The types decode every value they hold through the decoder they are given, so the options of
// the caller of json.Unmarshal or json.UnmarshalDecode, its unmarshalers included, apply to the
// values they hold and to every value nested in those. The exceptions are the values the types
// read themselves: an unmarshaler for *int64 does not see an int64 they hold, one for the slice
// or the map a container holds does not see that slice or map, and a null is recorded before
// any unmarshaler sees it. The keys of a NonNullMap decode through the unmarshalers, as the keys
// of a map do.
//
// A value of a type that has its own UnmarshalJSONFrom method, such as a generated struct or an
// opt.Option of package github.com/ngicks/option/opt, and raw JSON text in a jsontext.Value
// decide about null themselves, so the containers record no null for them.
package wire

import (
	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/ngicks/kama/kamaruntime/internal/decimal"
	"github.com/ngicks/kama/kamaruntime/validator"
)

// NonNull is a value that must not be null. A null or, for an int64, a number that is no
// integer in the range of int64 decodes without failing and is reported by Value.
type NonNull[T any] struct {
	value   T
	problem error
}

// Int64 is an int64 decoded from any JSON number of an integer value, such as 1.0 or 1e2, as
// JSON Schema calls such a number an integer. It records null, a fraction and a value out of the
// range of int64, which Value reports.
type Int64 = NonNull[int64]

// UnmarshalJSONFrom decodes the next value of dec into n.
func (n *NonNull[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var err error

	*n = NonNull[T]{}
	n.problem, err = decodeValue(dec, &n.value)

	return err
}

// Value returns the decoded value and the error of the value recorded while decoding, if any.
func (n NonNull[T]) Value() (T, error) {
	return n.value, n.problem
}

// NonNullSlice is a slice that must not be null, nor hold null where its element type does not
// decide about null itself. It decodes the elements one by one from the decoder. A null element
// keeps its index and holds the zero value of T.
type NonNullSlice[T any] struct {
	values []T
	null   bool
	errs   []error
}

// UnmarshalJSONFrom decodes the next value of dec into s.
func (s *NonNullSlice[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	*s = NonNullSlice[T]{}

	switch dec.PeekKind() {
	case jsontext.KindNull:
		s.null = true
		return dec.SkipValue()
	case jsontext.KindBeginArray:
	default:
		// The slice reports a value of another kind as encoding/json/v2 does.
		return json.UnmarshalDecode(dec, &s.values)
	}

	if _, err := dec.ReadToken(); err != nil {
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
			s.errs = append(s.errs, validator.Index(problem, len(s.values)))
		}

		s.values = append(s.values, v)
	}

	_, err := dec.ReadToken()

	return err
}

// Value returns the decoded slice as it is and the errors of the values recorded while
// decoding.
func (s NonNullSlice[T]) Value() ([]T, error) {
	if s.null {
		return nil, errNull()
	}

	return s.values, validator.Join(s.errs...)
}

// NonNullMap is a map decoded from a JSON object that must not be null, nor hold null where its
// value type does not decide about null itself. It decodes the members one by one from the
// decoder. A member holding null keeps its name and holds the zero value of V. K is a string
// type, as JSON names members by strings.
type NonNullMap[K ~string, V any] struct {
	values map[K]V
	null   bool
	errs   []error
}

// UnmarshalJSONFrom decodes the next value of dec into m.
func (m *NonNullMap[K, V]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	*m = NonNullMap[K, V]{}

	switch dec.PeekKind() {
	case jsontext.KindNull:
		m.null = true
		return dec.SkipValue()
	case jsontext.KindBeginObject:
	default:
		// The map reports a value of another kind as encoding/json/v2 does.
		return json.UnmarshalDecode(dec, &m.values)
	}

	if _, err := dec.ReadToken(); err != nil {
		return err
	}

	m.values = map[K]V{}

	for dec.PeekKind() != jsontext.KindEndObject {
		// The name decodes as encoding/json/v2 decodes the key of a map, through the
		// unmarshalers of the caller.
		var k K
		if err := json.UnmarshalDecode(dec, &k); err != nil {
			return err
		}

		var v V

		problem, err := decodeValue(dec, &v)
		if err != nil {
			return err
		}

		if problem != nil {
			m.errs = append(m.errs, validator.Key(problem, k))
		}

		m.values[k] = v
	}

	_, err := dec.ReadToken()

	return err
}

// Value returns the decoded map as it is and the errors of the values recorded while decoding.
func (m NonNullMap[K, V]) Value() (map[K]V, error) {
	if m.null {
		return nil, errNull()
	}

	return m.values, validator.Join(m.errs...)
}

// SliceOf returns a function converting a NonNullSlice of W into a slice of T by conv, which
// converts each element. Unlike Value, it allocates the slice it returns. It reports the errors
// of s and of every element.
func SliceOf[W, T any](conv func(W) (T, error)) func(NonNullSlice[W]) ([]T, error) {
	return func(s NonNullSlice[W]) ([]T, error) {
		values, err := s.Value()
		if s.null {
			return nil, err
		}

		errs := []error{err}
		out := make([]T, len(values))

		for i, v := range values {
			var err error
			if out[i], err = conv(v); err != nil {
				errs = append(errs, validator.Index(err, i))
			}
		}

		return out, validator.Join(errs...)
	}
}

// MapOf returns a function converting a NonNullMap of W into a map of T by conv, which converts
// each value. Unlike Value, it allocates the map it returns. It reports the errors of m and of
// every value.
func MapOf[K ~string, W, T any](
	conv func(W) (T, error),
) func(NonNullMap[K, W]) (map[K]T, error) {
	return func(m NonNullMap[K, W]) (map[K]T, error) {
		values, err := m.Value()
		if m.null {
			return nil, err
		}

		errs := []error{err}
		out := make(map[K]T, len(values))

		for k, v := range values {
			var err error
			if out[k], err = conv(v); err != nil {
				errs = append(errs, validator.Key(err, k))
			}
		}

		return out, validator.Join(errs...)
	}
}

// decodeValue decodes the next value of dec into p. It returns as problem a value the schema
// rejects that p could hold: a null where T does not decide about null itself, and for an int64
// a number that is no integer in the range of int64. The value is read in either case, so that
// decoding goes on.
func decodeValue[T any](dec *jsontext.Decoder, p *T) (problem, err error) {
	if _, own := any(p).(json.UnmarshalerFrom); own {
		return nil, json.UnmarshalDecode(dec, p)
	}

	switch dec.PeekKind() {
	case jsontext.KindNull:
		if _, raw := any(p).(*jsontext.Value); raw {
			break
		}

		return errNull(), dec.SkipValue()
	case jsontext.KindNumber:
		n, ok := any(p).(*int64)
		if !ok {
			break
		}

		raw, err := dec.ReadValue()
		if err != nil {
			return nil, err
		}

		v, ok := decimal.Int64(string(raw))
		if !ok {
			return validator.New(
				validator.ErrKind, "want integer in the range of int64, got %s", raw,
			), nil
		}

		*n = v

		return nil, nil
	}

	return nil, json.UnmarshalDecode(dec, p)
}

func errNull() error {
	return validator.New(validator.ErrNull, "value is null")
}
