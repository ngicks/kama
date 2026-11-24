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
// strings, a union or an allOf of several members. A schema outside an
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
//	array without items          []jsontext.Value
//	object with only
//	  additionalProperties T     map[string]T
//	no type and no applicator    jsontext.Value
//
// A schema without type takes the kind its keywords imply: properties,
// required and additionalProperties an object, items an array, const and
// enum the kind of their values. An integer decodes from any number with a
// zero fraction, such as 1.0 or 1e2.
//
// A root that declares no type of its own still needs a name to decode a
// document into. It declares a type defined over its Go type, such as
// type Root []int64, or type Root opt.Option[string] for a nullable root.
// The type encodes and decodes as its Go type. A schema that refers to the
// root by $ref takes the Go type, not the name, so such a root holding
// itself is an error.
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
// The json tag holds the property name as it is. A name holding a comma,
// a quote, a backslash or a control character is an error. For property p
// of type T:
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
// validation.ErrClosed located at the member. encoding/json/v2 refuses a
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
// # Unions
//
// A oneOf, an anyOf and a type naming two or more kinds other than null
// declare a holder, an interface and a wrapper per member:
//
//	type Pet struct{ Kind PetKind }
//	type PetKind interface{ isPetKind() }
//	type PetCat struct{ Cat Cat }
//	type PetDog struct{ Dog Dog }
//
// A member is named by the schema its $ref names, else by its title, else
// by its kind, such as String or Object, when no other member without a
// $ref or a title has that kind, else Option followed by its 1-based
// index. A member of type null is a wrapper without a field. The kinds of
// a type of several kinds must be scalars and null.
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
// the validator generator.
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
// An allOf of one member and no keyword other than type takes the type of
// the member. The type must then name the kinds the member admits. An
// allOf of more members declares a struct with a field per member, tagged
// "-", followed by the fields of its own properties.
// Decoding decodes each member and the own properties from the same
// object. The _x of the product holds the members that neither a member
// nor an own property declares. The product clears the _x of each member
// struct, which holds the members of the others, so a member value of an
// additionalProperties schema of a member is checked on decoding only.
// Marshaling writes one object holding the members of each, then those of
// _x, the first to write a member name winning. A member must be an object. A
// member rejecting other members by additionalProperties false, a member
// holding the product itself, additionalProperties next to the allOf and
// a property two members declare differently are errors. Two declarations
// differing only in annotations, such as title, description, default or
// an extension keyword, are not different. A required member held by a nil
// pointer is an error on marshaling, as in a struct.
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
//
// A keyword applies to one kind of value only, as minLength to strings, and
// a value of another kind satisfies it. A struct and every union wrapper
// carry hook=off, since the generator writes no hook. A property takes the
// keywords of its own schema. A schema it refers to declares a type with
// its own. A number compares as the Go value, so an enum of numbers cannot
// tell apart two numbers one float64 holds. unique compares raw JSON text
// byte by byte, so 1 and 1.0 differ. requires, required and members count
// an und.Und holding null as absent, while JSON Schema counts a member
// holding null as present. regexp is RE2, not ECMA-262.
//
// Generate warns with ErrUnenforced about a validation keyword the
// generated code does not check: a keyword of a schema of no single kind,
// whose Go type is jsontext.Value, of a union or of a nullable root, a
// pattern RE2 does not compile, a dependentRequired naming a member
// without a property, an object keyword next to a $ref to a struct, and a
// keyword of a schema outside an object property that declares no type,
// such as one next to a $ref.
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
//   - if, then, else, not, prefixItems, patternProperties,
//     dependentSchemas, unevaluatedItems, unevaluatedProperties, contains,
//     propertyNames and $dynamicRef;
//   - a $ref leaving the document, a $ref next to a keyword that changes
//     the type, a chain of references back to itself, and $id below the
//     root;
//   - a nil schema of a document built in Go, the false schema as a
//     value, a schema admitting null only outside a union, and a slice or
//     a map holding itself with no declared type in between;
//   - a const or an enum mixing kinds, and a value that is no Go constant
//     of its type;
//   - a numeric keyword that is no number, a multipleOf not above zero, and
//     a length or a count that is no non-negative integer;
//   - a required member without a property;
//   - a type next to a oneOf or an anyOf that narrows its members or
//     admits null that no member admits, a type next to an allOf of one
//     member naming other kinds than the member, an enum of strings next
//     to a type other than string, and keywords of both an object and an
//     array;
//   - a oneOf of members the generated code cannot tell apart, as told
//     under Unions.
package jsonschema
