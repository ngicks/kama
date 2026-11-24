package jsonschemart

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/kama/kamalib/pkg/validation"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

// Sentinel errors name the kinds of failure the validation package does not define. Errors of
// this package wrap them, or the sentinels of the validation package, through [validation.New].
var (
	ErrType          = errors.New("type")
	ErrEnum          = errors.New("enum")
	ErrUnknownMember = errors.New("unknown member")
	ErrNoMatch       = errors.New("no match")
	ErrMultipleMatch = errors.New("multiple matches")
	ErrNilKind       = errors.New("nil kind")
)

// ReadValue reads the next value of dec. The result stays valid after dec reads further.
func ReadValue(dec *jsontext.Decoder) (jsontext.Value, error) {
	raw, err := dec.ReadValue()
	if err != nil {
		return nil, err
	}

	return raw.Clone(), nil
}

// Unmarshal decodes raw into v. It rejects null for a string, a number, a boolean, a slice and a
// map. A json.SemanticError becomes a validation error located by its JSON pointer.
func Unmarshal(raw jsontext.Value, v any) error {
	err := json.Unmarshal(raw, v, rejectNull)

	se, ok := err.(*json.SemanticError)
	if !ok {
		return err
	}

	leaf := se.Err
	if _, ok := errors.AsType[*validation.Error](leaf); !ok {
		// The pointer moves to the validation path, so the copy drops it and the offset to
		// keep it from showing twice.
		c := *se
		c.ByteOffset = 0
		c.JSONPointer = ""
		leaf = &c
	}

	return locate(leaf, se.JSONPointer)
}

// UnmarshalObject is [Unmarshal] for a value that must be a JSON object.
func UnmarshalObject(raw jsontext.Value, v any) error {
	if raw.Kind() != '{' {
		return validation.New(ErrType, "want object, got %s", kindName(raw.Kind()))
	}

	return Unmarshal(raw, v)
}

// Required returns the value of a required, non-nullable member.
func Required[T any](u und.Und[T]) (T, error) {
	var zero T

	switch {
	case u.IsUndefined():
		return zero, validation.New(validation.ErrRequired, "member is missing")
	case u.IsNull():
		return zero, validation.New(validation.ErrNull, "member is null")
	}

	return u.Value(), nil
}

// Optional returns the value of an optional, non-nullable member. None means absent.
func Optional[T any](u und.Und[T]) (opt.Option[T], error) {
	if u.IsNull() {
		return opt.None[T](), validation.New(validation.ErrNull, "member is null")
	}

	return u.Flatten(), nil
}

// RequiredNullable returns the value of a required, nullable member. None means null.
func RequiredNullable[T any](u und.Und[T]) (opt.Option[T], error) {
	if u.IsUndefined() {
		return opt.None[T](), validation.New(validation.ErrRequired, "member is missing")
	}

	return u.Flatten(), nil
}

// RequiredPointer is [Required] for a member held by pointer.
func RequiredPointer[T any](u und.Und[T]) (*T, error) {
	v, err := Required(u)
	if err != nil {
		return nil, err
	}

	return &v, nil
}

// OptionalPointer is [Optional] for a member held by pointer. Nil means absent.
func OptionalPointer[T any](u und.Und[T]) (*T, error) {
	o, err := Optional(u)
	if err != nil {
		return nil, err
	}

	return o.Pointer(), nil
}

// DecodeRequired decodes the raw text of a required member whose Go type accepts null itself,
// such as a union holder with a null member. Nil raw means absent.
func DecodeRequired[T any](raw jsontext.Value) (T, error) {
	var v T
	if raw == nil {
		return v, validation.New(validation.ErrRequired, "member is missing")
	}

	err := Unmarshal(raw, &v)

	return v, err
}

// DecodeOptional is [DecodeRequired] for an optional member. None means absent.
func DecodeOptional[T any](raw jsontext.Value) (opt.Option[T], error) {
	if raw == nil {
		return opt.None[T](), nil
	}

	var v T
	if err := Unmarshal(raw, &v); err != nil {
		return opt.None[T](), err
	}

	return opt.Some(v), nil
}

// Enum reports an error unless v is one of allowed.
func Enum[T comparable](v T, allowed ...T) error {
	if slices.Contains(allowed, v) {
		return nil
	}

	return validation.New(ErrEnum, "want one of %v, got %v", allowed, v)
}

// RejectUnknown reports an error for every member of unknown, in name order.
func RejectUnknown(unknown map[string]jsontext.Value) error {
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(unknown)) {
		errs = append(
			errs,
			validation.Field(validation.New(ErrUnknownMember, "member is not allowed"), name),
		)
	}

	return validation.Join(errs...)
}

// IsKind reports whether raw is of one of the JSON Schema types, such as "object" or "null".
// "integer" accepts every number, since decoding into the Go type rejects a fraction.
func IsKind(raw jsontext.Value, types ...string) bool {
	name := kindName(raw.Kind())

	return slices.Contains(types, name) || (name == "number" && slices.Contains(types, "integer"))
}

// HasMembers reports whether raw is an object with every member of names.
func HasMembers(raw jsontext.Value, names ...string) bool {
	if raw.Kind() != '{' {
		return false
	}

	for _, name := range names {
		if _, ok := member(raw, name); !ok {
			return false
		}
	}

	return true
}

// OnlyMembers reports whether raw is an object whose every member is one of names.
func OnlyMembers(raw jsontext.Value, names ...string) bool {
	if raw.Kind() != '{' {
		return false
	}

	for name := range members(raw) {
		if !slices.Contains(names, name) {
			return false
		}
	}

	return true
}

// MemberEquals reports whether raw is an object whose member name equals the JSON text want.
// The comparison is on the canonical forms of RFC 8785.
func MemberEquals(raw jsontext.Value, name, want string) bool {
	got, ok := member(raw, name)
	if !ok {
		return false
	}

	w := jsontext.Value(want)
	if got.Canonicalize() != nil || w.Canonicalize() != nil {
		return false
	}

	return bytes.Equal(got, w)
}

// OneOf returns the only element of found. names lists the members found, and errs holds why
// the other members failed. Zero or two or more elements are an error.
func OneOf[K any](found []K, names []string, errs []error) (K, error) {
	var zero K

	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return zero, noMatch(errs)
	}

	return zero, validation.New(ErrMultipleMatch, "value matches %s", strings.Join(names, ", "))
}

// AnyOf returns the first element of found. errs holds why the other members failed. No
// element is an error.
func AnyOf[K any](found []K, errs []error) (K, error) {
	if len(found) == 0 {
		var zero K
		return zero, noMatch(errs)
	}

	return found[0], nil
}

// MemberError returns err annotated with the union member name.
func MemberError(name string, err error) error {
	return fmt.Errorf("member %s: %w", name, err)
}

// MarshalObjects writes one object holding the members of every value of values, each of
// which must marshal to an object. A member name already written is skipped.
func MarshalObjects(enc *jsontext.Encoder, values ...any) error {
	seen := map[string]bool{}

	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}

	for _, v := range values {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}

		raw := jsontext.Value(b)
		if raw.Kind() != '{' {
			return validation.New(ErrType, "want object, got %s", kindName(raw.Kind()))
		}

		for name, value := range members(raw) {
			if seen[name] {
				continue
			}

			seen[name] = true

			if err := enc.WriteToken(jsontext.String(name)); err != nil {
				return err
			}

			if err := enc.WriteValue(value); err != nil {
				return err
			}
		}
	}

	return enc.WriteToken(jsontext.EndObject)
}

// rejectNull leaves jsontext.Value alone, since a raw value carries null as data.
var rejectNull = json.WithUnmarshalers(
	json.UnmarshalFromFunc(func(dec *jsontext.Decoder, v any) error {
		if _, ok := v.(*jsontext.Value); ok || dec.PeekKind() != 'n' {
			return errors.ErrUnsupported
		}

		switch reflect.TypeOf(v).Elem().Kind() {
		case reflect.String,
			reflect.Int64,
			reflect.Float64,
			reflect.Bool,
			reflect.Slice,
			reflect.Map:
			return validation.New(validation.ErrNull, "value is null")
		default:
			return errors.ErrUnsupported
		}
	}),
)

// locate cannot tell an array index from an object member named by digits, so it takes a
// token of digits as an index. Both give the same JSON pointer.
func locate(err error, ptr jsontext.Pointer) error {
	tokens := slices.Collect(ptr.Tokens())
	for _, tok := range slices.Backward(tokens) {
		if i, convErr := strconv.Atoi(tok); convErr == nil && i >= 0 &&
			strconv.Itoa(i) == tok {
			err = validation.Index(err, i)
		} else {
			err = validation.Field(err, tok)
		}
	}

	return err
}

func noMatch(errs []error) error {
	return validation.Join(
		append([]error{validation.New(ErrNoMatch, "value matches no member")}, errs...)...,
	)
}

func kindName(k jsontext.Kind) string {
	switch k {
	case 'n':
		return "null"
	case 't', 'f':
		return "boolean"
	case '"':
		return "string"
	case '0':
		return "number"
	case '{':
		return "object"
	case '[':
		return "array"
	default:
		return "invalid"
	}
}

func member(raw jsontext.Value, name string) (jsontext.Value, bool) {
	for n, v := range members(raw) {
		if n == name {
			return v, true
		}
	}

	return nil, false
}

// members stops silently at malformed input. Its callers read values that ReadValue or
// json.Marshal already checked.
func members(raw jsontext.Value) iter.Seq2[string, jsontext.Value] {
	return func(yield func(string, jsontext.Value) bool) {
		dec := jsontext.NewDecoder(bytes.NewReader(raw))
		if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '{' {
			return
		}

		for dec.PeekKind() == '"' {
			tok, err := dec.ReadToken()
			if err != nil {
				return
			}

			// ReadValue voids tok, so the name is taken first.
			name := tok.String()

			value, err := dec.ReadValue()
			if err != nil {
				return
			}

			if !yield(name, value.Clone()) {
				return
			}
		}
	}
}
