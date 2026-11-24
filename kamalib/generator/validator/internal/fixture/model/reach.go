package model

import (
	"encoding/json/jsontext"

	"github.com/ngicks/option/opt"
)

// Shape is raw JSON text whose members and elements the validators
// reaching into it check.
//
//kama:validate props(field("a", kind=integer), field("b c"), match("^x", kind=string), rest(or()))
//kama:validate prefix(kind=string, rest(cmp=">=0"))
//kama:validate contains(>=1, <=2, const="0")
//kama:validate keys(runes="<=3")
type Shape jsontext.Value

// RawJson marks Shape as raw JSON text.
func (v Shape) RawJson() jsontext.Value { return jsontext.Value(v) }

// Holder is raw JSON text whose member bag decodes into a RawBag.
//
//kama:validate props(field("bag", decode=RawBag))
type Holder jsontext.Value

// RawJson marks Holder as raw JSON text.
func (v Holder) RawJson() jsontext.Value { return jsontext.Value(v) }

// Tree is raw JSON text whose member n is a Tree. An object holds a or b,
// and its other members are null. An array starting with a string holds
// strings only, and the elements of another array are null.
//
//kama:validate props(field("n", ref=Tree))
//kama:validate unevalprops(or(props(field("a")), props(field("b")), props(field("n"))), kind=null)
//kama:validate unevalitems(if(prefix(kind=string), items(kind=string)), kind=null)
type Tree jsontext.Value

// RawJson marks Tree as raw JSON text.
func (v Tree) RawJson() jsontext.Value { return jsontext.Value(v) }

// Encoded is a struct whose encoding the validators of raw JSON text check:
// it holds no member x, and the members its fields leave out, which X
// holds, are integers.
//
//kama:validate hook=off json(not(required=x), props(field("a"), rest(kind=integer)))
type Encoded struct {
	// A is not "b", and an absent A is not checked.
	//
	//kama:validate json(not(const="`\"b\"`"))
	A opt.Option[string]        `json:"a,omitzero"`
	X map[string]jsontext.Value `json:",embed"`
}

// Pairs is a slice whose encoding holds at most one 0.
//
//kama:validate json(contains(<=1, const="0"))
type Pairs []int
