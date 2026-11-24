// Package jsonschema generates Go types from a JSON Schema draft 2020-12
// document.
//
// Generate writes one file, <package>.kama_jsonschema.go, holding a type for
// the root schema and for every schema under it that needs one. The types
// decode with encoding/json/v2. Decoding checks the keywords that decide
// the shape of a value, type and required. The validation keywords become
// //kama:validate directives on the types and their fields, and the
// validator generator of package
// github.com/ngicks/kama/kamalib/generator/validator writes the Validate
// methods checking them. The decoders call Validate where a value converts
// from its wire form to its type, as Validation describes. Errors are those
// of package github.com/ngicks/kama/kamalib/pkg/validation, located by the
// JSON path of the offending member. The generated code imports package
// github.com/ngicks/kama/kamalib/pkg/jsonschema/jsonschemart. Proc runs
// Generate as the workflow step kind jsonschema.generate.
//
// # Proc
//
// Proc reads the schema file from the overlay of its out and stages the
// generated file in it. It takes no in. Its param has these keys:
//
//	schema     the path of the schema file, slash-separated and relative
//	           to the root of the out, optionally followed by # and a
//	           JSON pointer; required
//	dir        the output directory, relative to the root of the out;
//	           the directory of the schema file when left out
//	package    the package clause; the base name of dir when left out
//	type_name  the name of the root type, an exported Go identifier
//	           used as written; the title of the root schema when left
//	           out
//	validate   whether the decoders call Validate; true when left out
//
// The schema file holds JSON. A schema param with a fragment, such as
// tests/minimum.json#/0/schema, takes the value the RFC 6901 pointer of the
// fragment points to as the schema document. The fragment is
// percent-decoded first, as the fragment of a $ref is. The value must be
// an object or a boolean, and it is the root that "#" refers to in a $ref.
// Proc reads it from the raw bytes of the file, so the order of its members
// and the text of its numbers survive. A spec generating the types of
// api/schema.json into api/api.kama_jsonschema.go of package api, then
// their Validate methods:
//
//	version: 1
//	steps:
//	  root:
//	    proc: overlayfs.open
//	  generate:
//	    proc: jsonschema.generate
//	    out: root
//	    param:
//	      schema: api/schema.json
//	      type_name: Pet
//	  load:
//	    proc: packages.load
//	    in: root
//	    after: generate
//	  validate:
//	    proc: validate.generate
//	    in: load
//	    out: root
//	  final:
//	    proc: overlayfs.copy_down
//	    in: root
//	    after: validate
//
// A spec running Proc with validate true needs the validate.generate step.
// Without it the decoders fail with an error wrapping
// jsonschemart.ErrNoValidate. With validate false the directives are still
// written, the decoders call no Validate, and decoding checks no
// validation keyword. A union then takes the first member that decodes a
// value, even one holding a nested union that matches none of its members.
//
// The header of the generated file names the schema, type_name and
// validate params and the time of the run. Proc keeps a file that differs from the
// existing one in the generated: line only. A Go file of the output
// directory that holds a declaration marked by the generator and that the
// run does not produce is stale. Proc reports a warning wrapping ErrStale
// of package github.com/ngicks/kama/kamalib/generator/utils/base for it
// and leaves it in place. Every file the generator marks in one directory
// must therefore come from one run.
//
// A member of a schema that names no keyword of draft 2020-12, such as a
// typo or a keyword of an older draft, gives a warning wrapping
// ErrUnknownKeyword that names its JSON pointer. A member whose name starts
// with x- is an extension keyword and gives none.
//
// # Shapes
//
// A schema declares a Go type of its own when it is an object, an enum of
// strings, a union or a product of allOf. A schema outside an
// object property whose validation keywords apply to its value declares a
// type defined over its built-in type, as type PetName string, whose
// directive names their validators. Other schemas are spelled by built-in
// types where they are used:
//
//	string                       string
//	integer                      int64
//	number                       float64
//	boolean                      bool
//	array with items T           []T
//	array without items, or
//	  with prefixItems           []jsontext.Value
//	object with only
//	  additionalProperties T     map[string]T
//	raw JSON text                jsontext.Value
//
// The type keyword gives the kind. A schema without type admits a value of
// any kind, so whatever keywords it holds, it is raw JSON text, except a
// union, a product of allOf, an enum or a const of one kind, and a $ref
// alone. An integer decodes from any number with a zero fraction, such as
// 1.0 or 1e2.
//
// A schema whose value is of no single Go shape is raw JSON text: a boolean
// schema, a schema without type, a type naming several kinds one of which
// is an object or an array, a type of null only, a type naming null and
// one other kind next to not, if, then or else, which may reject null, an
// enum or a const of values of several kinds, of objects or of arrays, an
// object that makes neither a struct nor a map, and a $ref next to another
// keyword restricting the value. Its Go type is jsontext.Value, and a type it declares, as one
// under $defs or the
// root, is defined over jsontext.Value with the method RawJson, which tells
// the validators to check it as raw JSON text, and carries a Validate. Its
// keywords become the validators of raw JSON text, as Validation lists,
// and the schemas under them become their operands. Such a schema under a
// keyword of raw JSON text declares no type of its own; a schema under
// $defs always may.
//
// Any other schema keeps its Go type whatever other keywords it holds. The
// keywords its Go type does not express, such as not, if or
// patternProperties, check the value encoded as JSON, as Validation
// describes. A schema under such a keyword declares no type of its own.
//
// A root that declares no type of its own still needs a name to decode a
// document into. It declares a type defined over its Go type, such as
// type Root []int64, or type Root opt.Option[string] for a nullable root.
// The type encodes and decodes as its Go type. A schema that refers to a
// root of raw JSON text by $ref takes its name. One that refers to another
// root takes the Go type, so such a root holding itself is an error.
//
// A nullable value is a type naming null and one other kind, or a oneOf or
// an anyOf of exactly a schema and a schema of type null. It is no union.
// In a oneOf the schema must not admit null itself, or null would match
// both members, and the oneOf stays a union.
// Outside an object property it is an opt.Option of the type of the other
// kind.
//
// # Names
//
// The root takes Options.TypeName, else its title. A schema under $defs
// takes its name there. An object property takes the name of its object
// followed by its own name, items "Item" and additionalProperties "Value".
// A member of a union takes the name of its wrapper followed by "Value",
// and a member of an allOf the name of its object followed by the member
// name. The value of a nullable value and the member of an allOf of one
// member take the name of the schema holding them, followed by "Value" when
// that schema is the root, whose type already takes the name. A $ref takes
// the type of the schema it names; a reference declares nothing.
//
// The constant of a value of an enum of strings takes the name of the type
// followed by the name of the value. A value that gives no exported
// identifier takes Value followed by its 1-based index. A value whose name
// an earlier value of the enum takes is followed by the least number from 2
// that makes it distinct, so "foo bar" and "foo_bar" of type Root give
// RootFooBar and RootFooBar2.
//
// Names are upper-camel-cased on the characters an identifier cannot hold,
// as foo_bar and foo-bar give FooBar. A name that gives no exported
// identifier is an error wrapping ErrUnsupported. Two names colliding in
// the package, in a struct or in its wire struct, or a field named like
// a method of the struct, MarshalJSONTo, UnmarshalJSONFrom,
// AdditionalProperties or Validate, is an error wrapping ErrConflict. A
// union named Pet declares PetKind and PetUnknown besides its wrappers, so
// a member titled Unknown collides too.
//
// # Objects
//
// An object becomes a struct with a field per property, in source order.
// The json tag holds the property name as it is. For property p of type T:
//
//	required  nullable  field             tag            wire
//	yes       no        T                 "p"            present, not null
//	no        no        opt.Option[T]     "p,omitzero"   absent is none, null is an error
//	yes       yes       opt.Option[T]     "p"            present, null is none
//	no        yes       und.Und[T]        "p,omitzero"   absent is undefined, null is null
//
// A T that holds null itself, jsontext.Value or a union with a member of
// type null, is the field when required and an opt.Option otherwise.
//
// An object declaring a property whose name holds a comma, a quote, a
// backslash or a control character does not fit a struct and is raw JSON
// text. A member an object requires without declaring a property for it
// stays in _x, which the validators of the value encoded as JSON check.
//
// The struct decodes through an unexported wire struct. It holds a
// property as an und.Und, which keeps a missing member apart from null, or
// as a jsontext.Value where the Go type of the property holds null itself.
// UnmarshalJSONFrom converts each wire field and reports every member
// missing or null against the required x nullable matrix. Once every member
// converts, the struct must pass Validate. An enum of strings declares a
// string type with a constant per value, whose Validate rejects any other
// value.
//
// Every struct holds the members its properties do not name in the
// unexported field _x, tagged json:",embed", which the method
// AdditionalProperties returns. Decoding never rejects such a member, and
// encoding writes them back. _x is a map[string]T for an
// additionalProperties schema T, whose values decode as T, and a
// map[string]jsontext.Value otherwise. additionalProperties false makes
// Validate report each member of _x, with an error wrapping
// validation.ErrClosed located at the member. Next to patternProperties,
// which takes some of those members from additionalProperties, _x is a
// map[string]jsontext.Value and both keywords check the value encoded as
// JSON. encoding/json/v2 refuses a
// tagged unexported field, so every struct encodes through MarshalJSONTo,
// and go vet reports the tag, which the validator generator reads.
//
// # Recursion
//
// A struct cannot hold itself by value, and opt.Option and und.Und do not
// break the cycle. A field of a struct whose type, following the fields
// held by value, reaches the struct again is a pointer *T, as is every
// field on such a cycle. A slice, a map and a union break a cycle. A
// required pointer must not be nil on marshaling. A required nullable
// pointer is nil for null. An optional pointer is nil for absent and has
// omitzero. An optional nullable pointer is nil for absent and for null,
// so null encodes back as absent.
//
// # References
//
// A $ref resolves against the base URI of the schema holding it, which the
// $id of that schema and of the schemas above it give, as package
// github.com/ngicks/kama/kamalib/pkg/jsonschema resolves it. A root without
// $id has an empty base URI. A schema declaring $id below the root is a
// schema resource of its own: a reference names it by its URI, a fragment
// pointer in a reference starts at the resource, and an $anchor is looked
// up in the resource holding the reference. A schema of an embedded
// resource is named as any other schema, by its place in the document.
//
// # Unions
//
// A oneOf or an anyOf, and a type naming two or more kinds other than null
// and no other keyword shaping the value, declare a holder, an interface
// and a wrapper per member:
//
//	type Pet struct{ Kind PetKind }
//	type PetKind interface{ isPetKind() }
//	type PetCat struct{ Cat Cat }
//	type PetDog struct{ Dog Dog }
//
// A member is named by the schema its $ref names, else by its title, else
// by its kind, such as String or Object, when no other member without a
// $ref or a title has that kind, else Option followed by its 1-based
// index. A member of type null is a wrapper without a field. A type of
// several kinds is a union when they are scalars and null, and raw JSON
// text otherwise.
//
// Decoding drops a member whose kinds exclude the value. For a member that
// is an object it also drops it when a required property with a const or
// an enum of one value does not hold that value, when a required member is
// missing and, under additionalProperties false, when the value holds a
// member the object does not declare. The remaining members decode the
// value in schema order, and a member that decodes it matches when its
// wrapper passes Validate. An anyOf and a type of several kinds take the
// first member that matches and do not try later members. A oneOf needs
// exactly one to match and stops at the second, without trying later
// members.
//
// Decoding a union never fails on the selection. A value no member matches,
// or that two members of a oneOf match, gives the wrapper PetUnknown,
// holding the value as jsontext.Value and the error of the selection.
// Validate of the holder returns that error, which holds the error of
// every member tried or names the two members matching. The decoder of a
// struct holding the union reports it through its Validate. A union
// decoded alone reports it only when Validate is called. Marshaling writes
// the member the holder holds, the value of PetUnknown as it is.
//
// The holder has a Validate of its own, which calls the Validate of the
// wrapper it holds. The wrappers carry //kama:validate and get theirs from
// the validator generator. A wrapper encodes as its member. The other
// keywords of the union, the type next to a oneOf or an anyOf included,
// check the value of every member: each wrapper carries json(...) of them,
// as Validation describes, so decoding drops a member whose value fails
// them.
//
// The generated code does not check every validation keyword, as
// Validation lists, so it cannot tell apart two members of a oneOf that
// differ only in keywords one of them leaves unchecked, or in annotations,
// nor a member holding only such keywords, which every value matches. Such
// a oneOf is an error wrapping ErrUnsupported, since the generated code
// would reject values the schema accepts.
//
// # allOf
//
// An allOf of one member takes the type of the member when no other keyword
// restricts the value but a type naming every kind the type of the member
// names. An allOf of objects is a product: a struct with a field per
// member, tagged "-", followed by the fields of its own properties. Every
// member must be a struct, which takes members besides its own, counts
// none and checks nothing on the value encoded as JSON, which would lack
// the members of the others. The allOf must hold no keyword shaping the
// value but type object, properties, required and additionalProperties
// true. Its other keywords check the product encoded as JSON. Any other
// allOf next to a type of one kind takes the Go type of that kind, a struct
// of its own properties for an object, and json(and(...)) checks its
// members. An allOf without type that makes no product is raw JSON text
// checked by and.
// Decoding decodes each member and the own properties from the same
// object. The _x of the product holds the members that neither a member
// nor an own property declares. The product clears the _x of each member
// struct, which holds the members of the others, so a member value of an
// additionalProperties schema of a member is checked on decoding only.
// Marshaling writes one object holding the members of each, then those of
// _x, the first to write a member name winning. A member holding the
// product itself is an error. Two members declaring one property
// differently make no product; declarations differing only in annotations,
// such as title, description, default or an extension keyword, are not
// different. A required member held by a nil pointer is an error on
// marshaling, as in a struct.
//
// # Validation
//
// The validation keywords become args of //kama:validate on the field of
// an object property, or on the type a schema declares:
//
//	minimum, maximum, exclusiveMinimum,
//	exclusiveMaximum                      cmp on integers and numbers
//	multipleOf                            multipleof
//	minLength, maxLength                  runes on strings
//	pattern                               regexp on strings
//	minItems, maxItems                    length on arrays
//	uniqueItems                           unique
//	minProperties, maxProperties          length on a map, members on a
//	                                      struct
//	dependentRequired                     requires on the field of the key,
//	                                      required on a member a required
//	                                      key depends on
//	enum, const                           const on scalars
//	additionalProperties false            closed on a struct
//	type                                  kind on raw JSON text
//
// On raw JSON text each keyword checks the values of its own kind and the
// validators compare as JSON does: cmp and multipleof take JSON numbers and
// compare exactly, runes and regexp check strings, length counts the
// elements of an array and members the members of an object, unique and
// const compare JSON values, so 1 equals 1.0 and objects equal regardless
// of member order. The keywords holding subschemas become validators whose
// operands are the validators of the subschemas, one operand per schema,
// and(...) of them when it has several:
//
//	required                              required=<names>
//	dependentRequired                     if(required=<key>, required=<names>)
//	properties, patternProperties,
//	additionalProperties                  props(field(<name>, ...),
//	                                      match(<pattern>, ...), rest(...))
//	items                                 items(...)
//	prefixItems                           prefix(<schema>, ..., rest(<items>))
//	contains, minContains, maxContains    contains(>=<min>, <=<max>, ...)
//	propertyNames                         keys(...)
//	dependentSchemas                      if(required=<key>, <schema>)
//	allOf, anyOf, oneOf                   and, or, oneof of the members
//	not                                   not(<schema>)
//	if, then, else                        if(<if>, <then>, <else>), and()
//	                                      for an absent then
//	$ref                                  ref=<type>, decode=<type> or the
//	                                      validators of the target
//	unevaluatedProperties                 unevalprops(<the others>, ...)
//	unevaluatedItems                      unevalitems(<the others>, ...)
//	true, false                           no validator, or()
//
// The keywords a Go type other than raw JSON text does not express check
// the value encoded as JSON: the type or the field carries json(...) of the
// validators of raw JSON text above, as the type of raw JSON text would.
// These keywords are not, if, then, else, prefixItems, contains,
// minContains, maxContains, patternProperties, propertyNames,
// dependentSchemas, unevaluatedProperties and unevaluatedItems, an allOf
// that makes no product, items next to prefixItems, additionalProperties
// next to patternProperties, a required member without a property, a
// dependentRequired of a map, of a product or naming a member without a
// property, the keywords of a product but those shaping it, and the
// keywords of a union but its oneOf or anyOf. An unevaluatedProperties or
// an unevaluatedItems needs what every keyword evaluates, so json(...) then
// checks every keyword of the schema, those the Go type checks too, and a
// failure of such a keyword is reported twice. A field applies json(...) to
// the value an opt.Option or an und.Und holds, so an absent member and a
// null of a nullable member are not checked. Validate encodes the value
// once per type or field carrying json(...), and a value nested in such a
// value is encoded again by its own Validate.
//
// A $ref names a type of raw JSON text by ref and a type whose decoder
// validates by decode. A union, which validates on no decoding, and a
// schema declaring no type are written out, and one reached again on a
// cycle is an error. Under unevaluatedProperties or unevaluatedItems a
// $ref applying to the same value is written out too, since ref and decode
// report nothing evaluated. Validators named twice on one place join one
// and. Each validator of the type or the field stands on a
// //kama:validate line of its own.
//
// A keyword applies to one kind of value only, as minLength to strings, and
// a value of another kind satisfies it. A typed value has one kind, so a
// keyword of another kind gives no validator. A struct and every union wrapper
// carry hook=off, since the generator writes no hook. A property takes the
// keywords of its own schema. A schema it refers to declares a type with
// its own. A number compares as the Go value, so an enum of numbers cannot
// tell apart two numbers one float64 holds. unique on a slice compares its
// elements as Go values, so the raw JSON elements 1 and 1.0 of a slice
// differ. requires, required and members count
// an und.Und holding null as present, as JSON Schema counts a member
// holding null. An optional nullable property on a cycle is a pointer,
// which is nil for null and absent alike, so it counts as absent.
// regexp is RE2, not ECMA-262.
//
// Generate warns with ErrUnenforced about a validation keyword the
// generated code does not check: a keyword of a nullable root, a pattern
// RE2 does not compile, and a keyword of a typed schema outside an object
// property that declares no type.
//
// Decoders call Validate through jsonschemart.Validate, since the
// generated file compiles before the validator generator runs. A struct
// validates once its members convert, a type of a validated value once it
// decodes, and a union validates each member it tries. A struct nested in
// another is validated by its own decoder and again by the Validate of the
// outer one, so decoding a value nested n levels deep validates the
// innermost value n times.
//
// format and the content keywords are annotations and give no directive.
// description and deprecated feed doc comments, keeping the lines of a
// description. Every line is printed as a line comment of its own, so a
// description cannot write a directive or end the comment. title names the
// root type and members of unions and allOf instead, and the other
// annotations are ignored.
//
// # Unsupported
//
// These are errors wrapping ErrUnsupported that name the JSON pointer of
// the schema or of the keyword:
//
//   - $dynamicRef, and a $schema other than the meta-schema of draft
//     2020-12, whose vocabularies the generator cannot read;
//   - a $ref naming no schema of the document, a chain of references back
//     to itself, and a $ref in the validators of raw JSON text
//     reaching a union or a schema declaring no type again on a cycle;
//   - a pattern of patternProperties RE2 does not compile;
//   - a nil schema of a document built in Go, and a slice or a map holding
//     itself with no declared type in between;
//   - a const or an enum value that is no Go constant of the scalar type
//     its type keyword gives;
//   - a numeric keyword that is no number, a multipleOf not above zero, and
//     a length or a count that is no non-negative integer;
//   - an enum of strings next to a type other than string;
//   - a oneOf of members the generated code cannot tell apart, as told
//     under Unions.
package jsonschema
