// Package spell holds what the ast printer of typeprint and the dst
// printer of dsttype share: the spelling of names and the checks of the
// types no type expression spells.
package spell

import (
	"errors"
	"fmt"
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/pkg/importdecl"
)

var (
	// ErrUnknownPackage reports a type whose package the importdecl.Map
	// does not know.
	ErrUnknownPackage = errors.New("typeprint: unknown package")
	// ErrUnexported reports an unexported type, struct field or interface
	// method declared in a package other than the current one.
	ErrUnexported = errors.New("typeprint: unexported name of another package")
	// ErrUnsupported reports a type no type expression spells, such as an
	// untyped constant type, a tuple, a generic signature, a generic type
	// without type arguments or a type declared inside a function.
	ErrUnsupported = errors.New("typeprint: unsupported type")
)

// Printer holds what spelling a type takes: the imports of the file and
// the path of the package the expression goes into.
type Printer struct {
	M              *importdecl.Map
	CurrentPkgPath string
}

// TypeName returns the qualifier and the name of the type obj declares.
// The qualifier is empty for predeclared types and types of the current
// package.
func (p Printer) TypeName(obj *types.TypeName) (qual, name string, err error) {
	pkg := obj.Pkg()
	if pkg == nil {
		return "", obj.Name(), nil
	}

	if parent := obj.Parent(); parent != nil && parent != pkg.Scope() {
		return "", "", fmt.Errorf(
			"%w: %s is declared inside a function",
			ErrUnsupported,
			obj.Name(),
		)
	}

	if pkg.Path() == p.CurrentPkgPath {
		return "", obj.Name(), nil
	}

	if !obj.Exported() {
		return "", "", fmt.Errorf("%w: %s.%s", ErrUnexported, pkg.Path(), obj.Name())
	}

	qual, err = p.Ident(pkg.Path())
	if err != nil {
		return "", "", err
	}

	return qual, obj.Name(), nil
}

// CheckInstantiated rejects a generic type spelled without type arguments,
// which is no type expression.
func CheckInstantiated(
	obj *types.TypeName,
	tparams *types.TypeParamList,
	targs *types.TypeList,
) error {
	if tparams.Len() > 0 && targs.Len() == 0 {
		return fmt.Errorf("%w: generic type %s without type arguments", ErrUnsupported, obj.Name())
	}

	return nil
}

// CheckMemberName rejects the unexported name of a struct field or an
// interface method declared in a package other than the current one.
// Spelled in the current package, the name would belong to it and denote a
// different field or method.
//
// The check covers embedded fields too. An embedded field is named after
// its type, and typeName already rejects an unexported named type of
// another package, but an embedded predeclared type such as int gives an
// unexported field name only this check catches.
func (p Printer) CheckMemberName(obj types.Object) error {
	pkg := obj.Pkg()
	if obj.Exported() || pkg == nil || pkg.Path() == p.CurrentPkgPath {
		return nil
	}

	return fmt.Errorf("%w: %s.%s", ErrUnexported, pkg.Path(), obj.Name())
}

// BasicName returns the qualifier and the name of b.
func (p Printer) BasicName(b *types.Basic) (qual, name string, err error) {
	switch {
	case b.Kind() == types.Invalid:
		return "", "", fmt.Errorf("%w: invalid type", ErrUnsupported)
	case b.Info()&types.IsUntyped != 0:
		return "", "", fmt.Errorf("%w: %s", ErrUnsupported, b.Name())
	case b.Kind() == types.UnsafePointer:
		qual, err := p.Ident("unsafe")
		if err != nil {
			return "", "", err
		}

		return qual, b.Name(), nil
	default:
		// Name keeps byte and rune, which are aliases of uint8 and int32.
		return "", b.Name(), nil
	}
}

// Ident returns the identifier the file refers to the package at pkgPath
// by.
func (p Printer) Ident(pkgPath string) (string, error) {
	ident, ok := p.M.Ident(pkgPath)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownPackage, pkgPath)
	}

	return ident, nil
}

// CheckSignature rejects a generic signature, which no type expression
// spells.
func CheckSignature(sig *types.Signature) error {
	if sig.TypeParams().Len() > 0 {
		return fmt.Errorf("%w: generic signature %s", ErrUnsupported, sig)
	}

	return nil
}

// VariadicElem returns the element type of the variadic last parameter of
// sig.
func VariadicElem(sig *types.Signature) (types.Type, error) {
	last := sig.Params().At(sig.Params().Len() - 1).Type()

	s, ok := last.(*types.Slice)
	if !ok {
		// Only the predeclared append takes a string as its variadic
		// parameter.
		return nil, fmt.Errorf("%w: variadic parameter of type %s", ErrUnsupported, last)
	}

	return s.Elem(), nil
}

// ArrayLen returns the length of a as a literal.
func ArrayLen(a *types.Array) (string, error) {
	// A negative length means the length is unknown.
	if a.Len() < 0 {
		return "", fmt.Errorf("%w: array of unknown length", ErrUnsupported)
	}

	return strconv.FormatInt(a.Len(), 10), nil
}

// NeedsParen reports whether the element of a bidirectional channel must
// be parenthesized: chan <-chan T parses as chan<- (chan T).
func NeedsParen(c *types.Chan) bool {
	if c.Dir() != types.SendRecv {
		return false
	}

	elem, ok := c.Elem().(*types.Chan)

	return ok && elem.Dir() == types.RecvOnly
}

// ChanDir reports whether dir sends and receives.
func ChanDir(dir types.ChanDir) (send, recv bool) {
	switch dir {
	case types.SendOnly:
		return true, false
	case types.RecvOnly:
		return false, true
	default:
		return true, true
	}
}
