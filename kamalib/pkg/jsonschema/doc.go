// Package jsonschema models a JSON Schema draft 2020-12 document and resolves the references
// among the schema resources of a document.
//
// [Schema] decodes and encodes with encoding/json/v2. A decoded schema encodes back to JSON
// with the same members and the same number text. Numeric keywords and arbitrary values such
// as const, enum and default stay as [encoding/json/jsontext.Value]. [SchemaMap] keeps the
// members of properties, $defs, patternProperties and dependentSchemas in source order.
// Unknown keywords go to Schema.Unknown with their raw text.
//
// A document holds schema resources: the root, and each subschema declaring $id. The base URI
// of a schema joins each $id from the root down to it, as RFC 3986 resolves a reference, the
// first one against the retrieval URI of the document, which the caller gives and which may be
// empty. A keyword holding no schema, such as enum or an unknown keyword, holds no resource,
// whatever $id its value spells. The functions below take the document root and its retrieval
// URI, and they walk the document again on every call.
//
// [BaseUri] returns the base URI of the schema at a JSON pointer. [Resources] yields the URI and
// the pointer of every resource. [RefUri] resolves a $ref written at a pointer against the base
// URI there. [Locate] returns the pointer of the schema a URI names: the URI without its
// fragment names a resource, and the fragment is a JSON pointer from the resource or a plain
// name an $anchor or a $dynamicAnchor of the resource declares. An anchor belongs to the
// resource that holds it, not to the resources around that one. [ResolveRef] combines RefUri
// and Locate. [ResolvePointer] returns the schema at a pointer from the root. [ResourcesAlong]
// returns the resources holding a schema, from the root down, and [DynamicAnchor] the schema of
// a resource declaring a $dynamicAnchor, which a caller resolving $dynamicRef in a dynamic scope
// asks for each resource of the scope. An empty retrieval URI leaves a relative URI relative. A
// URI naming no
// resource of the document fails with [ErrNonLocalRef], and a fragment that names no schema
// fails with [ErrUnresolved].
package jsonschema
