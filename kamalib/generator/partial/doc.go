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
// identifiers the methods and helpers declare. A package-level declaration
// named like a predeclared identifier the generated code spells, such as
// make, len, new, nil or error, is an error; the generated code spells
// interface{} rather than any. The methods spell a blank type parameter by
// a fresh name, and the partial type declaration keeps it blank.
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
// The partial struct leaves out a blank field _ of the source struct,
// since nothing can refer to it. FromValue, ToValue and Merge skip it.
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
// # Helpers
//
// The methods call the option API directly where it converts or merges a
// value, as und.Defined, und.FromPointer(v).CollapseToNull(),
// und.WrapSlice, und.WrapMap, und.FromOption, Value and Flatten. A
// conversion through the methods of a partial type, an append merge, a
// recursive merge and the test of an embedded partial value for every
// field undefined go through helper functions instead, one per type:
//
//	FromTarget      M or *M -> und.Und[MPartial]
//	FromTargetRef   *M -> und.Und[*MPartial]
//	ToTarget        und.Und[MPartial] -> *M
//	ToTargetRef     und.Und[*MPartial] -> *M
//	FromEmbedded    *M -> MPartial              embedded *M
//	ToEmbedded      MPartial -> *M              embedded *M
//	MergeAppend     (und.Und[T], und.Und[T]) -> und.Und[T]
//	MergeRecursive  (und.Und[T], und.Und[T]) -> und.Und[T]
//	AllUndefined    MPartial -> bool            embedded M or *M
//
// AllUndefined tests the fields of MPartial with IsUndefined, and the
// fields of a partial value embedded in it with its own AllUndefined. It
// skips blank fields, which a partial struct does not have. It cannot read an unexported
// field of a partial type of another package, so embedding a target of
// another package that has unexported fields, where AllUndefined is
// needed, is an error wrapping ErrUnsupported.
//
// A helper is named kamaPartial, its op as above, _, the first 5 bytes of
// the SHA-256 of the canonical encoding of its parameter types, _ and
// those of its result types, in lowercase hexadecimal. The helper
// converting *Address of package example.com/m into
// und.Und[AddressPartial] is kamaPartialFromTarget_2b27c0c476_7bfb4f6244.
// The op is in the name only, so two ops of one signature differ in their
// prefix.
//
// Helpers of one op whose signatures are not identical but hash alike get
// that name followed by _1, _2 and so on up to _ff, in lowercase
// hexadecimal, numbered in the order of their encodings, parameters then
// results. The number of a helper may change between runs when the
// helpers of its package change. More than 0xff such helpers are an error
// wrapping ErrConflict.
//
// The canonical encoding is defined by type identity: identical types, as
// types.Identical tells them, encode alike, and other types differently.
// It is built from the types, not from how they are printed. Each node
// starts with a byte telling its kind, and each string and list with its
// length as an unsigned varint:
//
//	list       L, count, types
//	basic      b, kind               byte and rune as uint8 and int32
//	pointer    p, elem
//	slice      s, elem
//	array      a, length, elem
//	map        m, key, elem
//	chan       c, direction, elem
//	named      n, package path, name, list of type arguments
//	partial    P, package path, name of the target, list of type arguments
//	type param t, index, constraint  r, index within its own constraint
//	struct     S, count, per field: name, package path of an unexported
//	           name or empty, embedded, type, tag
//	func       F, list of parameters, list of results, variadic
//	interface  I, count, per method sorted by name: name, package path of
//	           an unexported name or empty, signature; count and embedded
//	           elements other than interfaces; comparable
//	union      U, count, per term: tilde, type
//
// Aliases are resolved at every level, and parameter names and type
// parameter names are left out. Two type parameters of one index and alike
// constraints encode alike although types.Identical tells them apart: a
// generic helper declares type parameters of its own, so the helpers of
// two receivers spelling *Item[T] and *Item[U] are one.
//
// The partial variant of a target does not exist before generation, nor
// need und.Und be imported by the package. A helper signature holds a
// synthetic named type for each: one origin per type, of the package path
// and name it stands for, instantiated with the type arguments spelled.
// und.Und encodes as the named type it stands for, and a partial variant as
// partial, naming its target.
//
// Within a run, two helpers share a name exactly when their signatures are
// identical, or encode alike for generic helpers. Identical signatures
// given two names would break the encoding and are an error wrapping
// ErrConflict. The doc comment of a helper spells its types
// as its code does, as "converts *Address into und.Und[AddressPartial]".
//
// A helper is generic only over the type parameters of the partial type
// its types mention, and those their constraints mention, which keep their
// names and constraints. The methods call it with explicit type arguments.
// Any other helper is not generic, so the compiler specializes it to its
// types rather than sharing one instantiation among all pointer types.
//
// Each helper is declared once per package, in a partial file calling it:
// the first one by path without a build constraint, or if every file
// calling it has one, the first one by path. A file without a build
// constraint is part of every build variant, so the helpers it calls are
// found in every variant. A helper called by constrained files only is
// declared in one of them, which assumes those files are built together.
// A file declares its helpers sorted by name. A package-level declaration
// named like a helper is an error.
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
// the //go:build line of the source file. The partial files declare the
// helpers they host, described under Helpers. Every generated declaration
// carries the marker //kama:generated kama-partial.
//
// Every generated file starts with this header:
//
//	// Code generated by github.com/ngicks/kama/kamalib/generator/partial. DO NOT EDIT.
//	// params:
//	//	merge: "overwrite"
//	//	merge_nested: "recursive"
//	//	option_file: "kama/partial.yaml"
//	// generated: 2026-11-04T10:12:33Z
//
// genhelper.GeneratorParams prints the header. params lists merge,
// merge_nested and option_file sorted by key, with the defaults applied,
// each value as JSON. option_file is the name of the option file read, as
// OptionFile.Name gives it, and null without an option file. generated is
// the time Options.Now returns, in UTC to the second.
//
// Proc keeps a file that already exists, on disk or staged by an earlier
// step, when the file it would write differs from it in the generated:
// line only, so that a run without changes leaves every file byte for byte
// as it was. Generate itself always returns the time of the run; a caller
// writing its files compares them with genhelper.SameExceptGenerated to do
// the same.
//
// A file of the loaded packages is stale when it holds a declaration with
// the marker of the generator and the run does not produce it. A partial
// file becomes stale when its source file no longer declares targets.
// Generate returns a warning wrapping ErrStale for a stale file, naming its
// path relative to the module root, and Proc reports the warning. The file
// is left in place; delete it if it is no longer needed. If it refers to a
// type that no longer exists, or declares a helper the run declares in
// another file, loading the packages fails until the file is removed.
// Files of build variants other than the one loaded are not in the loaded
// packages and are never reported.
//
// The flag set file of a package the run produces no files for is never
// reported. The run cannot tell whether the package still declares targets
// in files of build variants other than the one loaded, and those partial
// files need it. A package whose targets are all gone keeps its
// kama_partial_flagset.go without a warning; delete it by hand. The file
// kama_partial_helpers.go earlier versions wrote is stale like any other
// file the run does not produce.
package partial
