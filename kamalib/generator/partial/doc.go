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
// read like any other file and regenerated. Other args of //kama:partial,
// or //kama:partial on a field, are errors. Sub-directives such as
// //kama:partial:name are left alone.
//
// A target holding its own partial variant by value, as a struct with a
// field of type *X inside X, is an error: und.Und holds its value
// directly, so the partial variant would be an invalid recursive type.
//
// # Type mapping
//
// A struct target maps each field type:
//
//	T                   und.Und[T]
//	M                   und.Und[MPartial]  (M a target)
//	*T, *M              as T and M
//	**T                 error
//	opt.Option[T]       und.Und[T]
//	und.Und[T]          und.Und[T]
//	elastic.Elastic[T]  elastic.Elastic[T]
//	Box[int]            und.Und[BoxPartial[int]]  (Box a generic target)
//
// The option types are github.com/ngicks/option/{opt,und,elastic}. Any
// other type maps as a whole as T does: slices, maps and arrays, including
// those of targets, and funcs, channels and interfaces. Type arguments are
// kept as written. Unexported fields are kept.
//
// An embedded target M or *M stays embedded as MPartial, and its tag is
// copied unchanged: an unnamed embedded struct is inlined by encoders, and
// omitzero would not apply to it. An embedded target with a named tag is
// not given omitzero either. Any other embedded type T becomes the field T
// of the mapped type of T.
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
// # Output
//
// Each source file declaring targets gets a file of the same package next
// to it, named by inserting .kama_partial before the build constraint
// suffixes of its name, as in x.kama_partial_linux.go for x_linux.go, and
// the //go:build line of the source file. Every generated declaration
// carries the marker //kama:generated kama-partial.
//
// A file the generator wrote earlier whose source no longer declares
// targets is left in place. If it refers to a type that no longer exists,
// loading the packages fails until the file is removed.
package partial
