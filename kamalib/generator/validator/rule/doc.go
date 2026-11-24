// Package rule holds the validators //kama:validate names and the API a
// validator is written against. Package validator generates the Validate
// methods applying them.
//
// # Registry
//
// DefaultRegistry returns a new registry holding the built-in validators.
// The built-in validators use the same API a caller does. A Validator
// names itself and parses an arg into a Rule. A Rule checks a type and
// emits an expression of type error. Emit may declare helper functions
// through the Emitter. The Emitter puts them in the helpers file of the
// package. The expression may refer to the bool parameter stopOnFirstError
// of the function it lands in. Emitter.StopOnFirstError spells the name the
// function knows it by. Clone a registry before changing it. Registering a
// validator under the name of a built-in replaces both its arg and its
// code.
//
// A rule writes code with the steps of package genhelper, rendered with
// Context. A Gen renders an expression, and a Step renders the declaration
// of a helper. Each formats source text with the arguments of
// genhelper.Expr. A genhelper.Shadow argument declares a parameter or a
// local under exactly its name, and a genhelper.Ref argument spells it.
// Importer.Import spells a package by path and imports it into the file
// the text lands in.
//
// # Writing a validator
//
// A validator has three parts, and each built-in one lays them out in its
// own file in this order. First, the Validator type names itself. Second,
// an options struct holds the decoded arg, and Parse of the Validator
// decodes arg into it. Parse reports the arg errors that hold for any
// type. Those errors wrap ErrArgs. Third, the options struct is the Rule:
// it checks a type with Check and emits the expression with Emit. The
// generator parses arg again for every field the validator applies to,
// and the options live only for that field. A field names the validator
// below as //kama:validate multiple=3:
//
//	type multiple struct{}
//
//	func (multiple) Name() string { return "multiple" }
//
//	type multipleOptions struct{ N int64 }
//
//	func (multiple) Parse(arg string, _ rule.Resolver) (rule.Rule, error) {
//		n, err := strconv.ParseInt(arg, 10, 64)
//		if err != nil || n <= 0 {
//			return nil, fmt.Errorf("%w: want a positive integer", rule.ErrArgs)
//		}
//
//		return multipleOptions{N: n}, nil
//	}
//
//	func (multipleOptions) Check(t types.Type) error {
//		if b, ok := t.Underlying().(*types.Basic); !ok || b.Info()&types.IsInteger == 0 {
//			return fmt.Errorf("%s is not an integer type", t)
//		}
//
//		return nil
//	}
//
//	var lang genhelper.Lang[rule.Context]
//
//	func (o multipleOptions) Emit(e rule.Emitter, _ types.Type, v rule.Gen) (rule.Gen, error) {
//		e.Declare("kamaValidateMultiple", func(imp rule.Importer) rule.Step {
//			return lang.Func(
//				lang.Expr("func kamaValidateMultiple(%s, %s int64) error",
//					genhelper.Shadow("got"), genhelper.Shadow("n")),
//				lang.If(
//					lang.Expr("%s%%%s != 0", genhelper.Ref("got"), genhelper.Ref("n")),
//					lang.Stmt(`return %s.New("want a multiple")`, imp.Import("errors")),
//					nil,
//				),
//				lang.Stmt("return nil"),
//			)
//		})
//
//		return lang.Expr("kamaValidateMultiple(int64(%s), %d)", v, o.N), nil
//	}
//
//	reg := rule.DefaultRegistry().Clone()
//	reg.Register(multiple{})
//	files, warnings, err := validator.Generate(pkgs, validator.Options{Registry: reg})
//
// Format holds its formats in a map. A caller adds a format by registering
// a Format holding DefaultFormats and the new FormatHelper, as the doc
// comment of Format shows.
//
// A rule validating a struct as a whole implements StructRule. The doc
// comment of a struct type takes such rules only. Struct of the rule
// receives the struct as a Struct, which names its fields by the JSON
// members they encode as, and returns the Rule the generator applies to
// the struct value.
//
// A rule of a field reading the other fields of its struct implements
// SiblingRule. Siblings of the rule receives the struct as a Struct and
// returns the Rule the generator applies to the field. The generator
// applies it to the field itself, never to the value a pointer or an
// option holds, and adds no path segment of the field to its errors.
//
// # Writing a combinator
//
// A combinator is a validator whose arg names other validators, as
// and(cmp=">=1", required) does. Parse receives the Resolver of the
// registry in use and passes arg to ParseOperands. ParseOperands splits
// arg with dircmnt.ParseCallArgs and parses each operand through the
// Resolver, user validators and replaced built-ins included. The options
// hold the operands. Check delegates to Rule.Check of each operand, and
// Emit to Rule.Emit, combining the expressions of the operands into one
// expression of type error, usually a call of a helper taking their
// errors. A user combinator replaces a built-in one by its name, like any
// other validator. The built-in combinators turn into a StructRule or a
// SiblingRule when an operand is one. A user combinator taking such
// operands implements StructRule or SiblingRule itself.
//
// # Built-in validators
//
// The built-in validators report leaf errors created by validation.New with
// the sentinels listed here. Callers test the kind of a failure with
// errors.Is:
//
//	required              validation.ErrRequired
//	zero                  validation.ErrZero
//	nil                   validation.ErrNil
//	null                  validation.ErrNull
//	regexp=<pattern>      validation.ErrPattern
//	length="<op><n> ..."  validation.ErrLength
//	runes="<op><n> ..."   validation.ErrLength
//	cmp="<op><v> ..."     validation.ErrCompare
//	const="<v> ..."       validation.ErrConst
//	multipleof=<n>        validation.ErrMultiple
//	unique                validation.ErrUnique
//	format=<name>         validation.ErrFormat
//	requires="<name> ..." validation.ErrRequired
//	members="<op><n> ..." validation.ErrLength
//	closed                validation.ErrClosed
//	and(<v>, ...)         the errors of the operands
//	or(<v>, ...)          validation.ErrOr
//	not(<v>)              validation.ErrNot
//	oneof(<v>, ...)       validation.ErrOneOf
//	if(<c>, <t>[, <e>])   the errors of the operand t or e
//
// required takes no arg. The value is not the zero value: an opt.Option
// is some, an und.Und or an elastic.Elastic is defined, a pointer, slice,
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
// regexp takes its whole arg as the pattern, as in regexp="^[a-z]+$". A
// quoted or raw string lets the pattern hold spaces, = and quotes, as in
// regexp=`^a="b c"$`. The value of a string or []byte underlying type
// matches the RE2 pattern. The
// generator compiles the pattern to reject a bad one, and the generated
// code compiles it once into a package-level variable.
//
// length takes one or more comparisons <op><n> with the operators >, >=,
// ==, !=, <= and <, and a non-negative integer literal n, as in
// length=">=1 <=10".
// n may carry a unary plus. len of the value satisfies every comparison.
// It applies to string, slice, array, map and channel underlying types.
// The length of a string is its number of bytes.
//
// runes takes the arg of length. The number of runes of the value of a
// string underlying type, as utf8.RuneCountInString counts them,
// satisfies every comparison. A JSON string counts its length the same
// way. runes does not apply to []byte, which encodes to a JSON string of
// another length.
//
// cmp takes one or more comparisons <op><v>, where v is a Go literal of the
// type of the value: a number, a quoted string, true or false, as in
// cmp=">=0 <100" or cmp=`>"b"`. The value satisfies every comparison. It applies to
// integer, float and string underlying types, and with == and != only to
// bool underlying types. A literal the type cannot represent is an error,
// except a number on an integer type, which compares exactly: >=1.5 holds
// for the values >=2, <=1e30 for every value of int64 and ==1.5 for none.
// A comparison no value satisfies reports the literal as written.
//
// const takes one or more Go literals of the type of the value, as in
// const="1 2 3" or const=`"red" "dark blue"`. The value equals one of them.
// It applies to bool, numeric and string underlying types. A string type
// takes quoted literals only and any other type bare literals only.
// const=`"1"` and const=1 therefore never mean the same.
//
// multipleof takes one positive integer or float literal n, as in
// multipleof=2 or multipleof=0.01. The value divided by n is an integer.
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
// format takes one format name, as in format=date. The value of a string underlying type is
// of that format. DefaultFormats holds the built-in formats: date parses
// with time.DateOnly, date-time parses with time.RFC3339, and url parses
// with net/url and has a scheme and a host.
//
// requires takes one or more JSON member names of sibling fields, as in
// requires="city zip". When the field is present, each sibling named is
// present too, as JSON Schema dependentRequired tests. Present means what
// required tests, so an und.Und holding null is absent. The field and each
// sibling must be of a type required applies to. A name no sibling field
// encodes as is an error. The error of a missing sibling is located at the
// sibling. requires applies to a pointer or an option field itself and
// never to the value it holds.
//
// members takes the arg of length and is written in the doc comment of a
// struct type, as in //kama:validate members=">=1 <=3". The number of JSON
// members of the value satisfies every comparison, as JSON Schema
// minProperties and maxProperties count them. A field of an option type or
// a nillable type counts when required would pass on it. Any other field
// encoded as a member always counts, and so does each entry of the
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
// and, or, not, oneof and if take operands: validators written as the args
// of //kama:validate are, separated by commas, as in or(cmp=">=10",
// const="0"). They apply to a type every operand applies to. and holds
// when every operand holds and returns their errors, joined or with
// stopOnFirstError the first one. and() always holds. or holds when an
// operand holds. Its error, none of n alternatives holds, carries the
// error of every operand as a cause, which validation.Causes returns.
// errors.Is does not reach the causes. or() never holds. not takes one
// operand and holds when it fails. Its error spells the operand. oneof
// holds when exactly one operand holds. When none does, its error carries
// the errors of the operands as causes. When several do, it spells them.
// if takes cond, then and an optional else. When cond holds, it returns
// the error of then, otherwise that of else, or nil without else. The
// error of cond never surfaces. The operands of a combinator all run, so
// the generated code holds no closures. A combinator with a requires
// operand is bound to the siblings of its field, and one with a members
// operand to its struct, as package validator describes. The spelling of
// an operand in an error message quotes its value, as in
// not(const=5) reporting want the value to fail const="5".
//
// A validator with several comparisons, as length, runes, cmp and members
// take, checks them all. With stopOnFirstError true it returns the first
// failure only, otherwise every failure joined.
package rule
