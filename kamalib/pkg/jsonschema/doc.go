// Package jsonschema models a JSON Schema draft 2020-12 document and resolves its same-document
// references.
//
// [Schema] decodes and encodes with encoding/json/v2. A decoded schema encodes back to JSON
// with the same members and the same number text. Numeric keywords and arbitrary values such
// as const, enum and default stay as [encoding/json/jsontext.Value]. [SchemaMap] keeps the
// members of properties, $defs, patternProperties and dependentSchemas in source order.
// Unknown keywords go to Schema.Unknown with their raw text.
//
// [ResolveRef] and [ResolvePointer] look a subschema up from the document root every time they
// are called. They resolve "#", JSON pointer fragments and plain-name fragments. A reference
// to another document fails with [ErrNonLocalRef], and a fragment that names no schema fails
// with [ErrUnresolved].
package jsonschema
