// Package partial generates partial variants of data models.
//
// The partial variant XPartial of a type X has the type parameters of X,
// and its fields can each be undefined, null or defined, so that a value
// can describe a change to some fields of an X. Proc runs Generate as the
// workflow step kind partial.generate.
//
// # Targets
//
// Targets are taken from the loaded packages only, by directives or by the
// option file entries standing for them:
//
//   - //kama:partial in the doc comment of a type marks it.
//   - //kama:partial file in a free-floating comment, one that is not the
//     doc or line comment of any declaration, marks every type of the
//     file.
//   - //kama:partial package in a free-floating comment marks every type
//     of the package. The package clause doc comment is free-floating.
//   - //kama:partial ignore in the doc comment of a type exempts it from
//     file and package marks and from reachability.
//   - Every named type declared in a loaded package and reachable from a
//     target through struct fields, slice and array elements, map values
//     and pointers is a target too. Map keys, channel elements and type
//     arguments do not make a type reachable. Reachability continues
//     through targets only, so a type behind an ignored type or a named
//     array type is not reached through it.
//
// Only defined types whose underlying type is a struct, a slice or a map
// have partial variants. File marks, package marks and reachability skip
// other types silently; an explicit //kama:partial on one, or on an alias,
// is an error. Declarations whose doc comment carries the genmark marker
// of a registered generator are never targets, so files emitted earlier are
// read like any other file and regenerated. Options.Registry tells the
// registered generators. Other args of //kama:partial, or //kama:partial
// on a field, are errors. //kama:partial:merge is described under Merge,
// //kama:partial:flag under Flags, and any other //kama:partial:<name> is
// an error.
//
// # Option file
//
// An option file gives the options of the directives for types that cannot
// carry them, such as generated code. Options.OptionFile holds it, and
// ProcParam.OptionFile names the file Proc reads. The file is JSON, YAML or
// TOML:
//
//	version: 1
//	packages:
//	  example.com/gen/pb:         # package import path
//	    all_types: true           # //kama:partial package
//	    types:
//	      Server:                 # //kama:partial on Server
//	        fields:
//	          Addr:
//	            merge: append     # //kama:partial:merge append
//	            flag: {name: addr, short: a, usage: listen address}
//	      Client:
//	        ignore: true          # //kama:partial ignore on Client
//
// Listing a type marks it, as //kama:partial on it does, unless ignore is
// true. A listed type that has no partial variant is an error, as for the
// directive. Fields take the options of //kama:partial:merge and of the
// args of //kama:partial:flag. The file has no equivalent of
// //kama:partial file. version is required and is 1. An unknown key, a
// value of the wrong type, a package that is not loaded, a type the
// package does not declare and a field its struct type does not declare
// are errors naming the file and the key path.
//
// The entries of the file and the directives give the same output. An
// option set by both a directive and the file is allowed when both give
// the same value. Different values, as for a type marked by one and
// ignored by the other, or two merge strategies or flags of one field, are
// errors wrapping kamaopt.ErrConflict and naming both origins. Errors
// about an option name its directive position or its key path in the
// file.
//
// # Type mapping
//
// A struct target maps each field type:
//
//	T                   und.Und[T]
//	M                   und.Und[MPartial]  (M a target)
//	*T, *M              as T and M
//	*M                  und.Und[*MPartial] (closing a cycle)
//	**T                 error
//	opt.Option[T]       und.Und[T]
//	und.Und[T]          und.Und[T]
//	elastic.Elastic[T]  elastic.Elastic[T]
//	Box[int]            und.Und[BoxPartial[int]]  (Box a generic target)
//
// The option types are github.com/ngicks/option/{opt,und,elastic}. Any
// other type maps as a whole as T does: slices, maps and arrays, including
// those of targets, and funcs, channels and interfaces. Type arguments are
// kept as written. Unexported fields are kept. A target whose partial
// variant is unexported and declared in another package, reached through
// an exported alias as sub.Public = sub.hidden, maps as a whole as T does,
// as und.Und[sub.Public].
//
// und.Und holds its value directly, so a target holding its own partial
// variant through struct fields would be an invalid recursive type. A
// pointer field maps to und.Und[*MPartial] when its owner and its target
// lie in one strongly connected component of the graph of struct fields
// of targets and pointers to targets. Every pointer field on such a cycle
// is mapped so, which does not depend on the order types are visited in.
// Other pointer fields keep und.Und[MPartial]. Slices and maps hold their
// elements indirectly and close no cycle. A cycle through embedded
// pointers only, as in type N struct{ *N }, is an error.
//
// An embedded struct target M or *M stays embedded as MPartial, and its
// tag is copied unchanged: an unnamed embedded struct is inlined by
// encoders, and omitzero would not apply to it. An embedded target with a
// named tag is not given omitzero either. Any other embedded type T,
// including a slice or map target, becomes the field T of the mapped type
// of T, which keeps the name encoders give it.
//
// Two fields of a partial struct with one name, as for an embedded M next
// to a field MPartial, a field named like one of the methods, and a type
// parameter named like a predeclared identifier or like a partial type or
// helper the declarations refer to are errors. Import names avoid
// predeclared and package-level identifiers, type parameters and the
// identifiers the methods declare. A
// package-level declaration named like a predeclared identifier the
// generated code spells, such as make, len, string or error, is an error;
// the generated code spells interface{} rather than any. The methods spell
// a blank type parameter by a fresh name, and the partial type declaration
// keeps it blank.
//
// A target whose underlying struct type is declared in another package,
// as in type A sub.S, is an error when that struct has an unexported
// field, which the methods could not spell.
//
// A target with an underlying []T or map[K]V maps to []T' or map[K]V',
// where T' and V' map T and V as a struct field type.
//
// # Struct tags
//
// The json tag gets omitzero and loses omitempty, since encoding/json/v2
// omits a null value under omitempty. An exported field without a json tag
// gets json:",omitzero". A yaml tag present on the source field gets
// omitempty and loses omitzero, which go.yaml.in/yaml/v4 rejects. A toml
// tag present on the source field gets omitzero and loses omitempty. A
// tag of "-" is kept as is, and other keys are copied unchanged.
//
// # Methods
//
// Every partial type XPartial gets these methods, with the type parameters
// of X:
//
//	func (p *XPartial) FromValue(v X)
//	func (p XPartial) ToValue() X
//	func (p XPartial) Merge(r XPartial) XPartial
//	func (p XPartial) ApplyTo(v X) X
//
// FromValue sets every field to the partial form of the field of v, and
// ToValue converts back:
//
//	T <-> und.Und[T]                 defined; undefined and null give zero
//	slice or map <-> und.Und         nil <-> null, so nil and empty round-trip
//	*T, *M <-> und.Und               nil <-> null; ToValue allocates
//	M <-> und.Und[MPartial]          through the methods of MPartial
//	*M <-> und.Und[*MPartial]        nil <-> null; both sides allocate
//	opt.Option[T] <-> und.Und[T]     none <-> null
//	und.Und, elastic.Elastic         copied
//	embedded M <-> MPartial          through the methods of MPartial
//	embedded *M <-> MPartial         nil <-> every field undefined
//
// A slice or map partial converts element by element, and a nil value
// converts to nil. FromValue does not terminate on a value whose pointers
// form a cycle.
//
// An embedded partial has no undefined state of its own, so ToValue gives
// a nil embedded *M for an MPartial with every field undefined. FromValue
// of a non-nil pointer to an M whose fields all give undefined, as for an
// M holding undefined und.Und fields only, therefore converts back to nil.
//
// A blank field _ of the source struct stays in the partial struct with
// its mapped type. FromValue, ToValue and Merge skip it, since it cannot be
// referred to.
//
// ApplyTo returns ToValue of FromValue of v merged with p.
//
// # Merge
//
// p.Merge(r) merges every field of r onto the field of p. An undefined
// field of r keeps the field of p. Otherwise the strategy of the field
// decides:
//
//   - overwrite: the field of r replaces the field of p.
//   - append: when both are defined, a new slice holds the elements of p
//     then r, or a new map holds the entries of p with those of r set,
//     even when one of them is nil or empty. Two nil values give nil.
//     Otherwise as overwrite.
//   - recursive: when both are defined, the Merge of their partial values.
//     Otherwise as overwrite.
//
// An embedded partial of r counts as undefined when every field of it is
// undefined, so it keeps the embedded partial of p under every strategy.
// Otherwise recursive gives the Merge of both, and overwrite gives the
// embedded partial of r whole. ApplyTo of the zero partial value therefore
// returns v unchanged, except that a non-nil embedded *M whose fields all
// give undefined still converts back to nil, as described above.
//
// Options.Merge, overwrite or append, is the strategy of fields whose
// value is a slice or a map, as for []T, *[]T and opt.Option[map[K]V].
// Options.MergeNested, recursive or overwrite, is the strategy of fields
// holding partial values. Other fields, elastic.Elastic included, always
// overwrite. //kama:partial:merge <strategy> on a source field sets its
// strategy: overwrite or append for slices and maps, recursive or
// overwrite for partial values, any of the three for slice and map
// partial values, and overwrite for other fields. Any other strategy, a
// second directive on a field, the directive on a type or a file, and the
// directive on a field of a type without partial variant are errors.
//
// The Merge of a slice or map partial takes Options.Merge and treats nil
// as undefined: overwrite gives r unless r is nil, append concatenates the
// elements or sets the entries of r on those of p. Elements are not
// merged one by one.
//
// Merge writes to neither p nor r. Its result shares the slices, maps and
// pointers it does not build anew.
//
// # Flags
//
// A source struct field opts in to a command-line flag with
//
//	//kama:partial:flag name=<name> short=<c> usage="<text>"
//
// The args are parsed by kamaopt.ParseArgs. name is required, and has no
// leading -, no white space, no control character and no =. short is
// optional and is a single ASCII character. usage is optional. An unknown
// key, a second directive on a field, the directive on a type or a file,
// the directive on a field of a type without partial variant, the
// directive on a blank field, and two fields of one partial type with one
// flag name or shorthand are errors.
//
// A partial type with at least one flagged field gets these methods:
//
//	func (p *XPartial) RegisterFlags(f PartialFlagSet)
//	func (p *XPartial) FromFlagSet(f PartialFlagSet) error
//
// RegisterFlags defines the flags on f, each with the zero value as
// default. FromFlagSet sets a field to the defined value of its flag when
// f.Changed reports the flag as set, and leaves every other field as it
// is. Merging the result onto a partial value read from a file therefore
// keeps the fields of the flags not given. RegisterFlags keeps nothing of
// what it defines, so the partial type holds no fields beyond those of
// the source: FromFlagSet reads the flags back through the Get methods of
// f, such as GetString. A second RegisterFlags on one f redefines the
// flags, which pflag rejects.
//
// The flag sets the partial field, so the field type is taken after its
// mapping: *T, opt.Option[T] and und.Und[T] give T. T is then one of
//
//	string, bool, int, int64, uint, uint64, float64   StringP, BoolP, ...
//	time.Duration                                     DurationP
//	[]string                                          StringSliceP
//	map[string]string                                 StringToStringP
//	a type whose pointer implements encoding.TextUnmarshaler
//
// A TextUnmarshaler is checked before the underlying type. It is defined
// with StringP and FromFlagSet parses it with UnmarshalText, returning an
// error naming the flag on failure. Another named type converts from the
// flag of its underlying type, so a type defined as time.Duration takes an
// int64 flag. Any other type, *opt.Option[T] and elastic.Elastic included,
// is an error. Flags apply to the direct fields of a partial type only: a
// flag on a field holding a partial value, embedded or not, is an error,
// and the flags of nested partial types are not registered by their
// owner. A named slice or map type declared in a loaded package and
// reached from a target is a target too, so a field of such a type needs
// //kama:partial ignore on the type to take a flag.
//
// PartialFlagSet is declared in the file kama_partial_flagset.go of each
// package holding targets. It holds every method the flag types above
// call, whether the package uses it or not, so that partial files of build
// variants other than the one loaded keep finding theirs. The methods have
// signatures of basic and time types, so *pflag.FlagSet of
// github.com/spf13/pflag implements it and the generated code imports no
// flag package. The file has no build constraint and is generated on every
// run, and Proc keeps it as it was when only its generated: line would
// change; a declaration in it the generator did not mark is an error,
// and so is PartialFlagSet declared elsewhere in the package.
//
// # Output
//
// Each source file declaring targets gets a file of the same package next
// to it, named by inserting .kama_partial before the build constraint
// suffixes of its name, as in x.kama_partial_linux.go for x_linux.go, and
// the //go:build line of the source file. The methods call unexported
// generic helper functions, declared in the file kama_partial_helpers.go
// of each package holding targets. The file holds every helper whether the
// package uses it or not, so that partial files of build variants other
// than the one loaded keep finding theirs. It has no build constraint and
// is generated on every run, and Proc keeps it as it was when only its
// generated: line would change; a declaration in it the generator did not
// mark is an error, and so is a helper name declared elsewhere in the
// package. Every generated declaration carries the marker
// //kama:generated kama-partial.
//
// Every generated file starts with this header:
//
//	// Code generated by github.com/ngicks/kama/kamalib/generator/partial. DO NOT EDIT.
//	//
//	// generator: kama-partial
//	// params: merge=overwrite merge_nested=recursive option_file=kama/partial.yaml
//	// generated: 2026-11-04T10:12:33Z
//
// params lists merge, merge_nested and option_file in this order, with the
// defaults applied. option_file is the name of the option file read, as
// OptionFile.Name gives it, and none without an option file. A value that
// is empty or holds a space, a control or non-ASCII character, = or " is
// quoted as a Go string. generated is the time Options.Now returns, in UTC
// to the second.
//
// Proc keeps a file that already exists, on disk or staged by an earlier
// step, when the file it would write differs from it in the generated:
// line only, so that a run without changes leaves every file byte for byte
// as it was. Generate itself always returns the time of the run; a caller
// writing its files compares them with SameExceptGenerated to do the same.
//
// A file of the loaded packages is stale when it holds a declaration with
// the marker of the generator and the run does not produce it. A partial
// file becomes stale when its source file no longer declares targets.
// Generate returns a warning wrapping ErrStale for a stale file, naming its
// path relative to the module root, and Proc reports the warning. The file
// is left in place; delete it if it is no longer needed. If it refers to a
// type that no longer exists, loading the packages fails until the file is
// removed. Files of build variants other than the one loaded are not in
// the loaded packages and are never reported.
//
// The helpers file and the flag set file of a package the run produces no
// files for are never reported. The run cannot tell whether the package
// still declares targets in files of build variants other than the one
// loaded, and those partial files need both. A package whose targets are
// all gone keeps its kama_partial_helpers.go and kama_partial_flagset.go
// without a warning; delete them by hand.
package partial
