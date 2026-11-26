// Package basic holds the built-in validators of //kama:validate. Each
// validator lays out in its own file, and the package registers them all
// into the default registry of package ext in its init function. A
// program importing package validator imports this package with it.
//
// # Built-in validators
//
// The built-in validators report leaf errors created by validator.New with
// the sentinels listed here. Callers test the kind of a failure with
// errors.Is:
//
//	required                 validator.ErrRequired
//	zero                     validator.ErrZero
//	nil                      validator.ErrNil
//	null                     validator.ErrNull
//	regexp("<pattern>")      validator.ErrPattern
//	length(ge=<n>, ...)      validator.ErrLength
//	runes(ge=<n>, ...)       validator.ErrLength
//	cmp(ge=<v>, ...)         validator.ErrCompare
//	const(<v>, ...)          validator.ErrConst
//	multipleof(<n>)          validator.ErrMultiple
//	unique                   validator.ErrUnique
//	format("<name>")         validator.ErrFormat
//	requires("<name>", ...)  validator.ErrRequired
//	members(ge=<n>, ...)     validator.ErrLength
//	closed                   validator.ErrClosed
//	kind("<kind>", ...)      validator.ErrKind
//	a and b                  the errors of the operands
//	a or b                   validator.ErrOr
//	not a                    validator.ErrNot
//	oneof(a, ...)            validator.ErrOneOf
//	t if c else e            the errors of the operand t or e
//	always                   none
//	never                    validator.ErrOr
//	ref("<type>")            the errors of Validate of type
//	decode("<type>")         validator.ErrDecode
//	props(<entry>, ...)      the errors of the operands, at their members
//	items(a, ...)            the errors of the operands, at their elements
//	prefix(a, ...)           the errors of the operands, at their elements
//	contains(a, ..., ge=<n>) validator.ErrContains
//	keys(a, ...)             the errors of the operands, at their members
//	unevalprops(<s>, ...)    the errors of s and of the operands
//	unevalitems(<s>, ...)    the errors of s and of the operands
//	json(a, ...)             the errors of the operands, validator.ErrEncode
//
// A comparison is a keyword arg: gt, ge, lt, le, eq and ne compare with >,
// >=, <, <=, == and !=. One positional arg compares with ==, as in
// length(3), and ne takes a list of values too, as in length(ne=[2, 3]). A
// keyword arg a validator does not take is reported as a warning and left
// out.
//
// required takes no arg, except member names on raw JSON text, as told
// below. The value is not the zero value: an opt.Option
// is some, an und.Und or an elastic.Elastic is not undefined, so null
// counts as a value, as a JSON member holding null is present, a pointer, slice,
// map, channel, function or interface is not nil, a type with a method
// IsZero() bool is not zero by it, and any other comparable type, a
// comparable type parameter included, does not equal its zero value.
// IsZero counts when the type declares it or when embedded fields promote
// it without a pointer or an interface on the way. IsZero promoted through
// an embedded pointer, as in struct{ *time.Time }, would panic on the zero
// value. Such a type is compared with its zero value instead.
//
// zero takes no arg and applies to the types required applies to. The
// value is the zero value: a nillable value is nil, a type with IsZero,
// the option types among them, is zero by it, and any other comparable
// value equals its zero value. IsZero counts as for required.
//
// nil takes no arg. The value of a pointer, slice, map, channel, function
// or interface type is nil.
//
// null takes no arg. The value is null by its method IsNull() bool, as an
// und.Und or an elastic.Elastic tells. The method must be declared or
// promoted as for IsZero of required. null rejects type parameters and
// nillable types: pointer, slice, map, channel, function and interface
// types. Their nil value cannot call IsNull. A pointer therefore gets the
// validator on its pointee, and a nil pointer passes.
//
// regexp takes the pattern as a string, as in regexp(r"^[a-z]+$"). A raw
// string keeps every backslash. The value of a string or []byte underlying
// type matches the RE2 pattern. The
// generator compiles the pattern to reject a bad one, and the generated
// code compiles it once into a package-level variable.
//
// length takes one or more comparisons with non-negative integers, as in
// length(ge=1, le=10). The length of the value satisfies every comparison.
// It applies to string, slice, array, map and channel underlying types, and
// to a type an ext.LengthTester claims, as elastic.Elastic. The length of a
// string is its number of bytes.
//
// runes takes the args of length. The number of runes of the value of a
// string underlying type, as utf8.RuneCountInString counts them,
// satisfies every comparison. A JSON string counts its length the same
// way. runes does not apply to []byte, which encodes to a JSON string of
// another length.
//
// cmp takes one or more comparisons with values of the type of the value: a
// number, a string, True or False, as in cmp(ge=0, lt=100) or cmp(gt="b").
// The value satisfies every comparison. It applies to
// integer, float and string underlying types, and with == and != only to
// bool underlying types. A literal the type cannot represent is an error,
// except a number on an integer type, which compares exactly: ge=1.5 holds
// for the values >=2, le=1e30 for every value of int64 and eq=1.5 for none.
// A comparison no value satisfies reports the literal as written.
//
// const takes one or more literals of the type of the value, as in
// const(1, 2, 3) or const("red", "dark blue"). The value equals one of
// them. It applies to bool, numeric and string underlying types. A string
// type takes strings only and any other type takes no string. const("1")
// and const(1) therefore never mean the same.
//
// multipleof takes one positive number n, as in multipleof(2) or
// multipleof(0.01). The value divided by n is an integer.
// It applies to integer and float underlying types. An integer value is
// tested with % by the numerator of n in lowest terms, which must be a
// value of the type. A float value is tested exactly, as the shortest
// decimal reading back as the value, so float64(0.0075) is a multiple of
// 0.0001. NaN and the infinities are multiples of nothing.
//
// unique takes no arg. No two elements of the value of a slice or array
// underlying type are equal by value. Elements of a comparable type holding
// no pointer, interface or type parameter compare with ==. Other elements
// compare with reflect.DeepEqual, which follows pointers.
//
// format takes one format name, as in format("date"). The value of a string underlying type is
// of that format. On raw JSON text a string is of that format and a value of another kind
// passes. DefaultFormats holds the built-in formats: date parses with time.DateOnly,
// date-time parses with time.RFC3339, url parses with net/url and has a scheme and a host,
// ipv4 and ipv6 parse with net/netip as an address of that family without a zone, and uuid
// is 32 hexadecimal digits in hyphenated groups of 8, 4, 4, 4 and 12.
//
// requires takes one or more JSON member names of sibling fields, as in
// requires("city", "zip"). When the field is present, each sibling named is
// present too, as JSON Schema dependentRequired tests. Present means what
// required tests, so an und.Und holding null is present. The field and each
// sibling must be of a type required applies to. A name no sibling field
// encodes as is an error. The error of a missing sibling is located at the
// sibling. requires applies to a pointer or an option field itself and
// never to the value it holds.
//
// members takes the args of length and is written in the doc comment of a
// struct type, as in //kama:validate members(ge=1, le=3). The number of JSON
// members of the value satisfies every comparison, as JSON Schema
// minProperties and maxProperties count them. A field of an option type or
// a nillable type whose json tag holds omitzero or omitempty counts when
// required would pass on it. Any other field encoded as a member always
// counts, as encoders write it, as null when it holds no value, and so does each entry of the
// embedded fallback map, the field tagged json:",embed". A fallback of
// another type and a struct inlining an embedded struct are errors.
//
// closed takes no arg and is written in the doc comment of a struct type,
// as in //kama:validate closed. The value holds no JSON member beyond those
// its fields encode as, as JSON Schema additionalProperties false requires:
// the embedded fallback map is empty. Each entry is an error located at
// its member, in the order of the keys. A fallback of another type than a
// map keyed by strings is an error. A struct without one always passes.
//
// kind takes one or more JSON kinds, as in kind("integer", "null"): null,
// boolean, object, array, number, string and integer, a number with a zero
// fraction such as 1.0. It applies to raw JSON text only, whose kind is one
// of them, as JSON Schema type tests.
//
// On raw JSON text cmp and multipleof take JSON numbers and test numbers,
// runes and regexp test strings, length counts the elements of an array,
// members applies outside a struct and counts the members of an object,
// unique and const compare JSON values, so 1 equals 1.0 and objects equal
// regardless of member order, and required takes member names, as in
// required("a", "b"), which an object holds. Each passes a value of another
// kind. A const value on raw JSON text is a JSON value written as a
// literal, None for null, a list for an array and a dict for an object, as
// in const(1, None, "a", {"b": []}).
//
// The operators and, or, not and the conditional, and oneof, take rules as
// operands, as in cmp(ge=10) or const(0). They apply to a type every operand
// applies to, else to the level of a pointer or an option every operand
// applies to, else each operand to its own level, as package validator
// describes. and holds when every operand holds and returns their errors,
// joined or with stopOnFirstError the first one. always holds. or holds
// when an operand holds. Its error, none of n alternatives holds, carries
// the error of every operand as a cause, which validator.Causes returns.
// errors.Is does not reach the causes. never never holds. not holds when
// its operand fails. Its error spells the operand. oneof holds when exactly
// one operand holds. When none does, its error carries the errors of the
// operands as causes. When several do, it spells them. then if cond else
// otherwise returns the error of then when cond holds, otherwise that of
// otherwise, nil for always. The error of cond never surfaces. The
// operands of a combinator all run, as the arguments of one helper call. A
// combinator with a requires operand is bound to the siblings of its
// field, and one with a members operand to its struct. The other operands
// of a combinator bound to the siblings apply to the value a pointer or an
// option holds when they reject the field, inside a function literal
// guarding a nil pointer. An error message spells an operand as the
// directive does, as not const(5) reporting want the value to fail
// const(5).
//
// ref takes the name of a type of the generated package defined over raw
// JSON text, as in ref("Tree"), and calls Validate of the value converted
// to it. decode takes the name of a type whose decoder validates, as in
// decode("Pet"), and decodes the value into it. Both apply to raw JSON text
// only and record nothing for unevalprops and unevalitems.
//
// props takes entries: field("<name>", <v>, ...) applies the operands to
// the member name when present, match("<pattern>", <v>, ...) to each
// member whose name the RE2 pattern matches, and rest(<v>, ...) to each
// member neither names nor matches, as JSON Schema properties,
// patternProperties and additionalProperties do. items applies its
// operands to each element. prefix applies its operand i to the element i
// and the operands of a last rest(...) to each later element. contains
// takes the comparisons of length as keyword args, ge=1 when there is none,
// which the number of elements passing every operand satisfies, as in
// contains(kind("string"), ge=2, le=3). keys applies its operands to the
// name of each member as a JSON string. unevalprops and unevalitems take
// the scope first. Each applies to raw JSON text only and passes a value
// of another kind.
//
// json takes the validators of raw JSON text as operands and applies them
// to the value of any other type encoded as JSON, as in
// //kama:validate json(not required("x"), props(match("^x-", kind("string")))) on
// a struct. It applies to the value a pointer, an opt.Option or an und.Und
// holds, so a value holding none is not checked. It is a StructRule too, so
// the doc comment of a struct type takes it. A value that does not encode
// is an error wrapping validator.ErrEncode.
//
// A validator with several comparisons, as length, runes, cmp and members
// take, checks them all. With stopOnFirstError true it returns the first
// failure only, otherwise every failure joined.
package basic
