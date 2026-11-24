// Package typeprint renders go/types types as go/ast type expressions.
// Package dsttype renders them as dst type expressions.
//
// A type declared in another package is qualified with the identifier an
// importdecl.Map gives for the package. Asking the Map records the package
// as missing when the file does not import it yet. A type declared in the
// package the expression is generated into is left unqualified; the caller
// names that package by its path.
package typeprint

import "github.com/ngicks/kama/kamalib/pkg/typeprint/internal/spell"

var (
	// ErrUnknownPackage reports a type whose package the importdecl.Map
	// does not know.
	ErrUnknownPackage = spell.ErrUnknownPackage
	// ErrUnexported reports an unexported type, struct field or interface
	// method declared in a package other than the current one.
	ErrUnexported = spell.ErrUnexported
	// ErrUnsupported reports a type no type expression spells, such as an
	// untyped constant type, a tuple, a generic signature, a generic type
	// without type arguments or a type declared inside a function.
	ErrUnsupported = spell.ErrUnsupported
)
