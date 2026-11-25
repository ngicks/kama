// Package jsonschema generates Go types from a JSON Schema draft 2020-12
// document.
//
// Generate yields one file, <package>.kama_jsonschema.go, holding a type for
// the root schema and for every schema under it that needs one. The types
// decode with encoding/json/v2. Decoding checks the keywords that decide
// the shape of a value, type and required. The validation keywords become
// //kama:validate directives on the types and their fields, and the
// validator generator of package
// github.com/ngicks/kama/kamalib/generator/validator writes the Validate
// methods checking them. The decoders call Validate where a value converts
// from its wire form to its type, as Validation describes. Errors are those
// of package github.com/ngicks/kama/kamaruntime/validator, located by the
// JSON path of the offending member, as Decoding describes. The generated
// code imports the standard library, github.com/ngicks/option and the
// packages of module github.com/ngicks/kama/kamaruntime only. Generator
// is the generator of Generate in the framework of package
// github.com/ngicks/kama/kamalib/generator/utils/base, and the proc runs it
// on the schema file of each package as the workflow step kind
// jsonschema.generate.
//
// # Proc
//
// The proc takes the packages a packages.load step loaded as its in and
// generates from the schema file of each package into the directory of the
// package, in the package of the package, staging the files in its out. A
// package names its schema file with the directive
// //kama:option-file jsonschema=<path> in a free-floating comment, the path
// being slash-separated and relative to the package directory. A package
// naming none has the schema file kama_jsonschema.json in its directory, or
// none. A package without a schema file generates nothing. A package has
// one schema file at most, since its declarations go to one file named
// after the package. Several packages may name one file. The proc reads
// the schema files from the overlay of its out, so a file staged by an
// earlier step is read too. A schema file holds JSON whatever its
// extension. The proc decodes it from its raw bytes, so the order of its
// members and the text of its numbers survive.
//
// The param of the proc applies to every package of the step. It has
// these keys:
//
//	type_name  the name of the root type, an exported Go identifier
//	           used as written; the title of the root schema when left
//	           out
//	validate   whether the decoders call Validate; true when left out
//	remotes    a list mapping the URIs of other documents to files of
//	           the out, as References describes; each entry has uri and
//	           either file, the file of that URI, or dir, the directory
//	           a URI starting with uri continues into; the paths are
//	           slash-separated and relative to the root of the out; a
//	           file may be followed by # and an RFC 6901 JSON pointer,
//	           percent-decoded as the fragment of a $ref is, naming the
//	           document inside the file
//	assert_formats
//	           whether format asserts the formats the validator
//	           generator checks; false when left out
//
// The packages load before the proc runs, and the generated decoders call
// the Validate methods the validator generator writes, so a spec loads the
// packages twice: once for the proc and once, after it, for
// validate.generate. A spec generating the types of api/schema.json into
// api/api.kama_jsonschema.go of the package of api/api.go, which holds
// //kama:option-file jsonschema=schema.json, then their Validate methods:
//
//	version: 1
//	steps:
//	  root:
//	    proc: overlayfs.open
//	  load:
//	    proc: packages.load
//	    in: root
//	    param:
//	      patterns: [./api]
//	      allow_type_errors: true
//	  generate:
//	    proc: jsonschema.generate
//	    in: load
//	    out: root
//	    param:
//	      type_name: Pet
//	  load_generated:
//	    proc: packages.load
//	    in: root
//	    after: generate
//	    param:
//	      patterns: [./api]
//	  validate:
//	    proc: validate.generate
//	    in: load_generated
//	    out: root
//	  final:
//	    proc: overlayfs.copy_down
//	    in: root
//	    after: validate
//
// The proc reads the name, the directory and the syntax trees of a
// package, not its types, so the first load sets allow_type_errors. Go
// files referring to the generated files before they exist, and a
// generated file of an earlier run that no longer compiles, such as one of
// an older version of the generator importing a package that is gone, then
// give warnings instead of failing the load. The second load keeps the
// default and fails on any error, since validate.generate reads the types.
//
// A spec running the proc with validate true needs the validate.generate step.
// Without it the decoders fail on the stubs of the Validate methods, as
// Validation describes. With validate false the directives are still
// written, the decoders call no Validate, and decoding checks no
// validation keyword. A union then takes the first member that decodes a
// value, even one holding a nested union that matches none of its members.
//
// The header of the generated file names the schema file, relative to the
// root of the out, the type_name and validate params and the time of the
// run. The helpers the declarations call go to
// zz_gen.helper.kama_jsonschema.go of the output directory, as Decoding
// describes. The proc keeps a file that differs from the existing one in
// the generated: line only. A Go file of the directory of a package the
// proc loaded that holds a declaration marked by the generator and that the
// run does not produce is stale, a package that dropped its schema file
// included. The proc reports a warning wrapping ErrStale of package
// github.com/ngicks/kama/kamalib/generator/utils/base for it and leaves it
// in place. Every file the generator marks in one directory must therefore
// come from one step.
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
// both members, and the oneOf stays a union. A type naming null and one
// other kind that makes raw JSON text is no nullable value: its
// jsontext.Value holds null itself, so that its validators check null too.
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
// identifier is an error wrapping ErrUnsupported. A field named like a
// method of the struct, MarshalJSONTo, UnmarshalJSONFrom, Validate or
// ValidateShallow, takes the suffix Field, as a property validate gives the
// field ValidateField. Two names colliding in the package, in a struct or
// in its wire struct, after that suffix, is an error wrapping ErrConflict.
// The fallback field of a struct, described under Objects, never collides:
// it takes the first of AdditionalProperties, AdditionalProperties_2,
// AdditionalProperties_3, ... that no field and no generated method takes,
// so a property additionalProperties keeps the name AdditionalProperties.
// A
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
// stays in the fallback field, which the validators of the value encoded
// as JSON check.
//
// The struct decodes through an unexported wire struct. It holds a
// property as an und.Und, which keeps a missing member apart from null, or,
// where the Go type of the property holds null itself, as the helper
// kamaJsonschemaPresent, which keeps a missing member apart from any value.
// UnmarshalJSONFrom converts each wire field and reports every member
// missing or null against the required x nullable matrix. Once every member
// converts, the struct must pass ValidateShallow. An enum of strings declares a
// string type with a constant per value, whose Validate rejects any other
// value.
//
// Every struct holds the members its properties do not name in its
// fallback field, an exported field tagged json:",embed" and named
// AdditionalProperties unless a property takes that name. Decoding never
// rejects such a member, and encoding writes them back. The fallback field
// is a map[string]T for an additionalProperties schema T, whose values
// decode as T, and a map[string]jsontext.Value otherwise.
// additionalProperties false makes Validate report each member of the
// fallback field, with an error wrapping validator.ErrClosed located at
// the member. Next to patternProperties, which takes some of those members
// from additionalProperties, the fallback field is a
// map[string]jsontext.Value and both keywords check the value encoded as
// JSON. A struct encodes by the default encoding of encoding/json/v2. A
// product, and a struct holding a required member by a pointer, declare
// MarshalJSONTo, described under allOf and Recursion.
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
// # Decoding
//
// A decoder decodes its value straight from the jsontext.Decoder it is
// given, so a document streams through the decoders: a struct decodes its
// wire struct from the decoder, and its members, the elements of a slice
// and the values of a map decode through their own decoders from the same
// decoder. A decoder reads a whole value first only where one value must
// be decoded more than once: a union, whose members are tried against the
// value, and a product of allOf, whose members each decode the whole
// object. Raw JSON text is the value itself. Such a value decodes from the
// bytes read, and each nested value in it decodes from those bytes again.
//
// Decoding rejects JSON null for a string, a number, a boolean, a slice
// and a map. Schema nullability is expressed by github.com/ngicks/option
// types and by jsontext.Value, so a null that reaches any other Go type is
// invalid, with an error wrapping validator.ErrNull. A number with a zero
// fraction, such as 1.0 or 1e2, decodes into an int64, since JSON Schema
// calls such a number an integer. A struct decoding a value that is no
// object fails with an error wrapping validator.ErrKind. The decoders pass
// the options doing this to every value they decode, nested values
// included.
//
// A decoder decodes with the options of the decoder it is given, which
// json.Unmarshal, json.UnmarshalDecode and json v1 fill from the options of
// their caller. The unmarshalers of the caller come before those rejecting
// null and decoding integers, so an unmarshaler of the caller decodes every
// value of its type, at any depth. Two options are pinned whatever the
// caller passes: member names match exactly, as JSON Schema compares them,
// and errors below the root are reported as json/v2 reports them, which
// locates them, even under the legacy semantics of json v1. A value a union
// or an allOf reads whole decodes again with the options of the caller, so
// AllowDuplicateNames and AllowInvalidUTF8 of the caller hold there as on
// the stream: a member name an object repeats fails the decoding unless the
// caller allows it.
//
// A decoder reads straight from the decoder of its caller. A member that
// fails to decode leaves that decoder inside the value, as any
// json.UnmarshalerFrom failing midway does, so the caller cannot read on
// from it.
//
// An error of a decoder is located relative to the value it decodes, by
// the path of the offending member in the lookups of the validator
// package. json.Unmarshal reports the error of the root in a
// json.SemanticError, whose Err holds it. A decoder below the root that
// fails while reading its value reports the error in a json.SemanticError
// naming the pointer of its value, so that the decoder of the enclosing
// value locates the error in turn. The errors of several members thus keep
// apart up to the root.
//
// The helpers the decoders call, such as kamaJsonschemaDecode, are
// unexported declarations of the package in zz_gen.helper.kama_jsonschema.go
// of the output directory. The file holds the helpers the declarations
// call and those these call, and no file is written when none is called.
// A declaration of a Go file of the output directory named like a helper,
// or one in that file, that the generator did not write is an error
// wrapping ErrConflict.
//
// # References
//
// A $ref resolves against the base URI of the schema holding it, which the
// $id of that schema and of the schemas above it give, as package
// github.com/ngicks/kama/kamalib/pkg/jsonschema resolves it. A root without
// $id has an empty base URI, against which the path of a relative
// reference is rooted: Options.Load and the remotes param see /pet.json for
// a $ref pet.json, which a remote with uri / and a dir covers. A schema declaring $id below the
// root is a
// schema resource of its own: a reference names it by its URI, a fragment
// pointer in a reference starts at the resource, and an $anchor is looked
// up in the resource holding the reference. A schema of an embedded
// resource is named as any other schema, by its place in the document,
// except that a schema under $defs inside a resource other than the root
// takes the name of the resource followed by its own, as CustomerName for
// $defs/Name of a resource named Customer. A resource at a keyword giving
// no name, such as then, prefixes nothing.
//
// A reference whose URI names no resource of the document names another
// document. Generate calls Options.Load with the URI without its fragment,
// and the proc loads the file its remotes param maps the URI to: the remote
// with file whose uri is the URI, or the remote with dir whose uri is the
// longest prefix of the URI, the rest of the URI taken as a path under dir.
// The generator never fetches a document from the network. A URI no remote
// covers is an error naming the reference and its JSON pointer:
//
//	remotes:
//	  - uri: https://example.com/schemas/
//	    dir: schemas
//
// Generate embeds each document it loads under $defs of the root, keyed by
// its retrieval URI, and loads the documents the embedded one refers to in
// turn, each once, so references across documents may form cycles. The
// embedded root keeps its $id, which resolves against the retrieval URI,
// and takes the retrieval URI as $id when it has none. A key of $defs of
// the root that is an absolute URI is taken for such a document. The types
// of the schemas of every document land in the one generated file. The
// root of a document takes the base name of the path of its retrieval URI
// without its extension, so https://example.com/schemas/money.json gives
// Money, and a schema under $defs inside it takes that name followed by
// its own, as MoneyCurrency. Two documents giving one name is an error
// wrapping ErrConflict naming both. The JSON pointers in comments, warnings
// and errors run from the root through the key of the document.
//
// # Meta-schemas
//
// A $schema of the root naming a meta-schema other than that of draft
// 2020-12 is loaded as another document is, through Options.Load. The
// meta-schema must declare $vocabulary. A vocabulary it requires must be
// one of draft 2020-12 other than format-assertion, which the generated
// code does not implement, and an optional vocabulary the generator does
// not know is ignored. A meta-schema leaving out the validation, the
// applicator or the unevaluated vocabulary turns their keywords off: the
// generator reads the document as if no schema held them.
//
// # Dynamic references
//
// A $dynamicRef resolves as a $ref does. When its fragment is a plain name
// and the schema it names declares a $dynamicAnchor of that name, it names
// instead the schema declaring that $dynamicAnchor in the outermost schema
// resource of the dynamic scope that declares one: the resources
// evaluation entered, from the root down. The generated validators carry
// no dynamic scope, so the generator resolves each $dynamicRef on every
// path the validators of a type follow, at generation time. A schema
// holding $dynamicRef is raw JSON text, and its validators are those of
// the schema it resolves to.
//
// The validators of a type resolve in the lexical scope of the type: the
// root and the resources holding the type in the document, so a type of a
// document decoded alone validates as if evaluation came to it from the
// root. A $ref or a $dynamicRef names the type of its target, or a $ref
// alone takes it, only where entering the target from the scope at hand
// resolves every $dynamicRef of the document as that lexical scope does.
// Otherwise the validators of the target are written out in place, and the
// $ref is raw JSON text. A reference written out that reaches its own
// schema again on such a path is an error wrapping ErrUnsupported. The
// classic extensible tree generates a type for each schema:
//
//	{
//	  "$id": "https://example.com/strict-tree",
//	  "title": "StrictTree",
//	  "$dynamicAnchor": "node",
//	  "$ref": "tree",
//	  "unevaluatedProperties": false,
//	  "$defs": {
//	    "Tree": {
//	      "$id": "tree",
//	      "$dynamicAnchor": "node",
//	      "type": "object",
//	      "properties": {
//	        "data": true,
//	        "children": {"type": "array", "items": {"$dynamicRef": "#node"}}
//	      }
//	    }
//	  }
//	}
//
// The children of StrictTree and those of Tree validate as StrictTree,
// since the root is the outermost resource declaring the $dynamicAnchor
// node in the scope of either.
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
// member the object does not declare. Before a member that reaches a
// reference decodes the value, an unexported probe of raw JSON text, such
// as petCatProbe, checks the keywords of the member that need no decoding
// of a nested value: those of the member itself that hold no subschema,
// and the properties whose schemas reach no reference. The remaining
// members decode the value in schema order, and a member that decodes it
// matches when its wrapper passes ValidateShallow. An anyOf and a type of
// several kinds take the first member that matches and do not try later
// members. A oneOf needs exactly one to match and stops at the second,
// without trying later members.
//
// Decoding a union never fails on the selection. A value no member matches,
// or that two members of a oneOf match, gives the wrapper PetUnknown,
// holding the value as jsontext.Value and the error of the selection.
// Validate of the holder returns that error, which holds the error of
// every member tried or names the two members matching, and so does its
// ValidateShallow. It wraps validator.ErrOneOf for a oneOf and
// validator.ErrOr otherwise, as the validators oneof and or report. A nil
// Kind is an error wrapping validator.ErrNil. The decoder of a struct holding the union reports it
// through its ValidateShallow. A union decoded alone reports it only when
// Validate is called. Marshaling writes the member the holder holds, the
// value of PetUnknown as it is.
//
// The holder has a Validate of its own, which calls the Validate of the
// wrapper it holds, and a ValidateShallow, which takes the wrapper as valid,
// since the decoder validated it when selecting it. The wrappers carry
// //kama:validate and get theirs from the validator generator. A wrapper
// encodes as its member. The other keywords of the union, the type next to
// a oneOf or an anyOf included, check the value of every member: each
// wrapper carries json(...) of them, as Validation describes, so decoding
// drops a member whose value fails them.
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
// object. The fallback field of the product holds the members that
// neither a member nor an own property declares. The product clears the
// fallback field of each member struct, which holds the members of the
// others, so a member value of an additionalProperties schema of a member
// is checked on decoding only. Marshaling writes one object holding the
// members of each, then those of its fallback field, the first to write a
// member name winning. A member holding the product itself is an error. Two members declaring one
// property
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
//	$ref, $dynamicRef                     ref=<type>, decode=<type> or the
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
// failure of such a keyword is reported twice. The properties are the
// exception: an entry of a property the Go type declares only records the
// member as evaluated, since the field checks its value and a nested value
// it decoded is valid. A field applies json(...) to the value an
// opt.Option or an und.Und holds, so an absent member and a null of a
// nullable member are not checked. Validate encodes the value once per
// type or field carrying json(...), and a value nested in such a value is
// encoded again by its own Validate.
//
// A $ref names a type of raw JSON text by ref and a type whose decoder
// validates by decode. A union, which validates on no decoding, and a
// schema declaring no type are written out, and one reached again on a
// cycle is an error. ref reports what its type evaluated through the
// ValidateEvaluated the validator generator writes for raw JSON text, so a
// type of raw JSON text keeps the entries that only record what they
// evaluate when the document holds unevaluatedProperties or
// unevaluatedItems. decode reports nothing evaluated, so under
// unevaluatedProperties or unevaluatedItems a $ref to a type decode names
// applying to the same value is written out, at each such $ref, and the
// output grows with the number of paths reaching it. Validators named
// twice on one place join one and. Each validator of the type or the field
// stands on a //kama:validate line of its own.
//
// A keyword applies to one kind of value only, as minLength to strings, and
// a value of another kind satisfies it. A typed value has one kind, so a
// keyword of another kind gives no validator. A struct and every union wrapper
// carry hook=off, since the generator writes no hook. A property takes the
// keywords of its own schema. A schema it refers to declares a type with
// its own. A number compares as the Go value, so an enum of numbers cannot
// tell apart two numbers one float64 holds. uniqueItems on a slice of Go
// scalars gives unique, which compares the elements as Go values. On a
// slice of other elements, as structs holding additional members or raw
// JSON text, it gives json(unique), which compares them as JSON values. A
// bound a typed value cannot reach folds: a maximum of 1e400 holds for
// every finite float64, a maxLength above every int for every string, and
// a multipleOf above every value of the type leaves 0 the only multiple.
// requires, required and members count
// an und.Und holding null as present, as JSON Schema counts a member
// holding null. An optional nullable property on a cycle is a pointer,
// which is nil for null and absent alike, so it counts as absent.
// regexp is RE2, not ECMA-262.
//
// Generate warns with ErrUnenforced about a validation keyword the
// generated code does not check: a keyword of a nullable root, a pattern
// RE2 does not compile, and a keyword of a typed schema outside an object
// property that declares no type, an asserted format included. Leaving such a keyword out only
// accepts
// more values where the schema holding it applies positively. Where a
// schema applies under not, as a member of a oneOf other than a nullable
// value, as the if of a then or an else, as the contains of a
// maxContains, or in place of a schema holding unevaluatedProperties or
// unevaluatedItems, whose annotations decide what those reject, a value it
// wrongly accepts may get rejected. Such a keyword there, and in every
// schema such a schema reaches through subschemas and references, is an
// error wrapping ErrUnsupported instead.
//
// Decoders call ValidateShallow on the types they decode, and a union
// calls Validate on its probes. Every type whose decoder validates carries
// //kama:validate decoded, which makes the validator generator write
// ValidateShallow for it, and ValidateShallow skips the nested values of
// such a type: their own decoders validated them. A struct validates once
// its members convert, a type of a validated value once it decodes, and a
// union validates the wrapper of each member it tries. Decoding a value
// nested n levels deep thus validates each level once, and the time grows
// with the size of the value. json(...) encodes the value of its level, so
// a type carrying it nested n levels deep costs time growing as the square
// of the depth.
//
// The methods do not exist until the validator generator runs, and
// packages.load, which it runs after, fails on a package calling them.
// With validate true, Generate therefore yields stubs of them: Validate for
// every type carrying //kama:validate and ValidateShallow for every type
// carrying //kama:validate decoded. They go to the file the validator
// generator writes for the generated file, <package>.kama_jsonschema.kama_validate.go,
// and carry its generated mark, //kama:generated kama:validate, so that it
// takes them for its own output and replaces them. A stub returns an error
// wrapping validator.ErrStub telling to run validate.generate over the
// package. A union whose members fail on a stub fails the decoding with
// that error rather than taking the value for its unknown member, so
// decoding never succeeds on a stub. Generate writes the stubs when the
// file does not exist and validate is true, or when the declarations of
// the run differ from those of the existing generated file, whose methods
// may refer to fields and types the run drops, whatever validate is. A run
// giving the declarations of the existing file leaves the methods the
// validator generator wrote alone. A method the package declares by hand,
// in a declaration without a generated mark, gets no stub, which would
// declare it twice.
//
// A union decodes the value once per member its prefilter and probe leave,
// and nested unions multiply that per level. Members telling a value apart
// only by something below a nested reference, as two members whose
// properties differ only inside a recursive child, or a value no member of
// any level matches, cost time growing as two to the power of the depth.
// The same holds for a schema applying two references to one value, as an
// allOf of two $ref to one definition, which validation runs both of.
//
// The content keywords are annotations and give no directive, and so is
// format unless Options.AssertFormats is set. It makes format=<name> of the
// validator generator check the formats date, date-time, ipv4, ipv6 and
// uuid, on a string or on raw JSON text, and leaves the other formats
// annotations. date-time parses with time.RFC3339, which rejects a leap
// second and a lower-case t or z and accepts an offset out of range. The
// format of an object property, of a type a schema declares and of a
// schema in the validators of raw JSON text is checked. A schema that
// declares no type for its other keywords, such as a string under items
// or under $defs, leaves its format unchecked, since format gives it no
// type of its own. Generate warns about it with ErrUnenforced.
//
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
//   - a $schema of the root naming a meta-schema that Options.Load does
//     not give, that declares no $vocabulary or that requires a
//     vocabulary other than those of draft 2020-12, and a $schema below
//     the root other than the meta-schema of draft 2020-12;
//   - a $ref naming no schema of the documents, a chain of references back
//     to itself, a schema applying itself again to the same value through
//     allOf, anyOf, oneOf, not, if, then, else, dependentSchemas and
//     references with no member or element in between, which would
//     validate without end, and a $ref or a $dynamicRef in the validators of raw JSON
//     text reaching a union, a schema declaring no type or a type the
//     dynamic scope resolves otherwise again on a cycle;
//   - a pattern of patternProperties RE2 does not compile;
//   - a nil schema of a document built in Go, and a slice or a map holding
//     itself with no declared type in between;
//   - a const or an enum value that is no Go constant of the scalar type
//     its type keyword gives;
//   - a numeric keyword that is no number, a multipleOf not above zero or
//     below 1 and too small for a Go constant to hold exactly, and a
//     length or a count that is no non-negative integer;
//   - a keyword left out where leaving it out may reject a value, as told
//     under Validation;
//   - an enum of strings next to a type other than string;
//   - a oneOf of members the generated code cannot tell apart, as told
//     under Unions.
package jsonschema
