// Package partial generates partial variants of data models.
//
// The partial variant XPartial of a type X has the type parameters of X,
// and its fields can each be undefined, null or defined, so that a value
// can describe a change to some fields of an X. The proc of NewProc runs
// Generate as the workflow step kind partial.generate.
//
// # Targets
//
// Targets are taken from the loaded packages only, by directives or by the
// option file entries standing for them:
//
//   - //kama:partial in the doc comment of a type marks it. In the doc
//     comment of a type declaration with several specs, as in
//     type ( A struct{}; B struct{} ), it marks every type of the
//     declaration, and so do the other directives of types.
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
// A type of a package outside the run is an implementor when its package
// declares its partial variant with the marker of this generator, as an
// earlier run on that package leaves it. An implementor is no target: the
// run declares nothing for it. Its fields map as the fields of a target
// do, to the partial variant its package already has. The packages
// outside the run are those the loaded packages import, directly or not.
// A type named like a partial variant without the marker gives no
// implementor. A type of a loaded package is never an implementor, so a
// type the run leaves out gets no partial form from a file of an earlier
// run.
//
// Only defined types whose underlying type is a struct, a slice or a map
// have partial variants. File marks, package marks and reachability skip
// other types silently; an explicit //kama:partial on one, or on an alias,
// is an error. Declarations whose doc comment carries the genmark marker
// of a registered generator are never targets, so files emitted earlier are
// read like any other file and regenerated. Options.Registry tells the
// registered generators. Other args of //kama:partial, or //kama:partial
// on a field, are errors. //kama:partial:merge is described under Merge,
// //kama:partial:flag under Flags, //kama:partial:fieldmask under Field
// mask, and any other //kama:partial:<name> is an error.
//
// Several lines of one directive on one target count as one line. Their
// args are joined in source order, so //kama:partial:flag name=a on one
// line and //kama:partial:flag usage="the a" on the next give one flag. A
// name given twice in the joined args is an error. Errors about joined
// lines name the position of the first line. The free-floating comments
// of one file are one target.
//
// # Test files
//
// Targets may live in _test.go files, so that tests get partial variants
// of the types they declare. A load with tests, as packages.load with
// tests: true, gives two shapes, and Generate handles both:
//
//   - A _test.go file of the package x. The test variant of x holds the
//     files of x and these test files, and the run reads it in place of x.
//     The files of x are generated once, as without tests. A target of
//     a_test.go gets a.kama_partial_test.go.
//   - The external test package x_test, declared by the _test.go files of
//     the directory of x with the package clause x_test. Its import path
//     is the path of x followed by _test. Its partial files carry the
//     package clause x_test. Its helpers file and flag set file take the
//     _test suffix, zz_gen.helper.kama_partial_test.go and
//     zz_gen.flagset.kama_partial_test.go, so that they do not meet the
//     files of x in the one directory.
//
// A helper only _test.go files of x call goes to the helpers file of the
// first of them, as in zz_gen.helper.a.kama_partial_test.go for
// a_test.go, and so stays out of the build of x. A helper the files of x
// call too stays in zz_gen.helper.kama_partial.go. The flag set file of x
// serves its test files too, and stays zz_gen.flagset.kama_partial.go even
// when only test files of x declare targets: the name with the _test
// suffix belongs to x_test.
//
// The package option file found in the directory of x belongs to x. The
// option file names the types of x_test under the path of x_test, and a
// free-floating //kama:option-file directive in a file of x_test names a
// package option file of its own.
//
// With tests loaded, file marks, package marks, all_types and reachability
// take in the types of the test files too, and a test type that cannot
// have a partial variant fails the run as any other type does. A target of
// a test file reaches the types of the files of x as well, and those get
// partial variants in the partial files of x.
//
// A run without tests reads neither test files nor x_test and reports no
// file of the tests stale. It produces the files of x as a run with tests
// does unless the tests reach types of x that nothing else reaches: their
// partial variants are then left out and their files reported stale. An
// option file entry naming a type of the tests, and a //kama:option-file
// directive in a _test.go file of x, need the tests loaded. Once test
// files declare targets, run the generator with tests every time.
//
// # Option file
//
// An option file gives the options of the directives for types that cannot
// carry them, such as generated code. ProcParam.OptionFile names the file
// the proc reads. Options.Loaded holds the options of the directives and the
// option files. The Loader of NewLoader loads them. A package may also
// have an option file of its own, described under Package option
// file. The file is JSON, YAML or TOML:
//
//	version: 1
//	packages:
//	  example.com/gen/pb:         # package import path
//	    all_types: true           # //kama:partial package
//	    types:
//	      Server:                 # //kama:partial on Server
//	        fieldmask: true       # //kama:partial:fieldmask on Server
//	        fields:
//	          Addr:
//	            merge: append     # //kama:partial:merge append
//	            flag:
//	              name: addr
//	              short: a
//	              usage: listen address
//	            fieldmask:        # //kama:partial:fieldmask name=address
//	              name: address
//	      Client:
//	        ignore: true          # //kama:partial ignore on Client
//
// Listing a type marks it, as //kama:partial on it does, unless ignore is
// true. A listed type that has no partial variant is an error, as for the
// directive. fieldmask of a type stands for //kama:partial:fieldmask on
// it. Fields take the options of //kama:partial:merge, of the args of
// //kama:partial:flag and of the args of //kama:partial:fieldmask. The file has no equivalent of
// //kama:partial file. version is required and is 1. A value of the wrong
// type, a package that is not loaded, a type the package does not declare,
// a field its struct type does not declare and fields of a type that is
// not a struct type are errors naming the file and the key path. An
// unknown key is a warning wrapping genopt.ErrUnknownKey that names the
// file and the key path. An entry holding unknown keys only does not list
// its type or field, so a misspelt ignore leaves the directives in effect.
// Keys are case-sensitive. A JSON file is decoded
// with encoding/json/v2. A duplicate key, invalid UTF-8 and data after the
// top-level value make it fail to load.
//
// The entries of the file and the directives give the same output. The
// options of one type come from one source: its entry in an option file,
// or else its directives. The entry of a type replaces every directive on
// the type, on its type declaration and on its fields, even those the entry
// does not set. Generate returns a warning wrapping genopt.ErrOverridden
// for a type whose directives are replaced, naming the position of the
// first directive and the key path of the entry. The free-floating
// directives stay in effect beside the file, so //kama:partial file and
// //kama:partial package still mark types. Errors about an option name its
// directive position or its key path in the file.
//
// # Package option file
//
// A package option file gives the options of a single package. Its
// document has no packages level: version, all_types and types sit at the
// top, with the meaning and vocabulary of one entry of packages of the
// option file. The proc reads the package option file of every loaded package:
//
//	version: 1
//	all_types: true
//	types:
//	  Client:
//	    ignore: true
//
// A free-floating comment of the package, the package clause doc comment
// included, names the file:
//
//	//kama:option-file partial=../options/pb.yaml
//
// option-file is a directive of the tool shared by its generators. Each
// arg is <generator>=<path>. partial reads the arg keyed partial and
// leaves the others to their generators. The path is
// slash-separated and relative to the package directory. It may hold "..",
// but an absolute path or a path leaving the root of the out of the proc is an
// error. The extension gives the format: .yaml, .yml, .json or .toml. A
// named file that does not exist is an error. Several packages may name one
// file; its options apply to each of them. The directive follows the rule
// of joined lines. A package naming partial more than once, in one comment
// or in several files, uses the last one in file name and position order,
// and the proc reports a warning for each other one.
//
// A package without the directive gets kama_partial.toml, .yaml, .yml or
// .json in its directory, the first one present in that order, and no
// package option file when none is present.
//
// The options of a package option file meet those of the directives and of
// the option file under the rule above, with the package option file first:
// the entry of a type in the package option file replaces the entry of the
// type in the option file and the directives of the type, with a warning
// for each. all_types of a package option file replaces the all_types of
// the entry of the package in the option file the same way, when both set
// it. Errors about an entry name the file and the key path from its top
// level.
//
// # Type mapping
//
// A struct target maps each field type:
//
//	T                   und.Und[T]
//	M                   und.Und[MPartial]  (M a target or an implementor)
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
// An unknown member fallback of encoding/json/v2, a field of map type that
// is not embedded and whose json tag has the embed option, keeps its type
// and its tag. encoding/json/v2 rejects und.Und and any other option on
// such a field. A fallback has no undefined state: nil captured nothing.
//
// A target with an underlying []T or map[K]V maps to []T' or map[K]V',
// where T' and V' map T and V as a struct field type.
//
// # Source layout
//
// A struct target declared by a struct literal, as in type A struct{ ... },
// gets a partial struct built on a copy of that literal by
// github.com/ngicks/kama/kamalib/pkg/dstreplace. The copy keeps the order
// of the fields, the blank lines between them, the names that share one
// field and the tag literals as written. The copy replaces the type of
// every field with its mapped type. The generator spells each mapped type
// anew. The partial struct therefore refers to no import of the source
// file. A tag literal stays as written when the partial tag equals the
// source tag. Otherwise its value is replaced.
//
// The copy holds no comment of the source: field comments, line comments
// and directives are all erased. The doc comment of a partial type is
// generated.
//
// The copy keeps blank names. A blank name keeps the type of the source
// field, so a blank name grouped with other names gets a field of its own.
// Grouped names whose partial types or tags differ get one field each. An
// exported name and an unexported name of one group without a json tag get
// different tags.
//
// The generator builds slice and map targets, targets defined on another
// type and type parameter lists from the types. A target defined on
// another type is written as type A B or type A sub.S.
//
// # Struct tags
//
// The json tag gets omitzero and loses omitempty, since encoding/json/v2
// omits a null value under omitempty. An exported field without a json tag
// gets json:",omitzero". A yaml tag present on the source field gets
// omitempty and loses omitzero, which go.yaml.in/yaml/v4 rejects. A toml
// tag present on the source field gets omitzero and loses omitempty. A
// tag of "-" is kept as is, and other keys are copied unchanged. The tag
// of an unknown member fallback is copied unchanged.
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
//	unknown member fallback          copied
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
// The partial struct copies a blank field _ of the source struct with its
// type and tag as written. A blank field may direct the compiler, as
// _ structs.HostLayout does, so the partial struct needs it too. The type
// is not wrapped in und.Und and not replaced by a partial type. Nothing
// can refer to a blank field, so FromValue, ToValue and Merge skip it.
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
// An unknown member fallback of r replaces that of p when it is not nil.
// Its strategy is always overwrite, so an append default leaves it whole.
//
// Options.Merge, overwrite or append, is the strategy of fields whose
// value is a slice or a map, as for []T, *[]T and opt.Option[map[K]V].
// Options.MergeNested, recursive or overwrite, is the strategy of fields
// holding partial values. Other fields, elastic.Elastic included, always
// overwrite. //kama:partial:merge <strategy> on a source field sets its
// strategy: overwrite or append for slices and maps, recursive or
// overwrite for partial values, any of the three for slice and map
// partial values, and overwrite for unknown member fallbacks and other
// fields. Any other strategy, args
// holding more than one strategy, the directive on a type or a file, and
// the directive on a field of a type without partial variant are errors.
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
// tests an unknown member fallback against nil and skips blank fields,
// which hold no partial value. It cannot read an
// unexported field of a partial type of another package, so embedding a
// target of another package that has unexported fields, where
// AllUndefined is needed, is an error wrapping ErrUnsupported.
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
// Each helper is declared once per package, in a helpers file. The partial
// files declare no helper: two of them may call one helper, which belongs
// to neither.
//
// The helpers file of a package is zz_gen.helper.kama_partial.go. It has no
// build constraint and declares every helper that a partial file without a
// build constraint calls. Such a partial file is part of every build
// variant and calls the same helpers in each, so the helpers file comes
// out the same whichever variant a run loads.
//
// A helper called by constrained files only is declared in the helpers
// file of the first of them by path. That file is named zz_gen.helper.
// followed by the name of the partial file, as in
// zz_gen.helper.x.kama_partial_linux.go for x_linux.go, and carries the
// //go:build line of the source file. The name of the source file is kept
// so that two constrained source files of a package get a helpers file
// each, and so that a //go:build line, which no file name shows, still
// tells the files apart. The types of such a helper may not exist in
// another build variant, and a run loading another variant would drop the
// helper from zz_gen.helper.kama_partial.go. This placement assumes the
// constrained files calling one helper are built together.
//
// A helpers file declares its helpers sorted by name. A package-level
// declaration named like a helper is an error, and so is a declaration in
// a helpers file that the generator did not write.
//
// # Flags
//
// A source struct field opts in to a command-line flag with
//
//	//kama:partial:flag name=<name> short=<c> usage="<text>"
//
// The args are parsed by dircmnt.ParseArgsKeyValue. name is required, and
// has no leading -, no white space, no control character and no =. short is
// optional and is a single ASCII character. usage is optional. An unknown
// key, a key given twice, the directive on a type or a file,
// the directive on a field of a type without partial variant, the
// directive on a blank field or on an unknown member fallback, and two
// fields of one partial type with one flag name or shorthand are errors.
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
// PartialFlagSet is declared in the file zz_gen.flagset.kama_partial.go of
// each package holding targets. It holds every method the flag types above
// call, whether the package uses it or not, so that partial files of build
// variants other than the one loaded keep finding theirs. The methods have
// signatures of basic and time types, so *pflag.FlagSet of
// github.com/spf13/pflag implements it and the generated code imports no
// flag package. The file has no build constraint and is generated on every
// run, and the proc keeps it as it was when only its generated: line would
// change; a declaration in it the generator did not mark is an error,
// and so is PartialFlagSet declared elsewhere in the package.
//
// Options.OmitFlagSet, the key flag_set: false of the param of the proc,
// leaves the file out. A run generating into packages without any flag
// option does not need it. A flag option on a target of the run is an
// error then, wrapping ErrOption. A flag set file an earlier run wrote is
// reported as stale.
//
// # Field mask
//
// A struct target opts in to field masks with //kama:partial:fieldmask in
// its doc comment. A field mask is a list of paths naming fields. Its
// paths follow those of google.protobuf.FieldMask, and the generated code
// takes them as []string. A partial type of a target with field masks gets
// these methods:
//
//	func (p XPartial) FieldMask() []string
//	func (p *XPartial) FromValueWithMask(v X, paths []string) error
//
// Partial types of other targets get neither method. They neither declare
// nor promote one. A field of such a type may share a name with one of
// them.
//
// A path joins the segments of nested fields with dots. Segments are
// case-sensitive. The segment of a field is its Go name by default.
//
//	//kama:partial:fieldmask name=<segment>
//
// on the field sets another segment. dircmnt.ParseArgsKeyValue parses the
// args.
// name is required. It must not be empty or hold a dot. Json tags and
// other spellings of the field name give no segments. A path holds no
// index, map key or wildcard. These are errors:
//
//   - an unknown key, or a key given twice
//   - args on a type
//   - the directive on a type without a struct partial variant, or on a
//     free-floating comment
//   - name on a field of a type without field masks, on a blank field, on
//     an unknown member fallback or on an embedded partial value
//   - two fields of one type with one segment
//   - an embedding type and an embedded partial value of which only one
//     has field masks
//
// A path addresses every field of the partial struct as a whole, except an
// unknown member fallback, which has no path. Some
// fields hold a partial value of a struct target with field masks, as M,
// *M and a pointer closing a cycle do. Paths below the segment of such a
// field address the fields of that value, as in Field.Child. A path
// addresses a partial value of a target without field masks only as a
// whole. The same holds for slices, maps and slice and map targets, even
// when their elements are partial values. An embedded partial value has no
// segment. Its paths are paths of its parent, in the way Go promotes the
// fields of an embedded struct. Its type must have field masks too. Its
// segments must differ from those of the parent and of other embedded
// partial values.
//
// The run knows no option of an implementor, so the FieldMask method of
// its partial variant tells whether it has field masks. Paths below the
// segment of a field holding such a partial value address its fields, as
// for a target with field masks. A target with field masks must not
// embed an implementor, whose segments the run cannot give. A target
// without field masks must not embed an implementor whose partial
// variant has FieldMask. Both are errors wrapping ErrUnsupported.
//
// An embedding type and the struct targets it embeds agree on field masks.
// A target without field masks must not embed a partial value of a target
// with field masks. Go would promote FieldMask and FromValueWithMask of
// the embedded partial value, and these methods know nothing of the
// fields of the embedding type. Both directions are errors.
//
// FieldMask returns the sorted paths of the fields p sets. It returns nil
// when p sets no field. An undefined field gives no path. A null field
// gives its own path. A defined field gives its own path even for a zero
// value. A defined field holding a partial value with field masks gives
// the paths of the FieldMask of that value below its segment instead. A
// defined partial value setting nothing therefore gives no path. A defined
// nil pointer of a field closing a cycle converts back to nil like null.
// It gives its own path too. FieldMask writes to no field of p.
//
// FromValueWithMask sets p to the partial form of the fields of v that
// paths name. Every other field of p becomes undefined.
//
//   - A field a path names gets what FromValue gives it. A zero value stays
//     defined, and a nil pointer, slice or map becomes null.
//   - A path below a field holding a partial value builds that value with
//     its own FromValueWithMask and the rest of the path. The field becomes
//     defined. A nil pointer in the field of v holds none of the fields the
//     paths name, so the field becomes null.
//   - A path naming a field covers the paths below it in any order. A
//     repeated path counts once.
//   - The paths of an embedded partial value go to its FromValueWithMask.
//     A nil embedded pointer leaves every field of the embedded partial
//     value undefined, as FromValue does.
//   - Empty or nil paths give the zero partial value. They never select
//     every field.
//
// An empty path, an empty segment, an unknown segment and a path below a
// field addressed only as a whole are errors. FromValueWithMask checks
// every path. The value of v and the other paths do not affect the check.
// The generated code builds the errors with fmt.Errorf. The error text
// starts with the path and a colon and then names the reason, as in
// Inner.Nope: unknown field Nope. For a path below a field, the error of
// the FromValueWithMask of the value gets the segment of the field and a
// dot in front. An empty path gives the text empty path. On error,
// FromValueWithMask leaves p unchanged.
//
// A field mask records which fields a partial value sets. It does not
// record whether they are null or defined, and it drops a defined partial
// value setting nothing. A field mask therefore does not round-trip a
// partial value on its own.
//
// ApplyTo of the result of FromValueWithMask changes only the fields the
// paths name when every field holding a partial value merges with the
// recursive strategy. Under overwrite, a path below such a field replaces
// the whole partial value of the field. A null from a nil pointer clears
// the whole field under every strategy.
//
// # Output
//
// Each source file declaring targets gets a file of the same package next
// to it, named by inserting .kama_partial before the build constraint
// suffixes of its name, as in x.kama_partial_linux.go for x_linux.go, and
// the //go:build line of the source file. The helpers the partial files
// call go to the helpers files described under Helpers. Every generated
// declaration carries the marker //kama:generated kama:partial.
//
// The files other than the partial files start with zz_gen.: the helpers
// file zz_gen.helper.kama_partial.go, the helpers files of constrained
// source files and the flag set file zz_gen.flagset.kama_partial.go. The
// prefix marks them as generated and sorts them after the files written by
// hand.
//
// Every generated file starts with this header:
//
//	// Code generated by github.com/ngicks/kama/kamalib/generator/partial. DO NOT EDIT.
//	// params:
//	//	merge: "overwrite"
//	//	merge_nested: "recursive"
//	//	option_file: "kama/partial.yaml"
//	//	package_option_file: "model/kama_partial.yaml"
//	// generated: 2026-11-04T10:12:33Z
//
// genhelper.GeneratorParams prints the header. params lists merge,
// merge_nested, option_file and package_option_file sorted by key, with
// the defaults applied, each value as JSON. option_file is the name of the
// option file read, relative to the root of the out of the proc, and null
// without an option file. It is the list of names when Options.Loaded
// holds several. package_option_file is the name of the package option
// file of the package the file belongs to, and null when the package has
// none.
// generated is the time Options.Now returns, in UTC to the second.
//
// The proc keeps a file that already exists, on disk or staged by an earlier
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
// path relative to the module root, and the proc reports the warning. The file
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
// zz_gen.flagset.kama_partial.go without a warning; delete it by hand. A
// helpers file is stale like any other file the run does not produce:
// zz_gen.helper.kama_partial.go serves the partial files without a build
// constraint only, which every run loads.
package partial
