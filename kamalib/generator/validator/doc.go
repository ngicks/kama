// Package validator generates Validate methods for data models.
//
// A target T of a struct type gets the method
//
//	func (v *T) Validate(stopOnFirstError bool) error
//
// A struct target is expected to be big, and a value receiver would copy
// it at every call and at every level of the nested validation. A target
// of another kind, as in type Name string, type Roster []User or type M
// map[K]V, keeps the value receiver, since copying it is cheap. Every
// method this generator writes for a type takes the receiver Validate
// takes. A method on the pointer receiver called on a nil pointer returns
// an error wrapping validator.ErrNilReceiver and never panics. A nil
// pointer field is the business of the struct holding it, through nil and
// required on the field, and the nested validation skips it.
//
// Validate runs the validators that //kama:validate directives name on T
// and on the fields of T, validates the values the fields hold through
// their own Validate, and calls a hook method per field. It returns errors of
// package github.com/ngicks/kama/kamaruntime/validator, located by the
// json names of the fields. Generator is the generator of the framework
// of package base, and the proc of NewProc runs it as the workflow step
// kind validate.generate. Shallow validation and Raw JSON
// text describe the methods ValidateShallow and ValidateEvaluated that
// some targets get besides.
//
// A generic target gets one more parameter per type parameter, in order:
//
//	func (v *Box[T, U]) Validate(
//		stopOnFirstError bool,
//		validateT func(v T, stopOnFirstError bool) error,
//		validateU func(v U, stopOnFirstError bool) error,
//	) error
//
// validateT validates the values of type T that a Box holds. A nil
// validateT leaves them unchecked. It takes the value, so a value crossing
// it is copied once. The generator passes a function literal taking the
// value for a type argument whose Validate is on the pointer receiver. A
// caller avoids the copy by instantiating the type with *T. The parameter is named validate
// followed
// by the name of the type parameter. A blank type parameter gets a fresh
// name. Because of these parameters no single interface describes every
// Validate, and the generator finds callable methods by their shape.
//
// A type parameter or a package-level declaration may be named like a
// local of the generated Validate, such as v, stopOnFirstError, errs or
// err. The local then takes a fresh name, as in func (v2 *Box[v]) Validate.
// A type parameter named like a predeclared identifier the generated code
// spells, or like a helper the generated code of its package calls,
// shadows it. Each is an error wrapping ErrConflict.
//
// # Directive
//
// One directive, //kama:validate, carries every option. The text after it
// is a comma separated list of items, each a Starlark expression as package
// pkg/dircmnt/expr reads it:
//
//	//kama:validate hook=False
//	type User struct {
//		//kama:validate required
//		Name string
//		//kama:validate length(ge=1, le=10), unique, e=runes(le=64)
//		Tags []string
//		//kama:validate regexp(r"^[a-z-]+$"), runes(le=64), hook=True
//		Slug string
//		//kama:validate format("date")
//		Born string
//		//kama:validate cmp(ge=0, lt=100), const(1, 2, 3)
//		N int
//		//kama:validate cmp(ge=10) or const(0), not const(5)
//		Stock int
//		//kama:validate k=regexp(r"^[a-z]+$"), v=cmp(ge=0)
//		Counts map[string]int
//	}
//
// The grammar of an item is
//
//	item   = rule | name "=" value .
//	rule   = call | rule "and" rule | rule "or" rule | "not" rule
//	       | rule "if" rule "else" rule | "(" rule ")" .
//	call   = name [ "(" [ arg { "," arg } ] ")" ] .
//	arg    = [ name "=" ] ( rule | literal ) .
//	value  = rule | literal | "g(" item { "," item } ")" .
//
// A literal is a string, a number, True, False, None, a list or a dict. A
// bare item is a rule validating the value the directive is written on. A
// keyword item is one of these:
//
//   - hook=True or hook=False switches the hook of a field, or the default
//     hook of the fields of a type.
//   - pre="f" and post="g" name the functions Validate of a type calls
//     before and after its checks, as Pre and post functions describes.
//   - A role of the type of the value, as e=length(le=64) on a []string,
//     validates the values of that role, as Roles describes.
//   - nested=False, unwrap="M", iter="M" and iter2="M" direct the walk into
//     the values a field holds, as Nested validation describes.
//
// Any other keyword item is an error wrapping ErrDirective. A keyword item
// naming a validator, as length=">=1", is reported with the call standing
// for it, length(ge=1). The bare words ignore, decoded, file and package
// are options of a type or a free-floating comment:
//
//   - In the doc comment of a type, the directive marks the type. ignore
//     exempts the type instead. decoded tells that decoding a value of the
//     type validates it, as Shallow validation describes. Every rule
//     validates the value of the type. Targets, Hooks and Type validators
//     describe them.
//   - On a struct field, every rule validates the field. Validators and
//     Hooks describe them.
//   - In a free-floating comment, file or package marks the types of the
//     file or of the package. Targets describes them.
//
// Each line of a directive is parsed on its own, and the items of the lines
// of one type or one field are joined in source order. An item does not
// span lines. hook, pre or post given twice on one type or field, and a
// rule or a role given twice, are errors wrapping ErrDirective. The words
// hook, pre, post, ignore, decoded, file, package, g, nested, unwrap, iter
// and iter2 name no validator and no role.
//
// A sub-directive //kama:validate:<name> is an error wrapping ErrDirective.
// The error shows the item of //kama:validate standing for it. For
// //kama:validate:length ge=1 it shows //kama:validate length(ge=1).
//
// # Roles
//
// A role names the values a value holds. A keyword item whose name is a
// role of the type of the value applies its value to each value of that
// role, under the guard telling the value is held or in a loop over the
// values, and locates its errors at the value:
//
//	type                                   roles
//	slice, array                           e
//	map                                    k, v
//	pointer                                e
//	opt.Option, und.Und, elastic.Elastic   t
//	either.Either, UntaggedEither          l, r
//	either.Result, UntaggedResult          ok, err
//	tuple.TupleN                           v1 ... vN
//
// The value of a role item is one rule, as e=length(le=64), or a group
// g(...). A group takes what a directive line takes: rules for the value
// and role items for the values it holds, as e=g(length(ge=1), v=cmp(ge=0))
// on a []map[string]int. A rule on elastic.Elastic validates the elastic
// value, so length(3) or null, t=length(le=64) asks for three elements or
// null, each of at most 64 bytes. A role the type does not offer is an
// error naming the roles it offers. The type supports of the registry give
// the roles of the types they claim, as ext.Roles tells. A role a support
// names like a word of the directive or a validator is an error wrapping
// ErrConflict.
//
// A pointer, an opt.Option and an und.Und hold one value. A rule on such a
// field applies to the outermost level taking it: nil, required, zero and
// null test the holder, and a rule of the held type goes through every
// level, as cmp(ge=1) on a *opt.Option[int], under the guards. A role item
// addresses an inner level, as e=g(nil or not cmp(0)) on a **int tests the
// inner pointer. A combinator whose operands take different levels applies
// each operand to its own level, and an operand holds when its level holds
// no value. A slice, a map, an elastic.Elastic and the composites hold no
// value a bare rule reaches. Their values take a role item.
//
// # Nested validation
//
// Validate calls Validate of each value a field holds whose type has one,
// as What Validate does describes, whether or not role items validate the
// value too. Each value gets one call. These keyword items, on a field or
// inside g(...), direct that walk:
//
//   - nested=False stops the calls for the value and for everything it
//     holds. nested=True in an inner group turns them back on. Role items
//     still apply.
//   - unwrap="M" reaches the one value M returns, M of the signature
//     func() T, or func() (T, bool) whose bool tells the value is held.
//     Role v names the value.
//   - iter="M" ranges over the result of M, any function func(yield
//     func(T) bool), as iter.Seq or opiter.Seq. Role v names each value,
//     located by its position.
//   - iter2="M" ranges over the result of M, any function func(yield
//     func(K, V) bool). Roles k and v name each key and value. A key of a
//     string or an integer kind locates its value, and the position does
//     otherwise.
//
// A description names a method of the type of the field, found with
// go/types, and replaces the roles of its type supports. A missing method,
// another signature, and two descriptions of one value are errors. A field
// with a description holds no type the target selection reports, so a
// generic container of the run needs no directive of its own:
//
//	//kama:validate iter="All", v=length(le=64)
//	Names List[string]
//	//kama:validate unwrap="Get", v=cmp(ge=1)
//	Size Maybe[int]
//
// # Targets
//
// The generator takes targets from the loaded packages only, by directives
// or by the option file entries standing for them:
//
//   - //kama:validate in the doc comment of a type marks it, with or
//     without hook=True or hook=False. In the doc
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
// slice and array elements, map values, pointers, aliases and the values
// the types a type support of the registry claims hold, as ext.Dependants
// tells. Package ext/opt registers the support of opt.Option, und.Und,
// elastic.Elastic and the types of packages either and tuple, and package
// ext/std the supports of sql.Null[T] and unique.Handle[T], so their
// type arguments make dependants. Map keys,
// channel elements and the type arguments of other generic types make no
// dependant. The user did not
// mark a dependant. Its hooks are therefore off by default. Its fields may
// still carry validators.
//
// A marked type does not make the types of its fields targets. A field of
// a type that is neither a target nor an implementor gets its own
// validators and hook only.
//
// Only defined types whose underlying type is a struct, a slice or a map
// get a Validate, and types of another underlying type with validators of
// their own, as Type validators describes. File marks, package marks and
// the search for dependants skip other types silently. An explicit
// //kama:validate without validators on one, or on an alias, is an error
// wrapping ErrUnsupported. Types of packages outside the run are never
// targets.
//
// A named array type without validators of its own or a named pointer type
// of the run holding something to validate, as in type Pair [2]Inner or
// type PI *Inner, gets no Validate either. A
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
// A type declaration whose doc comment carries the genmark marker of a
// registered generator is no target and no dependant, unless a
// //kama:validate in its doc comment or an option file entry marks it
// explicitly. A generator of data models, such as the one of package
// github.com/ngicks/kama/kamalib/generator/jsonschema, hands its types on
// that way. The registry of base.Env tells the registered generators.
//
// # Implementors
//
// An implementor is a named type with a hand-written Validate that
// generated code can call. Its method is declared on the value receiver or
// on the pointer receiver of the type itself, not promoted from an embedded
// field, and has the signature
//
//	func(bool, func(T1, bool) error, ..., func(Tn, bool) error) error
//
// with one function parameter per type parameter T1, ..., Tn of the type,
// in order, or takes no argument, or only a variadic one, and returns one
// error, as Validate() error and Validate(opts ...Option) error do. The
// first signature wins. Any other signature is ignored. A method of a loaded package carrying the
// marker of this generator does not count. A stale generated file
// therefore does not keep a type validated. A method another generator
// wrote counts as one written by hand. A generated Validate of a
// package outside the run counts like a hand-written one. An interface
// type is never an implementor. Its nil value would panic.
//
// An implementor of a loaded package gets no Validate, and marking it is an
// error wrapping ErrConflict. To keep both the generated checks and checks
// of its own, a type drops its Validate and moves those checks into pre and
// post functions or field hooks. The types holding it become dependants. An
// implementor of a package outside the run makes the types of the run
// holding it targets in the same way. A target with a Validate the
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
// An implementor whose Validate takes no stopOnFirstError is called as
// x.Validate(). A Validate on the pointer receiver, such as the one of a
// struct target, is called on the value in place wherever the value has an
// address: a field as v.F.Validate(...), an element of a slice or an array
// by its index as v.S[i].Validate(...), and the value a pointer points to
// as (*p).Validate(...), which passes p itself. The walk reaches the
// elements of a slice or an array by index for this reason, and copies
// none into a range variable. A slice or an array that a method returns,
// as elastic.Elastic.Options does, is bound to a local once and walked by
// index too. A value without an address is copied into a
// local first: a map value, a value an iterator yields, and the result of
// a method such as opt.Option.Value. A function literal passed for a type
// parameter calls the method on its parameter.
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
//     type literals and the values the types a type support claims hold to
//     each value of a target, of an implementor or of a type parameter. A
//     nil pointer, slice or map, a none opt.Option, an undefined or null
//     und.Und, a null element of an elastic.Elastic, the side an
//     either.Either does not hold and the error of an either.Result hold
//     nothing and are skipped. Each field of a tuple.Tuple2 to
//     tuple.Tuple16 is walked. The walk never goes
//     into the underlying type of a named type, except for a named array or
//     pointer type of a package outside the run. A struct, slice or map
//     type of the run holding something to validate by itself is a target
//     itself. A named pointer type, or a named array type without
//     validators of its own, of the run holding something to validate, a
//     type parameter of the target included, is an error. So
//     is an instance of an unmarked generic struct, slice or map type of
//     the run whose type arguments hold something to validate. Targets
//     describes both.
//  3. The hook of the field, described under Hooks.
//
// Validate wraps every error of these steps with the path segment of the
// field. Nested validation adds an index segment per slice, array and
// elastic.Elastic element, a key segment per map value, the field segment
// left or right of an either.Either, ok of an either.Result and V1 to VN
// of a tuple, as they encode. The values of either.UntaggedEither and
// either.UntaggedResult add none. With
// stopOnFirstError true, Validate returns the first error. Otherwise it runs
// every step and returns the errors joined by validator.Join, or nil when
// there are none. It passes stopOnFirstError unchanged to nested Validate
// calls, to hooks and to the validateT functions.
//
// Validate visits the keys of a map in sorted order when the key type is an
// integer or string kind or a named type of one. The first error and the
// order of the errors are then stable. It ranges over other maps directly,
// float keys included. A lookup by a NaN key never finds its value. Sorting
// the keys would therefore lose that value.
//
// A slice, array or map target gets a Validate walking its elements as
// step 2 does, with the index or key as the path segment. Hooks do not
// apply to it. A target with nothing to validate gets a Validate returning
// nil.
//
// The validators of the type itself, described under Type validators, run
// first in every Validate, before the steps of the fields and the walk of
// the elements. Only the function pre, described under Pre and post
// functions, runs before them.
//
// # Shallow validation
//
// A decoder validating the value it decodes, as the decoders the jsonschema
// generator writes do, finds every nested value validated by the decoder
// of its own type. Validate of the value would validate them again, and a
// value nested n levels deep would cost time growing as n squared at best.
// //kama:validate decoded on a type tells that its decoder validates it,
// and such a decoder calls the second method a target may get:
//
//	func (v *T) ValidateShallow(stopOnFirstError bool) error
//
// ValidateShallow runs what Validate runs, with two differences in the
// nested validation of step 2. It skips a value of a type marked decoded.
// It calls ValidateShallow in place of Validate on a value of a target or
// an implementor having one. An implementor has one when it declares
// ValidateShallow on the receiver of its Validate with the signature of
// its Validate. A target gets one when its walk reaches a value of a type
// marked decoded or of a type having a ValidateShallow, directly or through
// the walk of another target. A target marked decoded gets one too, since
// its decoder calls it on the type itself; where its walk reaches no such
// value, its ValidateShallow calls its Validate. Any other target would
// get a ValidateShallow equal to its Validate, so it gets none, and a
// caller asking such a type for its shallow form calls Validate instead. A
// generic target takes the parameters of its Validate, and the function
// given for a type parameter validates in full.
//
// ValidateShallow is meant for a value a decoder produced. A value built in
// Go may hold values of a type marked decoded that no decoder validated, so
// its Validate is the method to call. A target declaring ValidateShallow
// itself, or a struct field named ValidateShallow, collides with the
// generated method and is an error wrapping ErrConflict.
//
// # Path segments
//
// The path segment of a field is the name in its json tag when the tag has
// one and is not "-". Otherwise it is the Go name of the field. An embedded
// struct, or pointer to a struct, without a json name adds no segment.
// Encoders inline its fields. With a json name it adds that name. An
// embedded field of another type adds its Go name. A field tagged
// json:",embed" adds no segment either. Encoders inline its entries, which
// add their keys.
//
// # Hooks
//
// The hook of a field F is a method the user writes on the target T:
//
//	func (v T) validateF(stopOnFirstError bool) error
//	func (v *T) validateF(stopOnFirstError bool) error
//
// Its name is validate followed by the field name with its first letter
// upper-cased. A field Name and a field name both map to the hook
// validateName. Two fields of one struct whose hooks are on and share a
// name are an error wrapping ErrConflict. Turn the hook of one of them off.
// Validate of a struct target has the pointer receiver, so it calls a hook
// on either receiver. Validate wraps the error of the hook with the path segment
// of the field. A plain error of a hook is therefore located at the field.
//
// The hook of each field of a marked type is on by default. File and
// package marks and option file entries count as marks. The hooks of a
// dependant are off by default. A field whose hook is on and missing, or
// has another signature, is a generation error wrapping ErrHook. The error
// names the type, the field, the expected signature and the arg turning
// the hook off. These args of //kama:validate override the default:
//
//   - hook=False in the doc comment of a type turns the hooks of all its
//     fields off, and hook=True turns them on.
//   - hook=False on a field turns its hook off, and hook=True turns it on.
//
// A field arg wins over a type arg, and a type arg wins over the default.
// A field opts back in under a type-level hook=False.
//
// Every //kama:validate in the doc comment of a type marks the type unless
// it holds ignore. A dependant therefore opts in to its hooks with
// //kama:validate hook=True, and that directive marks it. A dependant is a
// target already, so the mark changes nothing else for it. The type stays a
// target when it no longer holds anything to validate. hook on a type or on a field of
// a type that gets no Validate, as in //kama:validate ignore, hook=True, is an
// error wrapping ErrDirective.
//
// # Pre and post functions
//
// The items pre="<func>" and post="<func>" in the doc comment of a target name
// package-level functions of its package that take the value of the type:
//
//	//kama:validate pre="validatePointPre", post="validatePointPost"
//	type Point struct {
//		X, Y int
//	}
//
//	func validatePointPre(v Point, stopOnFirstError bool) error
//	func validatePointPost(v *Point, stopOnFirstError bool) error
//
// A function of a struct target takes the value or a pointer to it.
// Validate passes its receiver to a function taking a pointer, and the
// value the receiver points to otherwise. A function of a target of
// another kind takes the value.
//
// A generic target G[T any] takes generic functions with type parameters
// of the same number, as in func validateGPre[T any](v G[T], stopOnFirstError
// bool) error. Validate calls pre first, before the validators of the type.
// It calls post in a deferred call, so post runs after every return,
// whether pre or a validator failed or not. With stopOnFirstError true, an
// error of pre skips the rest of the checks, and post still runs. With
// stopOnFirstError false, Validate joins the error of pre, the errors of
// the checks and the error of post. The errors of pre and post carry no
// path segment of their own, so a plain error is located at the value.
// validator.Root wraps it in a *validator.Error, so that a decoder records
// it as a problem of the value.
//
// ValidateShallow and ValidateEvaluated call the functions as Validate
// does. A ValidateShallow passing its call on to Validate leaves them to
// Validate, so every call of one of the methods runs each function once.
// The nested Validate of a value of the type runs them for that value.
//
// A type whose only args are pre and post gets a Validate, a type of
// another kind than struct, slice and map included. A function missing,
// not declared at package level, or of another signature is a generation
// error wrapping ErrHook. The error names the expected signature. pre or
// post on a field, on a type group or on a type that gets no Validate is
// an error wrapping ErrDirective.
//
// # Validators
//
// A rule of //kama:validate on a struct field of a target calls a validator
// of the registry: name alone, as required, or name(args), as
// length(ge=1, le=10). The rules run in the order written. ext.Params binds
// the args of a call to the parameters of the validator. A comparison is a
// keyword arg gt, ge, lt, le, eq or ne, and one positional arg compares
// with ==, as length(3). A name of a type, a format, a kind or a member is
// a string, as ref("Tree") and kind("integer", "null"). A number keeps its
// digits. A keyword arg no parameter takes is reported as a warning, and
// the rule applies without it.
//
// Several rules on one field must all pass. A name the registry does not
// hold is an error wrapping ErrDirective. The error names the field and
// lists the known validators. These are errors wrapping ErrDirective too:
//
//   - the same validator named twice on one field or one type. Join two
//     uses of one validator with and, as in not a and not b.
//   - a rule on a field of a type that gets no Validate, or in the doc
//     comment of such a type, as in //kama:validate ignore, cmp(gt=0).
//   - //kama:validate without items on a field.
//   - a rule in a free-floating comment.
//   - a rule other than an ext.StructRule in the doc comment of a struct
//     type, an ext.StructRule on a field or in the doc comment of a type of
//     another kind whose type its Check rejects, and an ext.SiblingRule in
//     the doc comment of any type or in a role item. members accepts raw
//     JSON text, as Raw JSON text describes.
//
// A validator applied to a type it cannot handle, or with an arg it does
// not accept, is an error wrapping ErrUnsupported. The error names the
// field, the validator and the type.
//
// The generator applies a rule to the type of the field first. When the
// rule rejects that type and the field holds one value, a pointer, an
// opt.Option or an und.Und, it applies the rule to the value held, through
// every level, under a guard that skips the rule when no value is held, as
// Roles describes. Only an error of Rule.Check leads to that attempt. The
// generator reports every error of Validator.Parse at once. Such an error
// comes from the args alone and should wrap ext.ErrArgs. An
// ext.SiblingRule, such as requires, reads the other fields of the struct
// and applies to the field itself only. Its errors are located where the
// rule puts them, as at a sibling field, without the path segment of the
// field it is written on.
//
// Package basic documents the built-in validators and the errors they report.
//
// # Combinators
//
// The operators and, or, not and the conditional combine rules, and oneof
// is a validator whose args are rules:
//
//		//kama:validate cmp(ge=10) or const(0), not regexp("^x")
//		//kama:validate requires("amount") if const("fixed") else const("percent")
//		//kama:validate cmp(ge=1) and not (const(5) or const(7)) or const(-1)
//
//	  - a and b: every operand holds. always, a rule without operands, always
//	    holds.
//	  - a or b: at least one operand holds. never, a rule without operands,
//	    never holds.
//	  - not a: the operand fails.
//	  - oneof(a, ...): exactly one operand holds.
//	  - then if cond else otherwise: when cond holds, then holds, otherwise
//	    otherwise holds. Write always for a branch that always holds. The
//	    error of cond never surfaces.
//
// not binds tightest, then and, then or, and the conditional loosest.
// Package basic registers the operators by the kinds of their nodes through
// ext.RegisterOperator, so a registry may give them another meaning. The
// operands resolve through the registry of the run, so a user validator
// and a validator replacing a built-in are operands too. An operand name
// the registry does not hold is an error wrapping ErrDirective. A
// combinator applies to a type when every operand applies to it, else to
// the level a pointer, an opt.Option or an und.Und holds that every operand
// applies to, else each operand to its own level: nil or cmp(ge=1) on a
// *int tests the pointer with nil and the int with cmp.
//
// A combinator holding an ext.SiblingRule operand, such as requires, is an
// ext.SiblingRule itself. It applies to the field itself. Each of its
// other operands applies to the type of the field, else to the value a
// pointer, an opt.Option or an und.Und holds, and holds when the field
// holds none: requires("b") if cmp(ge=1) else always on a field A *int
// compares *A when A is not nil. Its errors are located at the field,
// except those the sibling operand locates at a sibling. A combinator
// holding an ext.StructRule operand, such as members, is an ext.StructRule
// and goes in the doc comment of a struct type. Operands of both kinds in
// one combinator are an error.
//
// and and the conditional return the errors of their operands. or, not and
// oneof return errors of their own, which package basic describes.
//
// # Type validators
//
// The validators //kama:validate names in the doc comment of a type
// validate the value of the type itself. The receiver of Validate is the
// value, and the errors carry no path segment of their own. A field holding
// the type adds its segment through the nested call:
//
//	//kama:validate cmp(ge=1, le=5)
//	type Level int
//
//	//kama:validate length(ge=1), unique
//	type Tags []string
//
// A type whose underlying type is no struct, slice or map, such as Level,
// gets a Validate when its directive names validators. A pointer or
// interface type cannot declare methods. Validators on one are an error
// wrapping ErrUnsupported. A slice, array or map type runs its validators
// before the walk of its elements. A target of these kinds has no fields
// and therefore no hooks. hook=True in its doc comment calls nothing.
//
// Validate of every type holding such a type calls its Validate, as for a
// struct target. A field Level Level runs the validators of the field
// first, then v.Level.Validate.
//
// A struct type takes only validators of the struct as a whole, which
// implement ext.StructRule, such as members and json. The rule reads the fields through
// ext.Struct,
// which names them by the JSON members they encode as. A field takes a
// ext.StructRule only when the Check of the rule accepts the type of the
// field or the value it holds, as members accepts raw JSON text.
//
// # Raw JSON text
//
// A value of jsontext.Value, or of a type of the underlying type []byte
// declaring the method RawJson() jsontext.Value, is raw JSON text, as
// ext.IsRawJson tells. Its validators test it with the semantics of JSON
// Schema: each tests the values of the JSON kinds it applies to and passes
// any other, as minimum passes a string:
//
//	//kama:validate kind("number", "string"), cmp(ge=1), runes(le=3)
//	type Loose jsontext.Value
//
//	func (v Loose) RawJson() jsontext.Value { return jsontext.Value(v) }
//
// The built-in validators on raw JSON text are kind, cmp, multipleof, runes,
// regexp, length on arrays, members on objects, unique, const on JSON values,
// and required with member names. A combinator holds when its operands do as
// on any other type: not cmp(ge=5) fails on 7 and holds on "a". Validate
// does not walk the bytes of raw JSON text as the elements of a slice.
// props, items, prefix, contains, keys, unevalprops and unevalitems reach
// into the members and the elements instead, and ref and decode apply
// another type, as package basic describes:
//
//	//kama:validate kind("object")
//	//kama:validate props(field("next", ref("Tree")), rest(kind("integer")))
//	type Tree jsontext.Value
//
// A target of raw JSON text that is not generic gets one more method:
//
//	func (v T) ValidateEvaluated(stopOnFirstError bool, ev *rawjson.Evaluated) error
//
// It runs the validators of T as Validate does, those implementing
// ext.Annotator recording into ev the members and the elements they
// evaluated. ref calls it in the scope of unevalprops and unevalitems, so
// that a type a reference names reports what it evaluated without its
// validators being written out at the reference.
//
// json applies the validators of raw JSON text to a value of any other type
// encoded as JSON, a struct included:
//
//	//kama:validate json(not required("x"), props(match("^x-", kind("string"))))
//	type Labeled struct {
//		Name string `json:"name"`
//		X    map[string]jsontext.Value `json:",embed"`
//	}
//
// # Errors of Validate
//
// The errors carry a path from the outermost value to the failing one. The
// functions of package validator query them by RFC 6901 JSON pointer:
//
//	err := user.Validate(false)
//	validator.Pointers(err)               // ["/address/city", "/tags/1/name"]
//	validator.At(err, "/tags/1/name")     // the errors at that field
//	validator.Under(err, "/tags")         // the errors at or below it
//	errors.Is(err, validator.ErrRequired) // true
//
// An error prints as validation failed at .tags[1].name: required: want a
// non-zero value, got "".
//
// # Registry
//
// Validators are entries of an ext.Registry, and Options.Registry selects
// it. A validator named hook, pre, post, ignore, decoded, file or package
// makes Generate fail with an error wrapping ErrConflict. Package ext
// documents the registry, how to write a validator and how to add an
// extension package. Package basic holds the built-in validators. Package
// opt holds the type support of github.com/ngicks/option, which tells the
// generator and the validators about the values opt.Option, und.Und,
// elastic.Elastic and the types of packages either and tuple hold. Package
// std holds the type supports of types of the standard library: the null
// types of database/sql and unique.Handle hold a value, cmp compares
// time.Time, time.Duration and the pointers of math/big with operands such
// as "1s", and zero and required test netip.Addr and netip.Prefix by
// IsValid. Its doc lists each type and rule. This package imports the
// three with blank imports, which register them into the default registry
// of ext. The registry of
// Options.Registry also holds the type supports the walk, the selection of
// targets and the validators ask.
//
// The kind validate.generate of workflow.DefaultRegistry generates with
// ext.DefaultRegistry(). NewProcWith returns the constructor of a proc with
// another registry. A program runs a workflow spec with that registry
// through a workflow.Registry of its own. The program registers the
// constructor under validate.generate in it. It registers the other procs
// the spec names in it too:
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
// the proc reads, and Input.Loaded holds the loaded options. The Loader of
// NewLoader loads them. The file is JSON, YAML or TOML:
//
//	version: 1
//	packages:
//	  example.com/gen/pb:           # package import path
//	    all_types: true             # //kama:validate package
//	    types:
//	      Order:                    # //kama:validate on Order
//	        hook: false             # hook=False on Order
//	        fields:
//	          Id:
//	            validators:
//	              - required        # required on Id
//	              - regexp("^[A-Z]+$")
//	          Tags:
//	            validators:
//	              - e=length(le=8)  # a role item on Tags
//	          Note:
//	            hook: true          # hook=True on Note
//	      Draft:
//	        ignore: true            # //kama:validate ignore on Draft
//	      Event:
//	        decoded: true           # //kama:validate decoded on Event
//	      Point:
//	        pre: validatePointPre   # pre="validatePointPre" on Point
//	        post: validatePointPost # post="validatePointPost" on Point
//	      Level:
//	        validators:
//	          - cmp(ge=1)
//	          - const(0) or cmp(ge=10)
//
// Listing a type marks it unless ignore is true. Its hooks are then on by
// default. Each entry of validators is the text of one item, a rule or a
// keyword item of a role, of nested or of a description, read as a line
// of a directive is. An entry holding another text, as an object, is an
// error. The validators of a type stand for the rules of its doc comment.
// A validator listed twice on one field or one type is an error. The file
// has no equivalent of //kama:validate file.
// version is required and is 1. An unknown key is a warning wrapping
// genopt.ErrUnknownKey that names the file and the key path. An entry
// holding unknown keys only does not list its type or field, so a
// misspelt ignore leaves the directives in effect. The entry of a
// type replaces every directive on the type and its fields, and the proc
// reports a warning wrapping genopt.ErrOverridden for a type whose
// directives are replaced. An entry giving pre, post or both and nothing
// else adds those functions to the directives of the type instead, without
// a warning, unless a directive of the type gives one of them too. A type
// another generator wrote, which carries directives the user cannot edit,
// gets functions of its own that way. The free-floating directives stay in
// effect beside the file.
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
// and its directives, with a warning for each. An entry giving only pre and
// post adds them to the entry it comes before in the same way.
//
// # Output
//
// Each source file declaring targets gets a file of the same package next
// to it, named by inserting .kama_validate before the build constraint
// suffixes of its name, as in x.kama_validate_linux.go for x_linux.go,
// with the //go:build line of the source file. Every generated declaration
// carries the marker //kama:generated kama:validate.
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
// Every generated file starts with a header naming the generator
// kama:validate, the option_file and package_option_file it read, and the
// time of the run.
// The proc keeps a file that differs from the existing one in the generated:
// line only. A run without changes therefore leaves every file as it was.
//
// A file of the loaded packages is stale when it holds a declaration with
// the marker of the generator and the run does not produce it. The proc
// reports a warning wrapping ErrStale for a stale file. The file is left in
// place; delete it if it is no longer needed. A stale file referring to a
// type that no longer exists makes loading the packages fail until it is
// deleted, before the warning can be reported. Two steps loading
// overlapping packages report the files of each other as stale, so each
// package belongs to one step.
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
//   - Rules on the fields of a struct type literal. Give the struct a name
//     and validators of its own.
//   - Targets of packages outside the run, and types other than struct,
//     slice and map types without validators of their own.
package validator
