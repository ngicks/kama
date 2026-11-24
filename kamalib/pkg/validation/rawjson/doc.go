// Package rawjson validates raw JSON text, jsontext.Value or a type of the
// same underlying type, with the semantics of the validation keywords of
// JSON Schema. The code the validators of package
// github.com/ngicks/kama/kamalib/generator/validator/rule emit for raw JSON
// calls it.
//
// A function testing one kind of value passes a value of another kind, as
// a JSON Schema keyword ignores a value it does not apply to: Compare
// passes a string and Runes a number. Numbers compare by value, exactly, so
// 1 and 1.0 are equal and 0.1 is a multiple of 0.01. A number whose
// decimal exponent exceeds 10000 compares as a float of 4096 bits instead.
// The errors are those of package
// github.com/ngicks/kama/kamalib/pkg/validation.
package rawjson
