package rawjson

import (
	"encoding/json/jsontext"
	"regexp"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
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
		assert.NilError(t, Properties(named(ok), false, nil, entries...), ok)
	}

	err := Properties(named(`{"a": "1", "xa": 2, "b": null}`), false, nil, entries...)
	assert.ErrorIs(t, err, validation.ErrKind)
	assert.DeepEqual(t, validation.Pointers(err), []string{"/a", "/b", "/xa"})

	ev := &Evaluated{}
	assert.NilError(t, Properties(named(`{"a": 1, "xy": "s", "b": true}`), false, ev,
		Property("a", kindFunc("integer")), Property("c", kindFunc("null"))))
	assert.DeepEqual(t, ev.members, map[string]bool{"a": true})
}

func TestElements(t *testing.T) {
	prefix := []Func{kindFunc("string"), kindFunc("number")}

	assert.NilError(t, Elements(named(`["a", 1, true]`), false, nil, nil, prefix...))
	assert.NilError(t, Elements(named(`{"a": 1}`), false, nil, kindFunc("null")))

	err := Elements(named(`["a", "b", 1, null]`), false, nil, kindFunc("null"), prefix...)
	assert.DeepEqual(t, validation.Pointers(err), []string{"/1", "/2"})

	ev := &Evaluated{}
	assert.NilError(t, Elements(named(`["a", 1, true]`), false, ev, nil, prefix...))
	assert.DeepEqual(t, ev.items, map[int]bool{0: true, 1: true})
}

func TestContains(t *testing.T) {
	two := []Count{{Op: ">=", N: 1}, {Op: "<=", N: 2}}

	for _, ok := range []string{`[1, "a"]`, `[1, 1, "a"]`, `"a"`} {
		assert.NilError(t, Contains(named(ok), nil, kindFunc("integer"), two...), ok)
	}

	for _, bad := range []string{`[]`, `["a"]`, `[1, 1, 1]`} {
		assert.ErrorIs(t, Contains(named(bad), nil, kindFunc("integer"), two...),
			validation.ErrContains, bad)
	}

	ev := &Evaluated{}
	assert.NilError(t, Contains(named(`["a", 1]`), ev, kindFunc("integer"), two...))
	assert.DeepEqual(t, ev.items, map[int]bool{1: true})
}

func TestKeys(t *testing.T) {
	short := func(v jsontext.Value) error { return Runes(v, "<=", 2) }

	assert.NilError(t, Keys(named(`{"ab": 1}`), false, short))
	assert.NilError(t, Keys(named(`[1]`), false, short))

	err := Keys(named(`{"abc": 1, "a\"b": 2}`), false, short)
	assert.ErrorIs(t, err, validation.ErrLength)
	assert.DeepEqual(t, validation.Pointers(err), []string{"/a\"b", "/abc"})
}

func TestUnevaluated(t *testing.T) {
	scope := func(ev *Evaluated) error {
		return Properties(named(`{"a": 1, "b": 2}`), false, ev, Property("a", kindFunc("integer")))
	}

	err := UnevaluatedMembers(named(`{"a": 1, "b": 2}`), false, nil, scope, kindFunc("null"))
	assert.DeepEqual(t, validation.Pointers(err), []string{"/b"})

	ev := &Evaluated{}
	assert.NilError(t, UnevaluatedMembers(named(`{"a": 1, "b": 2}`), false, ev, scope,
		kindFunc("integer")))
	assert.DeepEqual(t, ev.members, map[string]bool{"a": true, "b": true})

	prefix := func(ev *Evaluated) error {
		return Elements(named(`["a", 1]`), false, ev, nil, kindFunc("string"))
	}

	err = UnevaluatedElements(named(`["a", 1]`), false, nil, prefix, kindFunc("null"))
	assert.DeepEqual(t, validation.Pointers(err), []string{"/1"})
}

func TestBranch(t *testing.T) {
	v := named(`{"a": 1, "b": 2}`)
	field := func(name string) Scope {
		return func(ev *Evaluated) error {
			return Properties(v, false, ev, Property(name, kindFunc("integer")))
		}
	}
	fails := func(*Evaluated) error { return validation.New(validation.ErrKind, "fails") }

	ev := &Evaluated{}
	assert.NilError(t, Branch(ev, field("a")))
	assert.ErrorIs(t, Branch(ev, func(ev *Evaluated) error {
		_ = field("b")(ev)
		return fails(ev)
	}), validation.ErrKind)
	assert.DeepEqual(t, ev.members, map[string]bool{"a": true})

	ev = &Evaluated{}
	assert.NilError(t, If(ev, field("a"), field("b"), fails))
	assert.DeepEqual(t, ev.members, map[string]bool{"a": true, "b": true})

	ev = &Evaluated{}
	assert.ErrorIs(t, If(ev, fails, field("b"), fails), validation.ErrKind)
	assert.NilError(t, If(ev, fails, fails, nil))
	assert.Equal(t, len(ev.members), 0)
}

type decoded struct{ A int }

func TestDecode(t *testing.T) {
	assert.NilError(t, Decode[decoded](named(`{"A": 1}`)))
	assert.ErrorIs(t, Decode[decoded](named(`"a"`)), validation.ErrDecode)
}
