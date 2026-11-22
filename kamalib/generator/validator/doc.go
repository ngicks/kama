// Package validator generates Validate methods for data models.
//
// A target T gets one method:
//
//	func (v T) Validate(stopOnFirstError bool) error
//
// Validate runs the validators that //kama:validate:<name> directives put
// on the fields of T, validates the values the fields hold through their
// own Validate, and calls a hook method per field. It returns errors of
// package github.com/ngicks/kama/kamalib/pkg/validation, located by the
// json names of the fields. Proc runs Generate as the workflow step kind
// validate.generate.
//
// A generic target gets one more parameter per type parameter, in order:
//
//	func (v Box[T, U]) Validate(
//		stopOnFirstError bool,
//		validateT func(v T, stopOnFirstError bool) error,
//		validateU func(v U, stopOnFirstError bool) error,
//	) error
//
// validateT validates the values of type T that a Box holds. A nil
// validateT leaves them unchecked. The parameter is named validate followed
// by the name of the type parameter. A blank type parameter gets a fresh
// name. Because of these parameters no single interface describes every
// Validate, and the generator finds callable methods by their shape.
//
// A type parameter named v, stopOnFirstError, errs or err collides with an
// identifier the generated Validate declares. A type parameter named like a
// predeclared identifier the generated code spells, or like a helper the
// generated code of its package calls, shadows it. Each is an error
// wrapping ErrConflict.
//
// # Targets
//
// The generator takes targets from the loaded packages only, by directives
// or by the option file entries standing for them:
//
//   - //kama:validate in the doc comment of a type marks it. In the doc
//     comment of a type declaration with several specs, as in
//     type ( A struct{}; B struct{} ), it marks every type of the
//     declaration, and so do the other directives of types.
//   - //kama:validate file in a free-floating comment, one that is not the
//     doc or line comment of any declaration, marks every type of the file.
//   - //kama:validate package in a free-floating comment marks every type
//     of the package. The package clause doc comment is free-floating.
//   - //kama:validate ignore in the doc comment of a type exempts it from
//     file and package marks and from being a dependant. A type both marked
//     and ignored is an error.
//
// Every type of a loaded package that holds a marked type or another
// dependant is a dependant, and a dependant is a target too. A type holds
// another through struct fields, the fields of struct type literals,
// slice and array elements, map values, pointers, aliases and the type
// argument of opt.Option, und.Und and elastic.Elastic of
// github.com/ngicks/option. Map keys, channel elements and the type
// arguments of other generic types make no dependant. The user did not
// mark a dependant. Its hooks are therefore off by default. Its fields may
// still carry validators.
//
// A marked type does not make the types of its fields targets. A field of
// a type that is neither a target nor an implementor gets its own
// validators and hook only.
//
// Only defined types whose underlying type is a struct, a slice or a map
// get a Validate. File marks, package marks and the search for dependants
// skip other types silently. An explicit //kama:validate on one, or on an
// alias, is an error wrapping ErrUnsupported. Types of packages outside the
// run are never targets.
//
// A named array or pointer type of the run holding something to validate,
// as in type Pair [2]Inner or type PI *Inner, gets no Validate either. A
// field of a struct type or a slice or map type holding such a type is an
// error wrapping ErrUnsupported. The error applies to a type that would be
// a dependant through it as well. So does a type argument reaching such a
// type in a Validate. Use an unnamed array or pointer type, or a wrapper
// struct, in its place.
//
// Declarations whose doc comment carries the genmark marker of a
// registered generator are never targets. The generator reads files
// emitted earlier like any other file and generates them again.
// Options.Markers tells the registered generators.
//
// # Implementors
//
// An implementor is a named type with a hand-written Validate that
// generated code can call. Its method must be declared on the value
// receiver of the type itself, not promoted from an embedded field, and
// have the signature
//
//	func(bool, func(T1, bool) error, ..., func(Tn, bool) error) error
//
// with one function parameter per type parameter T1, ..., Tn of the type,
// in order, and nothing else. Validate() error is no implementor, and any
// other signature is ignored. A method of a loaded package carrying the
// marker of a registered generator does not count. A stale generated file
// therefore does not keep a type validated. A generated Validate of a
// package outside the run counts like a hand-written one. An interface
// type is never an implementor. Its nil value would panic.
//
// An implementor of a loaded package gets no Validate, and marking it is an
// error wrapping ErrConflict. The types holding it become dependants. An
// implementor of a package outside the run makes the types of the run
// holding it targets in the same way. A type whose Validate of that shape
// is declared on the pointer receiver only is an error wrapping
// ErrUnsupported where Validate would call it. Generated code holds values,
// which cannot call such a method in general. A target with a Validate the
// generator did not write and that is no implementor is an error wrapping
// ErrConflict.
//
// Validate calls x.Validate(stopOnFirstError, a1, ..., an) on a value x of
// a target or an implementor. ai is built from the i-th type argument of
// the type of x:
//
//   - a type parameter P of the receiver passes the own validateP of the
//     receiver.
//   - a target or an implementor X[...] passes a function literal calling
//     Validate of X, built the same way.
//   - another type holding something to validate, such as []Tag, passes a
//     function literal walking it.
//   - any other type, such as int, passes nil.
//
// # What Validate does
//
// For a struct target, Validate visits the fields in declaration order. It
// skips blank fields. It also skips the unexported fields of a struct type
// of another package, as in type T other.S. Generated code cannot spell
// them. For each other field it runs, in this order:
//
//  1. The validators of the directives on the field, in the order the
//     directive names first appear. Several lines of one directive on one
//     field count as one line: their args are joined in source order.
//  2. The nested validation of the values the field holds. Validate walks
//     through pointers, slices, arrays, map values, the fields of struct
//     type literals and the option types to each value of a target, of an
//     implementor or of a type parameter. A nil pointer, slice or map, a
//     none opt.Option, an undefined or null und.Und and a null element of
//     an elastic.Elastic hold nothing and are skipped. The walk never goes
//     into the underlying type of a named type. A struct, slice or map type
//     of the run holding something to validate is a target itself. A named
//     array or pointer type of the run holding something to validate is an
//     error, as Targets describes.
//  3. The hook of the field, described under Hooks.
//
// Validate wraps every error of these steps with the path segment of the
// field. Nested validation adds an index segment per slice, array and
// elastic.Elastic element and a key segment per map value. With
// stopOnFirstError true, Validate returns the first error. Otherwise it runs
// every step and returns the errors joined by validation.Join, or nil when
// there are none. It passes stopOnFirstError unchanged to nested Validate
// calls, to hooks and to the validateT functions.
//
// Validate visits the keys of a map in sorted order when the key type is an
// integer or string kind or a named type of one. The first error and the
// order of the errors are then stable. It ranges over other maps directly,
// float keys included. A lookup by a NaN key never finds its value. Sorting
// the keys would therefore lose that value.
//
// A slice or map target gets a Validate walking its elements as step 2
// does, with the index or key as the path segment. Validators and hooks do
// not apply to it. A target with nothing to validate gets a Validate
// returning nil.
//
// # Path segments
//
// The path segment of a field is the name in its json tag when the tag has
// one and is not "-". Otherwise it is the Go name of the field. An embedded
// struct, or pointer to a struct, without a json name adds no segment.
// Encoders inline its fields. With a json name it adds that name. An
// embedded field of another type adds its Go name.
//
// # Hooks
//
// The hook of a field F is a method the user writes on the target T:
//
//	func (v T) validateF(stopOnFirstError bool) error
//
// Its name is validate followed by the field name with its first letter
// upper-cased. A field Name and a field name both map to the hook
// validateName. Two fields of one struct whose hooks are on and share a
// name are an error wrapping ErrConflict. Turn the hook of one of them off.
// The hook must be callable on a value of T. A hook on the pointer receiver
// is an error. Validate wraps the error of the hook with the path segment
// of the field. A plain error of a hook is therefore located at the field.
//
// The hook of each field of a marked type is on by default. File and
// package marks and option file entries count as marks. The hooks of a
// dependant are off by default. A field whose hook is on and missing, or
// has another signature, is a generation error wrapping ErrHook. The error
// names the type, the field, the expected signature and the directive
// turning the hook off. These fuses override the default:
//
//   - //kama:validate:hook off in the doc comment of a type turns the hooks
//     of all its fields off, and //kama:validate:hook on turns them on.
//   - //kama:validate:hook off on a field turns its hook off, and
//     //kama:validate:hook on turns it on.
//
// A field fuse wins over a type fuse, and a type fuse wins over the
// default. A dependant opts in with //kama:validate:hook on, and a field
// opts back in under a type-level off. A hook fuse on a type or a field of
// a type that gets no Validate is an error wrapping ErrDirective.
//
// # Validators
//
// A directive //kama:validate:<name> <args> on a struct field of a target
// runs the validator registered under name. dircmnt.ParseArgsKeyValue
// parses the args as space-separated bare or quoted tokens. A bare token
// starting with <, >, = or ! is one comparison, as in >=1 or =="a b".
// Several validators on one field must all pass. An unknown name is an
// error wrapping ErrDirective. A validator on a field of a type that gets
// no Validate is an error wrapping ErrDirective. //kama:validate on a field
// and //kama:validate:<name> on a type or a free-floating comment are
// errors wrapping ErrDirective too. The hook fuses are the only
// //kama:validate:<name> directives a type takes. A validator applied to a
// type it cannot handle, or with args it does not accept, is an error
// wrapping ErrUnsupported that names the field, the directive and the type.
//
// The generator applies a validator to the type of the field first. When
// the validator rejects that type and the field is a pointer, an
// opt.Option or an und.Und, it applies the validator to the value held,
// under a guard that skips the validator when no value is held. The
// generator reports an error wrapping ErrArgs without that second attempt.
// Such an error comes from the args alone. required, zero and nil
// therefore look at a pointer itself, and every other validator applies to
// the pointee of a non-nil pointer. elastic.Elastic holds no single value
// and takes required, zero and null only.
//
// The built-in validators report leaf errors created by validation.New with
// the sentinels listed here. Callers test the kind of a failure with
// errors.Is:
//
//	required            validation.ErrRequired
//	zero                validation.ErrZero
//	nil                 validation.ErrNil
//	null                validation.ErrNull
//	regexp "<pattern>"  validation.ErrPattern
//	length <op><n> ...  validation.ErrLength
//	cmp <op><v> ...     validation.ErrCompare
//	const <v> ...       validation.ErrConst
//	format <name>       validation.ErrFormat
//
// required takes no args. The value is not the zero value: an opt.Option
// is some, an und.Und or an elastic.Elastic is defined, a pointer, slice,
// map, channel, function or interface is not nil, a type with a method
// IsZero() bool is not zero by it, and any other comparable type, a
// comparable type parameter included, does not equal its zero value.
// IsZero counts when the type declares it or when embedded fields promote
// it without a pointer or an interface on the way. IsZero promoted through
// an embedded pointer, as in struct{ *time.Time }, would panic on the zero
// value. Such a type is compared with its zero value instead.
//
// zero takes no args and applies to the types required applies to. The
// value is the zero value: a nillable value is nil, a type with IsZero,
// the option types among them, is zero by it, and any other comparable
// value equals its zero value.
//
// nil takes no args. The value of a pointer, slice, map, channel, function
// or interface type is nil.
//
// null takes no args. The value is null by its method IsNull() bool, as an
// und.Und or an elastic.Elastic tells. The method must be declared or
// promoted as for IsZero of required. null rejects type parameters and
// nillable types: pointer, slice, map, channel, function and interface
// types. Their nil value cannot call IsNull. A pointer therefore gets the
// validator on its pointee, and a nil pointer passes.
//
// regexp takes one pattern, quoted when it holds =, spaces or quotes. The
// value of a string or []byte underlying type matches the RE2 pattern. The
// generator compiles the pattern to reject a bad one, and the generated
// code compiles it once into a package-level variable.
//
// length takes one or more comparisons <op><n> with the operators >, >=,
// ==, !=, <= and <, and a non-negative integer literal n, as in >=1 <=10.
// len of the value satisfies every comparison. It applies to string,
// slice, array, map and channel underlying types. The length of a string
// is its number of bytes.
//
// cmp takes one or more comparisons <op><v>, where v is a Go literal of the
// type of the value: a number, a quoted string, true or false, as in
// >=0 <100 or >"b". The value satisfies every comparison. It applies to
// integer, float and string underlying types, and with == and != only to
// bool underlying types. A literal the type cannot represent is an error.
//
// const takes one or more Go literals of the type of the value, as in
// const 1 2 3 or const "red" "dark blue". The value equals one of them. It
// applies to bool, numeric and string underlying types. A string type takes
// quoted literals only and any other type bare literals only. const "1"
// and const 1 therefore never mean the same.
//
// format takes one format name. The value of a string underlying type is
// of that format. DefaultFormats holds the built-in formats: date parses
// with time.DateOnly, date-time parses with time.RFC3339, and url parses
// with net/url and has a scheme and a host.
//
// A validator with several comparisons, as length and cmp take, checks
// them all. With stopOnFirstError true it returns the first failure only,
// otherwise every failure joined.
//
// # Errors of Validate
//
// The errors carry a path from the outermost value to the failing one. The
// functions of package validation query them by RFC 6901 JSON pointer:
//
//	err := user.Validate(false)
//	validation.Pointers(err)               // ["/address/city", "/tags/1/name"]
//	validation.At(err, "/tags/1/name")     // the errors at that field
//	validation.Under(err, "/tags")         // the errors at or below it
//	errors.Is(err, validation.ErrRequired) // true
//
// An error prints as validation failed at .tags[1].name: required: want a
// non-zero value, got "".
//
// # Registry
//
// Validators are entries of a Registry, and Options.Registry selects it.
// DefaultRegistry returns a new registry holding the built-in validators.
// The built-in validators use the same API a caller does. A Validator
// names its directive, checks a type and args, and emits an expression of
// type error. Emit may declare helper functions through the Emitter. The
// Emitter puts them in the helpers file of the package. The expression may
// refer to the bool parameter stopOnFirstError of the function it lands
// in. Emitter.StopOnFirstError returns the name the function spells it by.
// Clone a registry before changing it. Registering a validator under the
// name of a built-in replaces both its args and its code:
//
//	type even struct{}
//
//	func (even) Name() string { return "even" }
//
//	func (even) Check(t types.Type, args []dircmnt.KeyValue) error {
//		if len(args) > 0 {
//			return fmt.Errorf("%w: want no args", validator.ErrArgs)
//		}
//		if b, ok := t.Underlying().(*types.Basic); !ok || b.Info()&types.IsInteger == 0 {
//			return fmt.Errorf("%s is not an integer type", t)
//		}
//		return nil
//	}
//
//	func (even) Emit(
//		e validator.Emitter, _ types.Type, v string, _ []dircmnt.KeyValue,
//	) (string, error) {
//		e.Declare("kamaValidateEven", func(s validator.Spelling) (string, error) {
//			errs, err := s.Import("errors")
//			if err != nil {
//				return "", err
//			}
//			return "func kamaValidateEven(n int64) error {\n" +
//				"\tif n%2 != 0 {\n\t\treturn " + errs + ".New(\"want even\")\n\t}\n" +
//				"\treturn nil\n}", nil
//		})
//		return "kamaValidateEven(int64(" + v + "))", nil
//	}
//
//	reg := validator.DefaultRegistry().Clone()
//	reg.Register(even{})
//	files, warnings, err := validator.Generate(pkgs, validator.Options{Registry: reg})
//
// Format holds its formats in a map. A caller adds a format by registering
// a Format holding DefaultFormats and the new FormatHelper, as the doc
// comment of Format shows.
//
// # Option file
//
// An option file gives the options of the directives for types that cannot
// carry them, such as generated code. ProcParam.OptionFile names the file
// Proc reads, and Options.Loaded holds the loaded options. The Loader of
// NewLoader loads them. The file is JSON, YAML or TOML:
//
//	version: 1
//	packages:
//	  example.com/gen/pb:           # package import path
//	    all_types: true             # //kama:validate package
//	    types:
//	      Order:                    # //kama:validate on Order
//	        hook: false             # //kama:validate:hook off on Order
//	        fields:
//	          Id:
//	            validators:
//	              - name: required  # //kama:validate:required
//	              - name: regexp    # //kama:validate:regexp "^[A-Z]+$"
//	                args: '"^[A-Z]+$"'
//	          Note:
//	            hook: true          # //kama:validate:hook on
//	      Draft:
//	        ignore: true            # //kama:validate ignore on Draft
//
// Listing a type marks it unless ignore is true. Its hooks are then on by
// default. args take the grammar of the args of a directive, and a
// validator listed twice on one field is an error. The file has no
// equivalent of //kama:validate file. version is required and is 1. The
// entry of a type replaces every directive on the type and its fields, and
// Generate returns a warning wrapping genopt.ErrOverridden for a type whose
// directives are replaced. The free-floating directives stay in effect
// beside the file.
//
// A package option file gives the options of a single package. Its
// document has no packages level: version, all_types and types sit at the
// top. A free-floating comment of the package names it:
//
//	//kama:option-file validate=../options/pb.yaml
//
// A package without that directive gets kama_validate.yaml, .yml, .json or
// .toml in its directory, the first one present in that order. The entry of
// a type in the package option file replaces its entry in the option file
// and its directives, with a warning for each.
//
// # Output
//
// Each source file declaring targets gets a file of the same package next
// to it, named by inserting .kama_validate before the build constraint
// suffixes of its name, as in x.kama_validate_linux.go for x_linux.go,
// with the //go:build line of the source file. Every generated declaration
// carries the marker //kama:generated kama-validate.
//
// The helpers the Validate methods call go to the helpers file
// zz_gen.helper.kama_validate.go of the package. A helper called only by
// files with a build constraint goes to the helpers file of the first of
// them, as in zz_gen.helper.x.kama_validate_linux.go. A package emits only
// the helpers it calls. The built-in helpers are named requiredComparable
// or start with kamaValidate, and a compiled pattern is the variable
// kamaValidateRegexp_ followed by the first 5 bytes of the SHA-256 of the
// pattern in hexadecimal. A package-level declaration named like a helper,
// and a declaration in a helpers file that the generator did not write, are
// errors wrapping ErrConflict.
//
// Every generated file starts with a header naming the generator, the
// option_file and package_option_file it read, and the time of the run.
// Proc keeps a file that differs from the existing one in the generated:
// line only. A run without changes therefore leaves every file as it was.
//
// A file of the loaded packages is stale when it holds a declaration with
// the marker of the generator and the run does not produce it. Generate
// returns a warning wrapping ErrStale for a stale file, and Proc reports
// it. The file is left in place; delete it if it is no longer needed.
//
// # Not supported
//
//   - and(...) and or(...) combinators. Several validators on one field
//     are all required.
//   - Validators on the elements of a slice or map, and on the fields of a
//     struct type literal. Write a hook, or give the element type a
//     Validate.
//   - Validators other than required, zero and null on an elastic.Elastic.
//   - Hooks and implementors with a pointer receiver.
//   - Targets of packages outside the run, and types other than struct,
//     slice and map types.
package validator
