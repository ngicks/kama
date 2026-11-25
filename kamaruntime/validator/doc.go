// Package validator records where a validation error happened and lets callers query errors by
// JSON pointer.
//
// Validators create leaf errors with [New]. Callers that descend into a struct field, a slice
// element or a map value wrap the errors they get back with [Field], [Index] or [Key]. [Join]
// aggregates several errors into one. [At], [Under] and [Pointers] look the errors up by their
// RFC 6901 JSON pointer.
//
//	err := validator.Join(
//		validator.Field(validator.New(validator.ErrRequired, "value is missing"), "name"),
//		validator.Field(
//			validator.Index(validator.New(validator.ErrLength, "want <=3, got %d", 5), 2),
//			"tags",
//		),
//	)
//
//	err.Error()
//	// validation failed at .name: required: value is missing
//	// validation failed at .tags[2]: length: want <=3, got 5
//	validator.Pointers(err)            // ["/name", "/tags/2"]
//	validator.At(err, "/tags/2")       // the length error
//	validator.Under(err, "/tags")      // the length error
//	errors.Is(err, validator.ErrRequired) // true
//
// # Paths
//
// A path lists segments from the outermost value to the failing one. [Field] adds a field name,
// [Index] adds a slice or array index and [Key] adds a map key formatted with %v. Index(err, 5)
// and Key(err, 5) therefore produce the same text and the same pointer.
//
// The wrapping functions never change their input. They return a new value with the segment
// prepended to every leaf inside err. They distribute over a value made by [Join]. They treat
// any other error as one leaf. An error that wraps a path error of this package is such a leaf.
//
// # Composed errors
//
// [Compose] creates a leaf error whose failure other errors explain, as the error of an or
// combinator carries the error of each alternative. Its message lists the causes indented
// below its own. errors.Is reports its kind only, and the lookups locate the leaf alone.
// [Causes] returns the causes.
//
// # Locating errors
//
// The lookups split a value made by [Join] into its leaves and locate each leaf on its own.
// The location of a leaf is the concatenation of the paths of every [*Error] on its Unwrap()
// error chain, outermost first. The chain stops at an error with an Unwrap() []error method.
// A join behind a foreign wrapper, as in fmt.Errorf("ctx: %w", validator.Join(a, b)), is
// therefore one leaf. A value of the standard errors.Join is one leaf as well. Aggregate with
// [Join] to keep every leaf addressable.
//
// A plain error that carries no path is located at the root pointer "". [Error.Pointer]
// computes the same location as the lookups. A leaf of At(err, p) that is an [*Error] reports p
// from its Pointer method.
//
// The lookups return each leaf as the whole leaf value in input order. A foreign wrapper around
// a leaf stays on the returned value. [Pointers] sorts lexically. "/a/10" comes before "/a/2"
// in its result. A pointer that is neither empty nor starting with "/" matches nothing.
//
// # Dependencies
//
// Generated code imports this package. Its module has no dependency outside the standard
// library, tests included, so a consumer's module graph gains no other module. For that
// reason the package uses plain loops where the generator github.com/ngicks/kama/kamalib uses
// github.com/ngicks/opiter chains.
package validator
