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
//	           to the root of the out; required
//	dir        the output directory, relative to the root of the out;
//	           the directory of schema when left out
//	package    the package clause; the base name of dir when left out
//	type_name  the name of the root type; the title of the root schema
//	           when left out
//
// The schema file holds JSON. A spec generating the types of
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
// # Shapes
//
// A schema declares a Go type of its own when it is an object, an enum of
// strings, a union or an allOf of several members. The root schema must be
// one of them. Other schemas are spelled by built-in types where they are
// used:
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
// enum the kind of their values.
//
// A nullable value is a type naming null and one other kind, or a oneOf or
// an anyOf of exactly a schema and a schema of type null. It is no union.
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
// The struct decodes through an unexported wire struct holding every
// property as an und.Und, which keeps a missing member apart from null.
// UnmarshalJSONFrom converts each wire field and reports every member the
// schema rejects. A const or an enum of other than strings is checked
// there. An enum of strings declares a string type with a constant per
// value, which rejects any other value itself.
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
// member the object does not declare. Every remaining member decodes the
// value. A oneOf needs exactly one to succeed. An anyOf and a type of
// several kinds take the first in schema order and do not keep later
// matches. Marshaling writes the member the holder holds.
//
// # allOf
//
// An allOf of one member and no keyword other than type takes the type of
// the member. An allOf of more members declares a struct with a field per
// member, tagged "-", followed by the fields of its own properties.
// Decoding decodes each member and the own properties from the same
// object. Marshaling writes one object holding the members of each, the
// first to write a member name winning. A member must be an object. A
// member rejecting other members by additionalProperties false, a member
// holding the product itself, additionalProperties next to the allOf and
// a property two members declare differently are errors.
//
// # Not enforced
//
// minimum, maximum, exclusiveMinimum, exclusiveMaximum, multipleOf,
// minLength, maxLength, pattern, format, minItems, maxItems, uniqueItems,
// minProperties, maxProperties, dependentRequired and the content keywords
// are accepted and not checked. title, description and deprecated feed
// doc comments, and the other annotations are ignored.
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
//   - the false schema as a value, a schema admitting null only outside a
//     union, and a slice or a map holding itself with no declared type in
//     between;
//   - a const or an enum outside an object property other than an enum of
//     strings, one mixing kinds, and a value that is no Go constant of its
//     type;
//   - a required member without a property;
//   - a type next to a oneOf or an anyOf that narrows its members, and
//     keywords of both an object and an array.
package jsonschema
