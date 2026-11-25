package jsonschemart

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"math/big"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/kama/kamaruntime/validator"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

// Sentinel errors name the kinds of failure the validator package does not define. Errors of
// this package wrap them, or the sentinels of the validator package, through [validator.New].
var (
	ErrType          = errors.New("type")
	ErrNoMatch       = errors.New("no match")
	ErrMultipleMatch = errors.New("multiple matches")
	ErrNilKind       = errors.New("nil kind")
	// ErrNoValidate reports a value without the method Validate the validator generator
	// writes, since the generator has not run over the package of its type.
	ErrNoValidate = errors.New("no Validate")
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
	err := json.Unmarshal(raw, v, decodeOptions)

	se, ok := err.(*json.SemanticError)
	if !ok {
		return err
	}

	leaf := se.Err
	if _, ok := errors.AsType[*validator.Error](leaf); !ok {
		// The pointer moves to the validation path, so the copy drops it and the offset to
		// keep it from showing twice.
		c := *se
		c.ByteOffset = 0
		c.JSONPointer = ""
		leaf = &c
	}

	return locate(leaf, se.JSONPointer)
}

// Validate returns the error of the method Validate(stopOnFirstError) of v, which the validator
// generator writes. It is called through an interface, since a type gets that method only after
// the decoders calling it compile. A v without the method is an error wrapping ErrNoValidate.
func Validate(v any, stopOnFirstError bool) error {
	x, ok := v.(interface {
		Validate(stopOnFirstError bool) error
	})
	if !ok {
		return fmt.Errorf(
			"%w: %T has no method Validate(bool) error; run validate.generate over its package",
			ErrNoValidate, v,
		)
	}

	return x.Validate(stopOnFirstError)
}

// ValidateShallow returns the error of the method ValidateShallow(stopOnFirstError) of v, which
// the validator generator writes for a type holding values that validate on decoding. A decoder
// calls it on the value it decoded, whose nested values their own decoders validated. A v
// without the method has nothing such to skip, so its Validate runs instead, as [Validate]
// calls it.
func ValidateShallow(v any, stopOnFirstError bool) error {
	if x, ok := v.(interface {
		ValidateShallow(stopOnFirstError bool) error
	}); ok {
		return x.ValidateShallow(stopOnFirstError)
	}

	return Validate(v, stopOnFirstError)
}

// UnknownMember returns the error of the unknown member of a union, which holds a value no
// member decoded. err is the error decoding left, as NoMatch or MultipleMatch give it. A nil
// err, as of an unknown member built in Go, gives an error wrapping ErrNoMatch.
func UnknownMember(err error) error {
	if err == nil {
		return validator.New(ErrNoMatch, "value matches no member")
	}

	return err
}

// UnmarshalObject is [Unmarshal] for a value that must be a JSON object.
func UnmarshalObject(raw jsontext.Value, v any) error {
	if raw.Kind() != '{' {
		return validator.New(ErrType, "want object, got %s", kindName(raw.Kind()))
	}

	return Unmarshal(raw, v)
}

// Required returns the value of a required, non-nullable member.
func Required[T any](u und.Und[T]) (T, error) {
	var zero T

	switch {
	case u.IsUndefined():
		return zero, validator.New(validator.ErrRequired, "member is missing")
	case u.IsNull():
		return zero, validator.New(validator.ErrNull, "member is null")
	}

	return u.Value(), nil
}

// Optional returns the value of an optional, non-nullable member. None means absent.
func Optional[T any](u und.Und[T]) (opt.Option[T], error) {
	if u.IsNull() {
		return opt.None[T](), validator.New(validator.ErrNull, "member is null")
	}

	return u.Flatten(), nil
}

// RequiredNullable returns the value of a required, nullable member. None means null.
func RequiredNullable[T any](u und.Und[T]) (opt.Option[T], error) {
	if u.IsUndefined() {
		return opt.None[T](), validator.New(validator.ErrRequired, "member is missing")
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

// RequiredNullablePointer is [RequiredNullable] for a member held by pointer. Nil means null.
func RequiredNullablePointer[T any](u und.Und[T]) (*T, error) {
	o, err := RequiredNullable(u)
	if err != nil {
		return nil, err
	}

	return o.Pointer(), nil
}

// OptionalNullablePointer returns the value of an optional, nullable member held by pointer. Nil
// means absent or null.
func OptionalNullablePointer[T any](u und.Und[T]) *T {
	o := u.Flatten()
	return o.Pointer()
}

// DecodeRequired decodes the raw text of a required member whose Go type accepts null itself,
// such as a union holder with a null member. Nil raw means absent.
func DecodeRequired[T any](raw jsontext.Value) (T, error) {
	var v T
	if raw == nil {
		return v, validator.New(validator.ErrRequired, "member is missing")
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

// Omit returns the members of members not named by names, or nil when none is left.
func Omit(members map[string]jsontext.Value, names ...string) map[string]jsontext.Value {
	var out map[string]jsontext.Value

	for name, v := range members {
		if slices.Contains(names, name) {
			continue
		}

		if out == nil {
			out = map[string]jsontext.Value{}
		}

		out[name] = v
	}

	return out
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

// NoMatch reports a value no member of a union decodes. errs holds why each member failed.
func NoMatch(errs []error) error {
	return validator.Join(
		append([]error{validator.New(ErrNoMatch, "value matches no member")}, errs...)...,
	)
}

// MultipleMatch reports a value that the members first and second of a oneOf both decode.
func MultipleMatch(first, second string) error {
	return validator.New(ErrMultipleMatch, "value matches %s, %s", first, second)
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
			return validator.New(ErrType, "want object, got %s", kindName(raw.Kind()))
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

// decodeOptions are the options of [Unmarshal].
var decodeOptions = json.WithUnmarshalers(json.JoinUnmarshalers(rejectNull, integerNumber))

// rejectNull leaves jsontext.Value and a type marking itself as raw JSON
// text by a method RawJson alone, since a raw value carries null as data.
var rejectNull = json.UnmarshalFromFunc(func(dec *jsontext.Decoder, v any) error {
	_, isValue := v.(*jsontext.Value)
	_, isRaw := v.(interface{ RawJson() jsontext.Value })

	if isValue || isRaw || dec.PeekKind() != 'n' {
		return errors.ErrUnsupported
	}

	switch reflect.TypeOf(v).Elem().Kind() {
	case reflect.String,
		reflect.Int64,
		reflect.Float64,
		reflect.Bool,
		reflect.Slice,
		reflect.Map:
		return validator.New(validator.ErrNull, "value is null")
	default:
		return errors.ErrUnsupported
	}
})

// integerNumber decodes a number with a zero fraction, such as 1.0 or 1e2, into an int64, since
// JSON Schema calls such a number an integer. It converts the text exactly, never through
// float64, so 9007199254740993.0 stays 9007199254740993.
var integerNumber = json.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *int64) error {
	if dec.PeekKind() != '0' {
		return errors.ErrUnsupported
	}

	raw, err := dec.ReadValue()
	if err != nil {
		return err
	}

	text, ok := integerText(string(raw))
	if !ok {
		return validator.New(ErrType, "want integer in the range of int64, got %s", raw)
	}

	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return validator.New(ErrType, "want integer in the range of int64, got %s", raw)
	}

	*v = n

	return nil
})

// integerText returns num, the text of a JSON number, as the decimal digits of an integer with an
// optional sign. ok is false when num has a non-zero fraction or has more than 20 digits before
// its decimal point, which no int64 holds.
func integerText(num string) (string, bool) {
	d := parseDecimal(num)

	switch {
	case d.digits == "":
		return "0", true
	// The digits end in no zero, so a point before the last digit leaves a fraction.
	case d.point.Cmp(big.NewInt(int64(len(d.digits)))) < 0:
		return "", false
	// 19 digits hold every int64, so a longer integer needs no zeros written out.
	case d.point.Cmp(big.NewInt(20)) > 0:
		return "", false
	}

	sign := ""
	if d.neg {
		sign = "-"
	}

	return sign + d.digits + strings.Repeat("0", int(d.point.Int64())-len(d.digits)), true
}

// decimal is a JSON number as the value 0.digits * 10^point.
//
// The exponent of the source text can have any number of digits, so point is a big.Int and the
// value is never expanded into its digits.
type decimal struct {
	neg bool
	// digits has no leading and no trailing zero. It is empty for zero.
	digits string
	point  *big.Int
}

// parseDecimal parses num, the text of a JSON number. Zero has neg false whatever its sign.
func parseDecimal(num string) decimal {
	mantissa, expText, _ := strings.Cut(strings.ToLower(num), "e")

	neg := false
	if rest, ok := strings.CutPrefix(mantissa, "-"); ok {
		neg, mantissa = true, rest
	}

	whole, frac, _ := strings.Cut(mantissa, ".")
	all := whole + frac
	digits := strings.TrimLeft(all, "0")

	if digits == "" {
		return decimal{point: new(big.Int)}
	}

	point, ok := new(big.Int).SetString(strings.TrimPrefix(expText, "+"), 10)
	if !ok {
		point = new(big.Int)
	}

	// Leading zeros of the whole part move the point left; those of the fraction are already
	// after it.
	point.Add(point, big.NewInt(int64(len(whole)-(len(all)-len(digits)))))

	return decimal{neg: neg, digits: strings.TrimRight(digits, "0"), point: point}
}

// locate cannot tell an array index from an object member named by digits, so it takes a
// token of digits as an index. Both give the same JSON pointer.
func locate(err error, ptr jsontext.Pointer) error {
	tokens := slices.Collect(ptr.Tokens())
	for _, tok := range slices.Backward(tokens) {
		if i, convErr := strconv.Atoi(tok); convErr == nil && i >= 0 &&
			strconv.Itoa(i) == tok {
			err = validator.Index(err, i)
		} else {
			err = validator.Field(err, tok)
		}
	}

	return err
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
