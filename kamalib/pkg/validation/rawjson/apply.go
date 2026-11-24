package rawjson

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"regexp"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

// Func validates a member or an element of a JSON value.
type Func func(v jsontext.Value) error

// Scope validates a JSON value and records what it evaluates into ev.
type Scope func(ev *Evaluated) error

// Evaluated records the members of an object and the elements of an array
// that validation evaluated, as JSON Schema annotations tell the keywords
// unevaluatedProperties and unevaluatedItems. A nil *Evaluated records
// nothing.
type Evaluated struct {
	members map[string]bool
	items   map[int]bool
}

func (ev *Evaluated) addMember(name string) {
	if ev == nil {
		return
	}

	if ev.members == nil {
		ev.members = map[string]bool{}
	}

	ev.members[name] = true
}

func (ev *Evaluated) addItem(i int) {
	if ev == nil {
		return
	}

	if ev.items == nil {
		ev.items = map[int]bool{}
	}

	ev.items[i] = true
}

func (ev *Evaluated) merge(other *Evaluated) {
	for name := range other.members {
		ev.addMember(name)
	}

	for i := range other.items {
		ev.addItem(i)
	}
}

// Branch returns the error of f, which records into a record of its own.
// When f holds, what it evaluated is added to ev, as an alternative of
// anyOf or oneOf adds its annotations only when it holds.
func Branch(ev *Evaluated, f Scope) error {
	if ev == nil {
		return f(nil)
	}

	own := &Evaluated{}

	err := f(own)
	if err == nil {
		ev.merge(own)
	}

	return err
}

// If returns the error of then when cond holds, otherwise that of
// otherwise, or nil when otherwise is nil, as JSON Schema if, then and else
// test. Only the branch taken runs and records into ev, and cond records
// into ev when it holds.
func If(ev *Evaluated, cond, then, otherwise Scope) error {
	if Branch(ev, cond) == nil {
		return then(ev)
	}

	if otherwise == nil {
		return nil
	}

	return otherwise(ev)
}

// Member is an entry of Properties: a member it names, the members a
// pattern matches, or the members no other entry applies to.
type Member struct {
	name string
	re   *regexp.Regexp
	rest bool
	f    Func
}

// Property applies f to the member name, when present, as JSON Schema
// properties does.
func Property(name string, f Func) Member { return Member{name: name, f: f} }

// PatternProperty applies f to every member whose name re matches, as JSON
// Schema patternProperties does.
func PatternProperty(re *regexp.Regexp, f Func) Member { return Member{re: re, f: f} }

// Additional applies f to every member that no Property names and no
// PatternProperty matches, as JSON Schema additionalProperties does.
func Additional(f Func) Member { return Member{rest: true, f: f} }

// Properties reports the members of an object v that fail the entries
// applying to them, each located at its member. It records into ev every
// member an entry applies to. A value of another kind passes.
func Properties[T ~[]byte](v T, stopOnFirstError bool, ev *Evaluated, entries ...Member) error {
	raw := jsontext.Value(v)
	if raw.Kind() != '{' {
		return nil
	}

	var errs []error

	for name, value := range orderedMembers(raw) {
		matched := false

		for _, m := range entries {
			if m.rest || m.re != nil && !m.re.MatchString(name) || m.re == nil && m.name != name {
				continue
			}

			matched = true

			if err := m.f(value); err != nil {
				errs = append(errs, validation.Field(err, name))
				if stopOnFirstError {
					return errs[0]
				}
			}
		}

		for _, m := range entries {
			if !m.rest || matched {
				continue
			}

			matched = true

			if err := m.f(value); err != nil {
				errs = append(errs, validation.Field(err, name))
				if stopOnFirstError {
					return errs[0]
				}
			}
		}

		if matched {
			ev.addMember(name)
		}
	}

	return validation.Join(errs...)
}

// Elements reports the elements of an array v that fail the function
// applying to them, each located at its index: prefix[i] applies to the
// element i, as JSON Schema prefixItems does, and rest, when not nil, to
// every later element, as items does. It records into ev every element a
// function applies to. A value of another kind passes.
func Elements[T ~[]byte](
	v T,
	stopOnFirstError bool,
	ev *Evaluated,
	rest Func,
	prefix ...Func,
) error {
	raw := jsontext.Value(v)
	if raw.Kind() != '[' {
		return nil
	}

	var errs []error

	for i, e := range elements(raw) {
		f := rest
		if i < len(prefix) {
			f = prefix[i]
		}

		if f == nil {
			break
		}

		ev.addItem(i)

		if err := f(e); err != nil {
			errs = append(errs, validation.Index(err, i))
			if stopOnFirstError {
				return errs[0]
			}
		}
	}

	return validation.Join(errs...)
}

// Contains reports an array v whose number of elements passing f does not
// compare to each of counts, as JSON Schema contains, minContains and
// maxContains test. It records into ev every element passing f. A value of
// another kind passes.
func Contains[T ~[]byte](v T, ev *Evaluated, f Func, counts ...Count) error {
	raw := jsontext.Value(v)
	if raw.Kind() != '[' {
		return nil
	}

	var got int

	for i, e := range elements(raw) {
		if f(e) == nil {
			got++

			ev.addItem(i)
		}
	}

	for _, c := range counts {
		if !holds(compareInts(got, c.N), c.Op) {
			return validation.New(validation.ErrContains,
				"want the number of matching elements %s %d, got %d", c.Op, c.N, got)
		}
	}

	return nil
}

// Count is a comparison of a number of elements, as in >= 1.
type Count struct {
	Op string
	N  int
}

// Keys reports the names of the members of an object v that fail f, which
// receives each name as a JSON string, as JSON Schema propertyNames tests.
// The error of a name is located at its member. A value of another kind
// passes.
func Keys[T ~[]byte](v T, stopOnFirstError bool, f Func) error {
	raw := jsontext.Value(v)
	if raw.Kind() != '{' {
		return nil
	}

	var errs []error

	for name := range orderedMembers(raw) {
		key, err := jsontext.AppendQuote(nil, name)
		if err != nil {
			continue
		}

		if err := f(key); err != nil {
			errs = append(errs, validation.Field(err, name))
			if stopOnFirstError {
				return errs[0]
			}
		}
	}

	return validation.Join(errs...)
}

// UnevaluatedMembers runs scope on v with a record of its own, then
// reports each member of an object v that scope did not evaluate and that
// fails f, located at its member, as JSON Schema unevaluatedProperties
// tests. On success it records into ev what scope evaluated and every
// member. A value of another kind passes f.
func UnevaluatedMembers[T ~[]byte](
	v T,
	stopOnFirstError bool,
	ev *Evaluated,
	scope Scope,
	f Func,
) error {
	raw := jsontext.Value(v)
	own := &Evaluated{}

	err := scope(own)
	if err != nil && stopOnFirstError {
		return err
	}

	errs := []error{err}

	if raw.Kind() == '{' {
		for name, value := range orderedMembers(raw) {
			if own.members[name] {
				continue
			}

			own.addMember(name)

			if err := f(value); err != nil {
				errs = append(errs, validation.Field(err, name))
				if stopOnFirstError {
					return validation.Join(errs...)
				}
			}
		}
	}

	joined := validation.Join(errs...)
	if joined == nil {
		ev.merge(own)
	}

	return joined
}

// UnevaluatedElements is UnevaluatedMembers for the elements of an array,
// as JSON Schema unevaluatedItems tests.
func UnevaluatedElements[T ~[]byte](
	v T,
	stopOnFirstError bool,
	ev *Evaluated,
	scope Scope,
	f Func,
) error {
	raw := jsontext.Value(v)
	own := &Evaluated{}

	err := scope(own)
	if err != nil && stopOnFirstError {
		return err
	}

	errs := []error{err}

	if raw.Kind() == '[' {
		for i, e := range elements(raw) {
			if own.items[i] {
				continue
			}

			own.addItem(i)

			if err := f(e); err != nil {
				errs = append(errs, validation.Index(err, i))
				if stopOnFirstError {
					return validation.Join(errs...)
				}
			}
		}
	}

	joined := validation.Join(errs...)
	if joined == nil {
		ev.merge(own)
	}

	return joined
}

// Decode reports v that does not decode into a T, as JSON Schema $ref
// applies the schema of T. The decoder of T reports a value T does not
// admit, as the decoder of a type the jsonschema generator declares for an
// object does.
func Decode[T any, V ~[]byte](v V) error {
	var x T
	if err := jsonv2.Unmarshal(v, &x); err != nil {
		return validation.New(validation.ErrDecode, "want a value of %T: %v", x, err)
	}

	return nil
}

// Encoded reports the errors f returns for v encoded as JSON, as JSON
// Schema applies its keywords to the JSON value whatever Go type holds it.
// A value that does not encode is an error wrapping validation.ErrEncode.
func Encoded(v any, f Func) error {
	b, err := jsonv2.Marshal(v)
	if err != nil {
		return validation.New(validation.ErrEncode, "%v", err)
	}

	return f(b)
}
