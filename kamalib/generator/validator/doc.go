// Package validator generates Validate methods for marked struct, slice and
// map types and for the types holding them.
//
// A target T gets
//
//	func (v T) Validate(stopOnFirstError bool) error
//
// and a generic target one more parameter per type parameter P:
//
//	validateP func(v P, stopOnFirstError bool) error
//
// Validate runs the validators named by //kama:validate:<name> directives on
// the fields, validates the values the fields hold through their own
// Validate, and calls the hook validate<Field> of each field. The errors are
// those of package github.com/ngicks/kama/kamalib/pkg/validation, located by
// the json names of the fields.
package validator
