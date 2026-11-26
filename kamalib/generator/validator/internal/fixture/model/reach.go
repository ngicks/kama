package model

import (
	"encoding/json/jsontext"

	"github.com/ngicks/option/opt"
)

// Shape is raw JSON text whose members and elements the validators
// reaching into it check.
//
//kama:validate props(field("a", kind("integer")), field("b c"), match("^x", kind("string")), rest(never))
//kama:validate prefix(kind("string"), rest(cmp(ge=0)))
//kama:validate contains(const(0), ge=1, le=2)
//kama:validate keys(runes(le=3))
type Shape jsontext.Value

// RawJson marks Shape as raw JSON text.
func (v Shape) RawJson() jsontext.Value { return jsontext.Value(v) }

// Holder is raw JSON text whose member bag decodes into a RawBag.
//
//kama:validate props(field("bag", decode("RawBag")))
type Holder jsontext.Value

// RawJson marks Holder as raw JSON text.
func (v Holder) RawJson() jsontext.Value { return jsontext.Value(v) }

// Tree is raw JSON text whose member n is a Tree. An object holds a or b,
// and its other members are null. An array starting with a string holds
// strings only, and the elements of another array are null.
//
//kama:validate props(field("n", ref("Tree")))
//kama:validate unevalprops(props(field("a")) or props(field("b")) or props(field("n")), kind("null"))
//kama:validate unevalitems(items(kind("string")) if prefix(kind("string")) else always, kind("null"))
type Tree jsontext.Value

// RawJson marks Tree as raw JSON text.
func (v Tree) RawJson() jsontext.Value { return jsontext.Value(v) }

// Encoded is a struct whose encoding the validators of raw JSON text check:
// it holds no member x, and the members its fields leave out, which X
// holds, are integers.
//
//kama:validate hook=False, json(not required("x"), props(field("a"), rest(kind("integer"))))
type Encoded struct {
	// A is not "b", and an absent A is not checked.
	//
	//kama:validate json(not const("b"))
	A opt.Option[string]        `json:"a,omitzero"`
	X map[string]jsontext.Value `json:",embed"`
}

// Pairs is a slice whose encoding holds at most one 0.
//
//kama:validate json(contains(const(0), le=1))
type Pairs []int
