// Package jsonschema generates Go types from a JSON Schema draft 2020-12
// document.
//
// Generate writes one file, <package>.kama_jsonschema.go, holding a type for
// the root schema and for every schema under it that needs one. The types
// decode with encoding/json/v2 and check the keywords that decide what a
// value is: type, enum, const, required and additionalProperties. Errors
// are those of package github.com/ngicks/kama/kamalib/pkg/validation,
// located by the JSON path of the offending member. The generated code
// imports package github.com/ngicks/kama/kamalib/pkg/jsonschema/jsonschemart.
// Proc runs Generate as the workflow step kind jsonschema.generate.
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
//
// The schema file holds JSON. A schema param with a fragment, such as
// tests/minimum.json#/0/schema, takes the value the RFC 6901 pointer of the
// fragment points to as the schema document. The fragment is
// percent-decoded first, as the fragment of a $ref is. The value must be
// an object or a boolean, and it is the root that "#" refers to in a $ref.
// Proc reads it from the raw bytes of the file, so the order of its members
// and the text of its numbers survive. A spec generating the types of
// api/schema.json into api/api.kama_jsonschema.go of package api:
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
//	  final:
//	    proc: overlayfs.copy_down
//	    in: root
//	    after: generate
//
// The header of the generated file names the schema and type_name params
// and the time of the run. Proc keeps a file that differs from the
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
// strings, a union or an allOf of several members. Other schemas are
// spelled by built-in types where they are used:
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
// The type encodes and decodes as its Go type and checks the const or the
// enum of the root. A schema that refers to the root by $ref takes the Go
// type, not the name, so such a root holding itself is an error.
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
// member take the name of the schema holding them. A $ref takes the type of
// the schema it names; a reference declares nothing.
//
// Names are upper-camel-cased on the characters an identifier cannot hold,
// as foo_bar and foo-bar give FooBar. A name that gives no exported
// identifier is an error wrapping ErrUnsupported. Two names colliding in
// the package, in a struct or in its wire struct, or a field named like
// MarshalJSONTo or UnmarshalJSONFrom, is an error wrapping ErrConflict. A
// property named so that its field is Unknown or AdditionalProperties
// collides with the field the generator adds for additionalProperties.
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
// UnmarshalJSONFrom converts each wire field and reports every member the
// schema rejects. A const or an enum of other than strings is checked
// there, also inside a nullable value, whose null it lets through. Numbers
// compare by the values their JSON texts write, exactly. An enum of
// strings declares a string type with a constant per value, which rejects
// any other value itself.
//
// additionalProperties false makes decoding reject the members the object
// does not declare. A schema next to properties keeps them in the field
// AdditionalProperties, a map encoded inline. Absent or true ignores them
// on decoding and drops them.
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
// value in schema order. An anyOf and a type of several kinds take the
// first that succeeds and do not try later members. A oneOf needs exactly
// one to succeed. It stops at the second success and reports an error
// naming the two members, without trying later members. When no member
// succeeds, the error holds the error of every member tried. Marshaling
// writes the member the holder holds.
//
// The generated code does not check the keywords listed under Not
// enforced, so it cannot tell apart two members of a oneOf that differ
// only in them, or in annotations, nor a member holding only them, which
// every value matches. Such a oneOf is an error wrapping ErrUnsupported,
// since the generated code would reject values the schema accepts.
//
// # allOf
//
// An allOf of one member and no keyword other than type takes the type of
// the member. The type must then name the kinds the member admits. An
// allOf of more members declares a struct with a field per member, tagged
// "-", followed by the fields of its own properties.
// Decoding decodes each member and the own properties from the same
// object. Marshaling writes one object holding the members of each, the
// first to write a member name winning. A member must be an object. A
// member rejecting other members by additionalProperties false, a member
// holding the product itself, additionalProperties next to the allOf and
// a property two members declare differently are errors. Two declarations
// differing only in annotations, such as title, description, default or
// an extension keyword, are not different. A required member held by a nil
// pointer is an error on marshaling, as in a struct.
//
// # Not enforced
//
// minimum, maximum, exclusiveMinimum, exclusiveMaximum, multipleOf,
// minLength, maxLength, pattern, format, minItems, maxItems, uniqueItems,
// minProperties, maxProperties, dependentRequired and the content keywords
// are accepted and not checked. description and deprecated feed doc
// comments, keeping the lines of a description. Every line is printed as
// a line comment of its own, so a description cannot write a directive or
// end the comment. title names the root type and members of unions and
// allOf instead, and the other annotations are ignored.
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
//   - a const or an enum outside an object property and the root other
//     than an enum of strings, one mixing kinds, and a value that is no Go
//     constant of its type;
//   - a required member without a property;
//   - a type next to a oneOf or an anyOf that narrows its members or
//     admits null that no member admits, a type next to an allOf of one
//     member naming other kinds than the member, an enum of strings next
//     to a type other than string, and keywords of both an object and an
//     array;
//   - a oneOf of members the generated code cannot tell apart, as told
//     under Unions.
package jsonschema
