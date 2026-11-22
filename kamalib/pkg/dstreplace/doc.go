// Package dstreplace derives struct type declarations from source struct
// literals by replacing parts of a copy of their syntax.
//
// A generator that maps each field of a source struct to a field of a new
// struct can build the new struct from the types alone, which loses how
// the source is laid out. This package instead copies the decorated syntax
// of the source and lets the caller replace each field type. The copy
// keeps the field order, the blank lines between fields, the names that
// share one field, as in A, B int, and the tag literals as written.
//
// TypeSpec decorates a source file and finds the decorated node of a
// type spec in it. Struct copies a decorated struct literal, asks a
// Replacer about every field name and returns the new struct type. Fields
// lists the names Struct asks about, in the same order.
//
// Struct erases every comment of the copy. The comments of a source
// declaration describe the source and its directives, not the derived
// type. Replaced field types are used as given. No part of a source type
// expression is kept, so the result refers to no import of the source
// file.
//
// A name the Replacer drops leaves out the field when the field has no
// other name. The names of one field that get one type and one tag stay
// in one field. Otherwise each name gets a field of its own. The first of
// them takes the line break before the source field and the last one the
// line break after it. A field list does not start or end with a blank
// line.
//
// A Replacement gives the tag of the new field unquoted. The source tag
// literal stays as written when the tag equals the source tag, so raw and
// interpreted spellings survive. A changed tag is written as a raw string
// literal when it can be one, and as an interpreted one otherwise.
package dstreplace
