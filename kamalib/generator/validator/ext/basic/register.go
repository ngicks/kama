package basic

import "github.com/ngicks/kama/kamalib/generator/validator/ext"

// init registers the built-in validators: required, zero, nil, null,
// regexp, length, runes, cmp, const, multipleof, unique, format with
// DefaultFormats, requires, members, closed, kind, ref and decode, the
// combinators and, or, not, oneof and if, the validators reaching into raw
// JSON text props, items, prefix, contains, keys, unevalprops and
// unevalitems, and json.
func init() {
	ext.Register(
		Required{},
		Zero{},
		Nil{},
		Null{},
		Regexp{},
		Length{},
		Runes{},
		Cmp{},
		Const{},
		MultipleOf{},
		Unique{},
		Format{Formats: DefaultFormats()},
		Requires{},
		Members{},
		Closed{},
		Kind{},
		Ref{},
		Decode{},
		And{},
		Or{},
		Not{},
		OneOf{},
		If{},
		Props{},
		Items{},
		Prefix{},
		Contains{},
		Keys{},
		UnevalProps{},
		UnevalItems{},
		Json{},
	)
}
