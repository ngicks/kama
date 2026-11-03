// Package partial generates partial variants of data models.
//
// The partial variant XPartial of a type X has the type parameters of X,
// and its fields can each be undefined, null or defined, so that a value
// can describe a change to some fields of an X. Proc runs Generate as the
// workflow step kind partial.generate.
//
// # Targets
//
// Targets are taken from the loaded packages only:
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
// //kama:partial:flag is left alone, and any other //kama:partial:<name>
// is an error.
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
// parameter named like a partial type or helper the declarations refer to
// are errors. Import names avoid package-level identifiers, type
// parameters and the identifiers the methods declare.
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
//     then r, or a new map holds the entries of p with those of r set.
//     Otherwise as overwrite.
//   - recursive: when both are defined, the Merge of their partial values.
//     Otherwise as overwrite. An embedded partial has no undefined state
//     and always merges by its Merge, or is replaced under overwrite.
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
// # Output
//
// Each source file declaring targets gets a file of the same package next
// to it, named by inserting .kama_partial before the build constraint
// suffixes of its name, as in x.kama_partial_linux.go for x_linux.go, and
// the //go:build line of the source file. The methods call unexported
// generic helper functions, declared in the file kama_partial_helpers.go
// of each package holding targets. The file holds the helpers the package
// uses, has no build constraint, and is written again on every run; a
// declaration in it the generator did not mark is an error, and so is a
// helper name declared elsewhere in the package. Every generated
// declaration carries the marker //kama:generated kama-partial.
//
// A file the generator wrote earlier whose source no longer declares
// targets is left in place. If it refers to a type that no longer exists,
// loading the packages fails until the file is removed.
package partial
