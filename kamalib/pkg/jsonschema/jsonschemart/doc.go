// Package jsonschemart is the runtime of the Go code generated from JSON Schema documents.
//
// Generated code decodes every value from its raw text. An UnmarshalJSONFrom method reads one
// value with [ReadValue], decodes it into its wire struct with [UnmarshalObject] or into a member
// type with [Unmarshal], and converts the wire fields with [Required], [Optional],
// [RequiredNullable], [RequiredPointer], [OptionalPointer], [RequiredNullablePointer] and
// [OptionalNullablePointer]. A member whose Go type accepts null itself, such as a union holder
// with a null member, stays raw in the wire struct and decodes through [DecodeRequired] or
// [DecodeOptional]. Errors are those of github.com/ngicks/kama/kamalib/pkg/validation, located
// by the path of the offending member relative to the value being decoded.
//
// json.Unmarshal wraps the errors of a generated type in a json.SemanticError, so the errors of
// several members collapse into one at the root. [Unmarshal] is the entry point that keeps the
// path of every member addressable through the lookups of the validation package.
//
// [Unmarshal] rejects JSON null for a string, a number, a boolean, a slice and a map. Schema
// nullability is expressed by github.com/ngicks/option types and by [encoding/json/jsontext.Value],
// so a null that reaches any other Go type is invalid.
// It decodes a number with a zero fraction, such as 1.0 or 1e2, into an int64, since JSON Schema
// calls such a number an integer.
//
// Union holders select their member with [IsKind], [HasMembers], [OnlyMembers] and
// [MemberEquals], then decode the value with each remaining member and report a failure with
// [NoMatch] or [MultipleMatch]. Product structs of allOf marshal through [MarshalObjects].
//
// # Dependencies
//
// Generated code imports this package, so it depends on the standard library, the validation
// package and github.com/ngicks/option only. For that reason it uses plain loops where the rest of
// the module uses github.com/ngicks/opiter chains.
package jsonschemart
