package basic

import (
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// init registers the built-in validators: required, zero, nil, null,
// regexp, length, runes, cmp, const, multipleof, unique, format with
// DefaultFormats, requires, members, closed, kind, ref and decode, always,
// never and oneof, the validators reaching into raw JSON text props,
// items, prefix, contains, keys, unevalprops and unevalitems, and json. It
// registers the operators and, or, not and the conditional by the kinds of
// their nodes.
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
		Always{},
		Never{},
		OneOf{},
		Props{},
		Items{},
		Prefix{},
		Contains{},
		Keys{},
		UnevalProps{},
		UnevalItems{},
		Json{},
	)

	ext.RegisterOperator(expr.And{}, And)
	ext.RegisterOperator(expr.Or{}, Or)
	ext.RegisterOperator(expr.Not{}, Not)
	ext.RegisterOperator(expr.Cond{}, If)
}
