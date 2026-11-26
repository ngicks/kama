// Package ext holds the API an extension of //kama:validate is written
// against and the registry of extensions. Package validator generates the
// Validate methods applying them.
//
// # Extension packages
//
// An extension package provides validators and type supports. It
// registers them into the default registry with Register and
// RegisterTypeSupport in its init function. Package basic holds the
// built-in validators, and package opt holds the type support of
// github.com/ngicks/option. Package validator imports both with blank
// imports, so that a program generating validators gets them. A third
// party adds an extension package the same way, and a program imports
// that package with a blank import too:
//
//	package myext
//
//	func init() {
//		ext.Register(multiple{})
//		ext.RegisterTypeSupport(dequeSupport{})
//	}
//
// Register and RegisterTypeSupport panic on a name that is registered
// already. Two extension packages must not claim one name.
//
// # Type supports
//
// A TypeSupport tells about the values a named type of a package holds,
// which the generator cannot see through its fields. Pointers, slices,
// arrays and maps need none. A support implements one or more of these
// interfaces, and each method reports false for a type the support does
// not claim:
//
//   - Holder: the one value a type may hold, as opt.Option holds one. The
//     generator walks into it under its guard, and a rule rejecting the
//     type applies to the value held.
//   - Container: the elements of a type, ranged over and located by index
//     or key, as elastic.Elastic holds its elements.
//   - Composite: several values side by side, each of its own type, under
//     its own guard and located by its own segment, as either.Either holds
//     a left or a right value and tuple.Tuple2 holds two. A rule rejecting
//     the type does not apply to them.
//   - PresenceTester, ZeroTester and NullTester: how required, zero and
//     null test a value of the type.
//
// Each held value, each element and each value of a Composite has a role
// name, such as "t", "left" or "v1". A role names the value among the
// values a type holds. The role names a support gives are part of its API.
// A role name is a Go identifier, and it is not hook, pre, post or the
// name of a validator. Roles lists the roles of a type: e for the elements
// of a slice or an array, k and v for the keys and the values of a map,
// and the roles of the supports. RoleValue tells how the values of a role
// are reached: their type, the guard, the value expression or the range
// expression, and the segment locating them. A caller applying a rule to
// the values of a role applies it to that value under that guard, or in
// that loop, and locates its errors by that segment.
//
// The generator asks the supports at the point it needs an answer, in the
// order they were registered, and the first support claiming a type
// answers. HeldValue, HolderOf, ContainerOf, Presence, Zero, Null and
// Dependants ask the supports of a Resolver. A rule keeps the Resolver its
// Parse received to ask them. Target selection walks into the types
// Dependants tells, so a type argument holding something to validate
// makes the type holding the claimed type a target.
//
// A support of a double-ended queue of a third party package, whose method
// Iter yields its elements by index, is a Container:
//
//	type dequeSupport struct{}
//
//	func (dequeSupport) Name() string { return "deque" }
//
//	func (dequeSupport) Elements(
//		t types.Type,
//		v ext.Gen,
//	) (string, types.Type, ext.Gen, ext.Segment, bool) {
//		named, ok := types.Unalias(t).(*types.Named)
//		if !ok || named.Obj().Pkg() == nil ||
//			named.Obj().Pkg().Path() != "github.com/gammazero/deque" ||
//			named.Obj().Name() != "Deque" {
//			return "", nil, nil, 0, false
//		}
//
//		return "e", named.TypeArgs().At(0), lang.Expr("%s.Iter()", v), ext.SegmentIndex, true
//	}
//
// The generated Validate then ranges over v.Iter() and validates each
// element of a type with Validate, locating its errors by index.
//
// # Registry
//
// DefaultRegistry returns a new registry holding the validators the
// extension packages registered. The built-in validators use the same API
// a caller does. A Validator names itself and parses an arg into a Rule. A
// Rule checks a type and emits an expression of type error. Emit may
// declare helper functions through the Emitter. The Emitter puts them in
// the helpers file of the package. The expression may refer to the bool
// parameter stopOnFirstError of the function it lands in.
// Emitter.StopOnFirstError spells the name the function knows it by.
// NewRegistry returns a registry of the given validators only. A caller
// changing a registry for one run changes a clone of it. Registering a
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
// A validator has three parts, and each built-in one of package basic lays
// them out in its own file in this order. First, the Validator type names itself. Second,
// an options struct holds the decoded arg, and Parse of the Validator
// decodes arg into it. Parse reports the arg errors that hold for any
// type. Those errors wrap ErrArgs. Third, the options struct is the Rule:
// it checks a type with Check and emits the expression with Emit. The
// generator parses arg again for every field the validator applies to,
// and the options live only for that field. A field names the validator
// below as //kama:validate multiple(3):
//
//	type multiple struct{}
//
//	func (multiple) Name() string { return "multiple" }
//
//	type multipleOptions struct{ N int64 }
//
//	func (multiple) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
//		var args struct {
//			N int64 `json:"n"`
//		}
//
//		if _, err := (ext.Params{Positional: []string{"n"}}).Bind(r, c, &args); err != nil {
//			return nil, err
//		}
//
//		if args.N <= 0 {
//			return nil, fmt.Errorf("%w: want a positive integer", ext.ErrArgs)
//		}
//
//		return multipleOptions{N: args.N}, nil
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
//	var lang genhelper.Lang[ext.Context]
//
//	func (o multipleOptions) Emit(e ext.Emitter, _ types.Type, v ext.Gen) (ext.Gen, error) {
//		e.Declare("kamaValidateMultiple", func(imp ext.Importer) ext.Step {
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
//	reg := ext.DefaultRegistry().Clone()
//	reg.Register(multiple{})
//	files, warnings, err := validator.Generate(pkgs, validator.Options{Registry: reg})
//
// basic.Format holds its formats in a map. A caller adds a format by
// registering a basic.Format holding basic.DefaultFormats and the new
// basic.FormatHelper, as the doc comment of basic.Format shows.
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
// registry in use and passes arg to basic.ParseOperands, which splits
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
// # Raw JSON text
//
// IsRawJson tells a type whose values are raw JSON text: jsontext.Value, or
// a type of the underlying type []byte declaring the method
// RawJson() jsontext.Value. A validator applying to such a value accepts
// the type in Check and tests the value with the semantics of JSON Schema:
// it tests the values of the JSON kinds it applies to and passes any
// other, as minimum passes a string. A user validator implements the same
// capability by the same means. The built-in validators call the
// functions of the package at RawJsonPath, which compare numbers exactly
// and JSON values as JSON values.
//
// A combinator needs no mode of its own for raw JSON text: an operand of
// another kind holds, so not(cmp=">=5") fails on "a", as not with minimum
// rejects a string in JSON Schema, and or(cmp=">=5", kind=string) holds on
// it.
//
// # Reaching into raw JSON text
//
// A validator reaching into raw JSON text applies its operands to the
// members or the elements of the value, as the keywords of JSON Schema
// holding subschemas do: props, items, prefix, contains, keys, unevalprops
// and unevalitems. Its operands check jsontext.Value, the type RawJson
// returns, and the generated code passes them each member or element in a
// function literal. ref and decode apply a type of the generated package
// to the value, as $ref applies a schema.
//
// A validator reporting what it evaluated implements Annotator. unevalprops
// and unevalitems run their first operand, the scope, through Annotate
// with a record of their own, then check the members or the elements the
// record lacks. and passes the record to every operand, or and oneof to
// each operand in a call of rawjson.Branch, which keeps what an operand
// evaluated only when it holds, and if runs only the branch taken through
// rawjson.If. not records nothing, as JSON Schema drops the annotations of
// not. A user validator reaching into raw JSON text implements Annotator to
// take part.
package ext
