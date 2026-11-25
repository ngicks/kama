package rawjson

import (
	"encoding/json/jsontext"
	"regexp"
	"testing"

	"github.com/ngicks/kama/kamaruntime/validator"
)

func kindFunc(kinds ...string) Func {
	return func(v jsontext.Value) error { return Kind(v, kinds...) }
}

func TestProperties(t *testing.T) {
	entries := []Member{
		Property("a", kindFunc("integer")),
		PatternProperty(regexp.MustCompile("^x"), kindFunc("string")),
		Additional(kindFunc("boolean")),
	}

	for _, ok := range []string{`{}`, `{"a": 1, "xy": "s", "b": true}`, `"not an object"`, `[1]`} {
		nilError(t, Properties(named(ok), false, nil, entries...), ok)
	}

	err := Properties(named(`{"a": "1", "xa": 2, "b": null}`), false, nil, entries...)
	errorIs(t, err, validator.ErrKind)
	deepEqual(t, validator.Pointers(err), []string{"/a", "/b", "/xa"})

	ev := &Evaluated{}
	nilError(t, Properties(named(`{"a": 1, "xy": "s", "b": true}`), false, ev,
		Property("a", kindFunc("integer")), Property("c", kindFunc("null"))))
	deepEqual(t, ev.members, map[string]bool{"a": true})
}

func TestElements(t *testing.T) {
	prefix := []Func{kindFunc("string"), kindFunc("number")}

	nilError(t, Elements(named(`["a", 1, true]`), false, nil, nil, prefix...))
	nilError(t, Elements(named(`{"a": 1}`), false, nil, kindFunc("null")))

	err := Elements(named(`["a", "b", 1, null]`), false, nil, kindFunc("null"), prefix...)
	deepEqual(t, validator.Pointers(err), []string{"/1", "/2"})

	ev := &Evaluated{}
	nilError(t, Elements(named(`["a", 1, true]`), false, ev, nil, prefix...))
	deepEqual(t, ev.items, map[int]bool{0: true, 1: true})
}

func TestContains(t *testing.T) {
	two := []Count{{Op: ">=", N: 1}, {Op: "<=", N: 2}}

	for _, ok := range []string{`[1, "a"]`, `[1, 1, "a"]`, `"a"`} {
		nilError(t, Contains(named(ok), nil, kindFunc("integer"), two...), ok)
	}

	for _, bad := range []string{`[]`, `["a"]`, `[1, 1, 1]`} {
		errorIs(t, Contains(named(bad), nil, kindFunc("integer"), two...),
			validator.ErrContains, bad)
	}

	ev := &Evaluated{}
	nilError(t, Contains(named(`["a", 1]`), ev, kindFunc("integer"), two...))
	deepEqual(t, ev.items, map[int]bool{1: true})
}

func TestKeys(t *testing.T) {
	short := func(v jsontext.Value) error { return Runes(v, "<=", 2) }

	nilError(t, Keys(named(`{"ab": 1}`), false, short))
	nilError(t, Keys(named(`[1]`), false, short))

	err := Keys(named(`{"abc": 1, "a\"b": 2}`), false, short)
	errorIs(t, err, validator.ErrLength)
	deepEqual(t, validator.Pointers(err), []string{"/a\"b", "/abc"})
}

func TestUnevaluated(t *testing.T) {
	scope := func(ev *Evaluated) error {
		return Properties(named(`{"a": 1, "b": 2}`), false, ev, Property("a", kindFunc("integer")))
	}

	err := UnevaluatedMembers(named(`{"a": 1, "b": 2}`), false, nil, scope, kindFunc("null"))
	deepEqual(t, validator.Pointers(err), []string{"/b"})

	ev := &Evaluated{}
	nilError(t, UnevaluatedMembers(named(`{"a": 1, "b": 2}`), false, ev, scope,
		kindFunc("integer")))
	deepEqual(t, ev.members, map[string]bool{"a": true, "b": true})

	prefix := func(ev *Evaluated) error {
		return Elements(named(`["a", 1]`), false, ev, nil, kindFunc("string"))
	}

	err = UnevaluatedElements(named(`["a", 1]`), false, nil, prefix, kindFunc("null"))
	deepEqual(t, validator.Pointers(err), []string{"/1"})
}

func TestBranch(t *testing.T) {
	v := named(`{"a": 1, "b": 2}`)
	field := func(name string) Scope {
		return func(ev *Evaluated) error {
			return Properties(v, false, ev, Property(name, kindFunc("integer")))
		}
	}
	fails := func(*Evaluated) error { return validator.New(validator.ErrKind, "fails") }

	ev := &Evaluated{}
	nilError(t, Branch(ev, field("a")))
	errorIs(t, Branch(ev, func(ev *Evaluated) error {
		_ = field("b")(ev)
		return fails(ev)
	}), validator.ErrKind)
	deepEqual(t, ev.members, map[string]bool{"a": true})

	ev = &Evaluated{}
	nilError(t, If(ev, field("a"), field("b"), fails))
	deepEqual(t, ev.members, map[string]bool{"a": true, "b": true})

	ev = &Evaluated{}
	errorIs(t, If(ev, fails, field("b"), fails), validator.ErrKind)
	nilError(t, If(ev, fails, fails, nil))
	equal(t, len(ev.members), 0)
}

type decoded struct{ A int }

func TestDecode(t *testing.T) {
	nilError(t, Decode[decoded](named(`{"A": 1}`)))
	errorIs(t, Decode[decoded](named(`"a"`)), validator.ErrDecode)
}

type unencodable struct{ C chan int }

func TestEncoded(t *testing.T) {
	nilError(t, Encoded(decoded{A: 1}, func(v jsontext.Value) error {
		return Required(v, false, "A")
	}))
	errorIs(t, Encoded(decoded{A: 1}, kindFunc("string")), validator.ErrKind)
	errorIs(t, Encoded(unencodable{}, kindFunc("object")), validator.ErrEncode)
}
