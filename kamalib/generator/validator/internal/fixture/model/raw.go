package model

import "encoding/json/jsontext"

// Loose is raw JSON text of a number of at least 1 or a string of at most
// 3 runes.
//
//kama:validate kind("number", "string"), cmp(ge=1), runes(le=3)
type Loose jsontext.Value

// RawJson marks Loose as raw JSON text.
func (v Loose) RawJson() jsontext.Value { return jsontext.Value(v) }

// RawBag holds the validators of raw JSON text on fields.
//
//kama:validate hook=False
type RawBag struct {
	//kama:validate multipleof(0.5), regexp("^a"), length(le=2), unique
	Mixed jsontext.Value `json:"mixed"`
	//kama:validate members(le=1), required("a")
	Object jsontext.Value `json:"object,omitzero"`
	//kama:validate const(1, None, "a", {"b": []})
	Fixed jsontext.Value `json:"fixed"`
	//kama:validate not cmp(ge=5)
	Below Loose `json:"below"`
	//kama:validate format("uuid")
	Id jsontext.Value `json:"id,omitzero"`
	//kama:validate format("ipv4")
	Ipv4 jsontext.Value `json:"ipv4,omitzero"`
	//kama:validate format("ipv6")
	Ipv6 jsontext.Value `json:"ipv6,omitzero"`
}
