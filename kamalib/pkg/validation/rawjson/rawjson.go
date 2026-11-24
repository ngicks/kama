package rawjson

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"iter"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

// Kind reports v holding none of kinds, the JSON types null, boolean,
// object, array, number, string and integer, a number with a zero fraction.
func Kind[T ~[]byte](v T, kinds ...string) error {
	got := kindOf(jsontext.Value(v))
	if slices.Contains(kinds, got) ||
		got == "number" && slices.Contains(kinds, "integer") && isInteger(string(v)) {
		return nil
	}

	return validation.New(validation.ErrKind, "want %s, got %s", strings.Join(kinds, " or "), got)
}

// Compare reports a number v that does not compare to num, the text of a
// JSON number, by op: >, >=, ==, !=, <= or <. It compares exactly. A value
// of another kind passes.
func Compare[T ~[]byte](v T, op, num string) error {
	raw := jsontext.Value(v)
	if raw.Kind() != '0' {
		return nil
	}

	if c, ok := compareNumbers(string(raw), num); ok && holds(c, op) {
		return nil
	}

	return validation.New(validation.ErrCompare, "want %s %s, got %s", op, num, raw)
}

// MultipleOf reports a number v that num, the text of a positive JSON
// number, does not divide to an integer. A value of another kind passes.
func MultipleOf[T ~[]byte](v T, num string) error {
	raw := jsontext.Value(v)
	if raw.Kind() != '0' || multiple(string(raw), num) {
		return nil
	}

	return validation.New(validation.ErrMultiple, "want a multiple of %s, got %s", num, raw)
}

// Runes reports a string v whose number of code points does not compare
// to n by op. A value of another kind passes.
func Runes[T ~[]byte](v T, op string, n int) error {
	s, ok := stringOf(jsontext.Value(v))
	if !ok {
		return nil
	}

	if got := utf8.RuneCountInString(s); !holds(compareInts(got, n), op) {
		return validation.New(validation.ErrLength, "want rune count %s %d, got %d", op, n, got)
	}

	return nil
}

// Pattern reports a string v that re does not match. A value of another
// kind passes.
func Pattern[T ~[]byte](v T, re *regexp.Regexp) error {
	s, ok := stringOf(jsontext.Value(v))
	if !ok || re.MatchString(s) {
		return nil
	}

	return validation.New(validation.ErrPattern, "want a match of %q, got %q", re.String(), s)
}

// Format reports a string v that check, a helper testing one format,
// rejects. A value of another kind passes.
func Format[T ~[]byte](v T, check func(string) error) error {
	s, ok := stringOf(jsontext.Value(v))
	if !ok {
		return nil
	}

	return check(s)
}

// Items reports an array v whose number of elements does not compare to n
// by op. A value of another kind passes.
func Items[T ~[]byte](v T, op string, n int) error {
	raw := jsontext.Value(v)
	if raw.Kind() != '[' {
		return nil
	}

	if got := len(elements(raw)); !holds(compareInts(got, n), op) {
		return validation.New(validation.ErrLength, "want length %s %d, got %d", op, n, got)
	}

	return nil
}

// Members reports an object v whose number of members does not compare to
// n by op. A value of another kind passes.
func Members[T ~[]byte](v T, op string, n int) error {
	raw := jsontext.Value(v)
	if raw.Kind() != '{' {
		return nil
	}

	if got := len(members(raw)); !holds(compareInts(got, n), op) {
		return validation.New(validation.ErrLength, "want member count %s %d, got %d", op, n, got)
	}

	return nil
}

// Unique reports an array v holding two elements Equal reports equal. A
// value of another kind passes.
func Unique[T ~[]byte](v T) error {
	raw := jsontext.Value(v)
	if raw.Kind() != '[' {
		return nil
	}

	elems := elements(raw)

	for i := range elems {
		for j := i + 1; j < len(elems); j++ {
			if Equal(elems[i], elems[j]) {
				return validation.New(validation.ErrUnique,
					"want unique elements, got equal elements at %d and %d", i, j)
			}
		}
	}

	return nil
}

// Const reports v equal to none of values, the texts of JSON values, by
// Equal.
func Const[T ~[]byte](v T, values ...string) error {
	raw := jsontext.Value(v)

	found := slices.ContainsFunc(values, func(want string) bool {
		return Equal(raw, jsontext.Value(want))
	})
	if found {
		return nil
	}

	return validation.New(
		validation.ErrConst,
		"want one of %s, got %s",
		strings.Join(values, ", "),
		raw,
	)
}

// Required reports each of names that an object v holds no member of, at
// that member. A value of another kind passes.
func Required[T ~[]byte](v T, stopOnFirstError bool, names ...string) error {
	raw := jsontext.Value(v)
	if raw.Kind() != '{' {
		return nil
	}

	have := members(raw)

	var errs []error

	for _, name := range names {
		if _, ok := have[name]; ok {
			continue
		}

		err := validation.Field(validation.New(validation.ErrRequired, "want a member"), name)
		if stopOnFirstError {
			return err
		}

		errs = append(errs, err)
	}

	return validation.Join(errs...)
}

// Equal reports whether a and b are the same JSON value: numbers of equal
// value, as 1 and 1.0, strings of equal content, arrays of equal elements
// in order, objects of the same names with equal values in any order, or
// the same literal.
func Equal(a, b jsontext.Value) bool {
	if a.Kind() != b.Kind() {
		return false
	}

	switch a.Kind() {
	case '0':
		c, ok := compareNumbers(string(a), string(b))
		return ok && c == 0
	case '"':
		sa, okA := stringOf(a)
		sb, okB := stringOf(b)

		return okA && okB && sa == sb
	case '[':
		ea, eb := elements(a), elements(b)

		return slices.EqualFunc(ea, eb, Equal)
	case '{':
		ma, mb := members(a), members(b)
		if len(ma) != len(mb) {
			return false
		}

		for name, va := range ma {
			vb, ok := mb[name]
			if !ok || !Equal(va, vb) {
				return false
			}
		}

		return true
	}

	return a.Kind() != 0
}

// kindOf returns the JSON type of v, never integer.
func kindOf(v jsontext.Value) string {
	switch v.Kind() {
	case 'n':
		return "null"
	case 't', 'f':
		return "boolean"
	case '{':
		return "object"
	case '[':
		return "array"
	case '0':
		return "number"
	case '"':
		return "string"
	}

	return "invalid JSON"
}

func stringOf(v jsontext.Value) (string, bool) {
	var s string
	if v.Kind() != '"' || jsonv2.Unmarshal(v, &s) != nil {
		return "", false
	}

	return s, true
}

// elements returns the elements of the array v.
func elements(v jsontext.Value) []jsontext.Value {
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if _, err := dec.ReadToken(); err != nil {
		return nil
	}

	var out []jsontext.Value

	for dec.PeekKind() != ']' {
		e, err := dec.ReadValue()
		if err != nil {
			return out
		}

		out = append(out, e.Clone())
	}

	return out
}

// members returns the members of the object v by name.
func members(v jsontext.Value) map[string]jsontext.Value {
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if _, err := dec.ReadToken(); err != nil {
		return nil
	}

	out := map[string]jsontext.Value{}

	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return out
		}

		// The token is valid until the next read only.
		name := tok.String()

		value, err := dec.ReadValue()
		if err != nil {
			return out
		}

		out[name] = value.Clone()
	}

	return out
}

// orderedMembers yields the members of the object v in the order v holds
// them, so that errors come in that order.
func orderedMembers(v jsontext.Value) iter.Seq2[string, jsontext.Value] {
	return func(yield func(string, jsontext.Value) bool) {
		dec := jsontext.NewDecoder(bytes.NewReader(v))
		if _, err := dec.ReadToken(); err != nil {
			return
		}

		for dec.PeekKind() != '}' {
			tok, err := dec.ReadToken()
			if err != nil {
				return
			}

			// The token is valid until the next read only.
			name := tok.String()

			value, err := dec.ReadValue()
			if err != nil || !yield(name, value.Clone()) {
				return
			}
		}
	}
}

// holds reports whether the result c of comparing two values satisfies op.
func holds(c int, op string) bool {
	switch op {
	case ">":
		return c > 0
	case ">=":
		return c >= 0
	case "==":
		return c == 0
	case "!=":
		return c != 0
	case "<=":
		return c <= 0
	case "<":
		return c < 0
	}

	return false
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}

	return 0
}
