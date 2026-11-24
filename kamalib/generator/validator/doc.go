// Package validator generates Validate methods for data models.
//
// A target T gets one method:
//
//	func (v T) Validate(stopOnFirstError bool) error
//
// Validate runs the validators that //kama:validate directives name on the
// fields of T, validates the values the fields hold through their own
// Validate, and calls a hook method per field. It returns errors of
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
// A type parameter or a package-level declaration may be named like a
// local of the generated Validate, such as v, stopOnFirstError, errs or
// err. The local then takes a fresh name, as in func (v2 Box[v]) Validate.
// A type parameter named like a predeclared identifier the generated code
// spells, or like a helper the generated code of its package calls,
// shadows it. Each is an error wrapping ErrConflict.
//
// # Directive
//
// One directive, //kama:validate, carries every option. Its args follow the
// grammar of dircmnt.ParseArgsKeyValue: space-separated words written key
// or key=value. A value is a bare word or a quoted or raw Go string literal:
//
//	//kama:validate hook=off
//	type User struct {
//		//kama:validate required
//		Name string
//		//kama:validate required length=">=1 <=10" unique
//		Tags []string
//		//kama:validate regexp="^[a-z-]+$" runes="<=64" hook=on
//		Slug string
//		//kama:validate format=date
//		Born string
//		//kama:validate cmp=">=0 <100" const="1 2 3"
//		N int
//		//kama:validate multipleof=0.5
//		Score float64
//		//kama:validate const=`"red" "dark blue"`
//		Color string
//	}
//
// The place of the directive gives its args their meaning:
//
//   - In the doc comment of a type, the directive marks the type. The arg
//     ignore exempts the type instead. The arg hook=on or hook=off sets the
//     default hook of the fields of the type. Targets and Hooks describe
//     them.
//   - On a struct field, each arg names a validator, and the arg hook=on or
//     hook=off switches the hook of the field. Validators and Hooks
//     describe them.
//   - In a free-floating comment, the arg file or package marks the types
//     of the file or of the package. Targets describes them.
//
// Several //kama:validate lines on one type or one field count as one
// directive. Their args are joined in source order. hook given twice on one
// type or field is an error wrapping ErrDirective. The words hook, ignore,
// file and package name no validator.
//
// A sub-directive //kama:validate:<name> is an error wrapping ErrDirective.
// The error shows the args of //kama:validate standing for it. For
// //kama:validate:length >=1 it shows //kama:validate length=">=1".
//
// # Targets
//
// The generator takes targets from the loaded packages only, by directives
// or by the option file entries standing for them:
//
//   - //kama:validate in the doc comment of a type marks it, with or
//     without hook=on or hook=off. In the doc
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
// type in a Validate. In a generic target, a type parameter of the target
// counts as something to validate. Take type PT[T any] *T as an example.
// A field P PT[T] of a target G[T any] is an error. A field P PT[int] holds
// nothing to validate. It is no error. Use an unnamed array or pointer
// type, or a wrapper struct, in its place.
//
// The type arguments of a generic struct, slice or map type make no
// dependant, so such a type of the run is a target only when it is marked
// or holds something to validate by itself. An instance of an unmarked
// one whose type arguments hold something to validate, as LS[Inner] of
// type LS[T any] []T, is an error wrapping ErrUnsupported in the same
// places as a named array or pointer type. Mark the generic type with
// //kama:validate to pass validation on. An instance holding nothing to
// validate, as LS[int], is no error. A generic type ignored by
// //kama:validate ignore is no error either. Its values go unchecked.
//
// A named array or pointer type of a package outside the run is no error.
// Validate walks through it as through an unnamed array or pointer type.
// A type holding an implementor through it is a dependant. Such a type
// holding itself and something to validate, as in
// type L *struct{ Next L; I Impl }, is an error wrapping ErrUnsupported.
// Validate would need a recursive function to walk it.
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
//  1. The validators the directive on the field names, in the order
//     written. Several directive lines on one field count as one line:
//     their args are joined in source order.
//  2. The nested validation of the values the field holds. Validate walks
//     through pointers, slices, arrays, map values, the fields of struct
//     type literals and the option types to each value of a target, of an
//     implementor or of a type parameter. A nil pointer, slice or map, a
//     none opt.Option, an undefined or null und.Und and a null element of
//     an elastic.Elastic hold nothing and are skipped. The walk never goes
//     into the underlying type of a named type, except for a named array or
//     pointer type of a package outside the run. A struct, slice or map
//     type of the run holding something to validate by itself is a target
//     itself. A named array or pointer type of the run holding something to
//     validate, a type parameter of the target included, is an error. So
//     is an instance of an unmarked generic struct, slice or map type of
//     the run whose type arguments hold something to validate. Targets
//     describes both.
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
// names the type, the field, the expected signature and the arg turning
// the hook off. These args of //kama:validate override the default:
//
//   - hook=off in the doc comment of a type turns the hooks of all its
//     fields off, and hook=on turns them on.
//   - hook=off on a field turns its hook off, and hook=on turns it on.
//
// A field arg wins over a type arg, and a type arg wins over the default.
// A field opts back in under a type-level hook=off.
//
// Every //kama:validate in the doc comment of a type marks the type unless
// it holds ignore. A dependant therefore opts in to its hooks with
// //kama:validate hook=on, and that directive marks it. A dependant is a
// target already, so the mark changes nothing else for it. The type stays a
// target when it no longer holds anything to validate. hook on a type or on a field of
// a type that gets no Validate, as in //kama:validate ignore hook=on, is an
// error wrapping ErrDirective.
//
// # Validators
//
// An arg of //kama:validate on a struct field of a target names a
// validator of the registry: name alone, or name=<arg>. The validators run
// in the order written. arg is one string. The directive grammar unquotes
// it, and the validator receives it unchanged. Each validator parses its
// own arg. length, runes, cmp and const split it into tokens with
// dircmnt.ParseArgsKeyValue. A bare token starting with <, >, = or ! is one
// comparison, as in >=1 or =="a b". regexp takes the whole arg as its
// pattern. An empty arg, as in name="", counts as no arg.
//
// Several validators on one field must all pass. A name the registry does
// not hold is an error wrapping ErrDirective. The error names the field
// and lists the known validators. These are errors wrapping ErrDirective
// too:
//
//   - the same validator named twice on one field.
//   - a validator on a field of a type that gets no Validate.
//   - //kama:validate without args on a field.
//   - a validator name in the doc comment of a type or in a free-floating
//     comment.
//
// A validator applied to a type it cannot handle, or with an arg it does
// not accept, is an error wrapping ErrUnsupported. The error names the
// field, the validator and the type.
//
// The generator applies a validator to the type of the field first. When
// the validator rejects that type and the field is a pointer, an
// opt.Option or an und.Und, it applies the validator to the value held,
// under a guard that skips the validator when no value is held. Only an
// error of Rule.Check leads to that second attempt. The generator reports
// every error of Validator.Parse at once. Such an error comes from the arg
// alone and should wrap rule.ErrArgs. required, zero and nil therefore look
// at a pointer itself, and every other validator applies to the pointee of
// a non-nil pointer. elastic.Elastic holds no single value and takes
// required, zero and null only.
//
// Package rule documents the built-in validators and the errors they report.
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
// Validators are entries of a rule.Registry, and Options.Registry selects
// it. A validator named hook, ignore, file or package makes Generate fail
// with an error wrapping ErrConflict. Package rule documents the registry
// and how to write a validator.
//
// Proc generates with Proc.Registry. The kind validate.generate of
// workflow.DefaultRegistry uses rule.DefaultRegistry(). NewProcWith returns
// the constructor of a Proc with another registry. A program runs a workflow
// spec with that registry through a workflow.Registry of its own. The
// program registers the constructor under validate.generate in it. It
// registers the other procs the spec names in it too:
//
//	procs := workflow.NewRegistry()
//	for kind, newProc := range map[string]func() workflow.Proc{
//		"overlayfs.open":      overlayfs.NewOpenProc,
//		"overlayfs.copy_down": overlayfs.NewCopyDownProc,
//		"packages.load":       gopackages.NewProc,
//		"validate.generate":   validator.NewProcWith(reg),
//	} {
//		if err := procs.Register(kind, newProc); err != nil {
//			return err
//		}
//	}
//	g, err := spec.Build(ctx, procs)
//
// The packages overlayfs and gopackages are those under
// github.com/ngicks/kama/kamalib/proc. A program may instead register the
// constructor in workflow.DefaultRegistry under a kind of its own. The
// spec then names that kind:
//
//	workflow.Register("validate.generate_multiple", validator.NewProcWith(reg))
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
//	        hook: false             # hook=off on Order
//	        fields:
//	          Id:
//	            validators:
//	              - name: required  # required on Id
//	              - name: regexp    # regexp="^[A-Z]+$" on Id
//	                args: '^[A-Z]+$'
//	          Note:
//	            hook: true          # hook=on on Note
//	      Draft:
//	        ignore: true            # //kama:validate ignore on Draft
//
// Listing a type marks it unless ignore is true. Its hooks are then on by
// default. args is the arg a directive gives after name=, unquoted. An
// empty or absent args stands for no arg. A validator listed twice on one
// field is an error. The file has no equivalent of //kama:validate file.
// version is required and is 1. An unknown key is a warning wrapping
// genopt.ErrUnknownKey that names the file and the key path. An entry
// holding unknown keys only does not list its type or field, so a
// misspelt ignore leaves the directives in effect. The entry of a
// type replaces every directive on the type and its fields, and Generate
// returns a warning wrapping genopt.ErrOverridden for a type whose
// directives are replaced. The free-floating directives stay in effect
// beside the file.
//
// A package option file gives the options of a single package. Its
// document has no packages level: version, all_types and types sit at the
// top. A free-floating comment of the package names it:
//
//	//kama:option-file validate=../options/pb.yaml
//
// A package without that directive gets kama_validate.toml, .yaml, .yml or
// .json in its directory, the first one present in that order. The entry of
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
// # Test files
//
// With the packages loaded with their tests, targets of _test.go files get
// Validate too. The test variant of a package x stands for x, so a target
// of a_test.go of x gets a.kama_validate_test.go and the files of x come
// out as without tests. A helper only _test.go files of x call goes to the
// helpers file of the first of them, as in
// zz_gen.helper.a.kama_validate_test.go. The external test package x_test
// is a package of its own at the path of x followed by _test, and its
// helpers file is zz_gen.helper.kama_validate_test.go. The package option
// file found in the directory of x belongs to x. An option file entry
// naming a type of the tests needs the tests loaded.
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
