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
//	func (multiple) Parse(arg string) (rule.Rule, error) {
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
//	cmp="<op><v> ..."     validation.ErrCompare
//	const="<v> ..."       validation.ErrConst
//	format=<name>         validation.ErrFormat
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
// cmp takes one or more comparisons <op><v>, where v is a Go literal of the
// type of the value: a number, a quoted string, true or false, as in
// cmp=">=0 <100" or cmp=`>"b"`. The value satisfies every comparison. It applies to
// integer, float and string underlying types, and with == and != only to
// bool underlying types. A literal the type cannot represent is an error.
//
// const takes one or more Go literals of the type of the value, as in
// const="1 2 3" or const=`"red" "dark blue"`. The value equals one of them.
// It applies to bool, numeric and string underlying types. A string type
// takes quoted literals only and any other type bare literals only.
// const=`"1"` and const=1 therefore never mean the same.
//
// format takes one format name, as in format=date. The value of a string underlying type is
// of that format. DefaultFormats holds the built-in formats: date parses
// with time.DateOnly, date-time parses with time.RFC3339, and url parses
// with net/url and has a scheme and a host.
//
// A validator with several comparisons, as length and cmp take, checks
// them all. With stopOnFirstError true it returns the first failure only,
// otherwise every failure joined.
package rule
