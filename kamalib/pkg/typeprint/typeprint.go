// Package typeprint renders go/types types as go/ast and dst type
// expressions.
//
// A type declared in another package is qualified with the identifier an
// importdecl.Map gives for the package. Asking the Map records the package
// as missing when the file does not import it yet. A type declared in the
// package the expression is generated into is left unqualified; the caller
// names that package by its path.
package typeprint

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

type printer struct {
	m              *importdecl.Map
	currentPkgPath string
}

// typeName returns the qualifier and the name of the type obj declares.
// The qualifier is empty for predeclared types and types of the current
// package.
func (p printer) typeName(obj *types.TypeName) (qual, name string, err error) {
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
	if pkg.Path() == p.currentPkgPath {
		return "", obj.Name(), nil
	}
	if !obj.Exported() {
		return "", "", fmt.Errorf("%w: %s.%s", ErrUnexported, pkg.Path(), obj.Name())
	}
	qual, err = p.ident(pkg.Path())
	if err != nil {
		return "", "", err
	}
	return qual, obj.Name(), nil
}

// checkInstantiated rejects a generic type spelled without type arguments,
// which is no type expression.
func checkInstantiated(
	obj *types.TypeName,
	tparams *types.TypeParamList,
	targs *types.TypeList,
) error {
	if tparams.Len() > 0 && targs.Len() == 0 {
		return fmt.Errorf("%w: generic type %s without type arguments", ErrUnsupported, obj.Name())
	}
	return nil
}

// checkMemberName rejects the unexported name of a struct field or an
// interface method declared in a package other than the current one.
// Spelled in the current package, the name would belong to it and denote a
// different field or method.
//
// The check covers embedded fields too. An embedded field is named after
// its type, and typeName already rejects an unexported named type of
// another package, but an embedded predeclared type such as int gives an
// unexported field name only this check catches.
func (p printer) checkMemberName(obj types.Object) error {
	pkg := obj.Pkg()
	if obj.Exported() || pkg == nil || pkg.Path() == p.currentPkgPath {
		return nil
	}
	return fmt.Errorf("%w: %s.%s", ErrUnexported, pkg.Path(), obj.Name())
}

// basicName returns the qualifier and the name of b.
func (p printer) basicName(b *types.Basic) (qual, name string, err error) {
	switch {
	case b.Kind() == types.Invalid:
		return "", "", fmt.Errorf("%w: invalid type", ErrUnsupported)
	case b.Info()&types.IsUntyped != 0:
		return "", "", fmt.Errorf("%w: %s", ErrUnsupported, b.Name())
	case b.Kind() == types.UnsafePointer:
		qual, err := p.ident("unsafe")
		if err != nil {
			return "", "", err
		}
		return qual, b.Name(), nil
	default:
		// Name keeps byte and rune, which are aliases of uint8 and int32.
		return "", b.Name(), nil
	}
}

func (p printer) ident(pkgPath string) (string, error) {
	ident, ok := p.m.Ident(pkgPath)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownPackage, pkgPath)
	}
	return ident, nil
}

func checkSignature(sig *types.Signature) error {
	if sig.TypeParams().Len() > 0 {
		return fmt.Errorf("%w: generic signature %s", ErrUnsupported, sig)
	}
	return nil
}

// variadicElem returns the element type of the variadic last parameter of
// sig.
func variadicElem(sig *types.Signature) (types.Type, error) {
	last := sig.Params().At(sig.Params().Len() - 1).Type()
	s, ok := last.(*types.Slice)
	if !ok {
		// Only the predeclared append takes a string as its variadic
		// parameter.
		return nil, fmt.Errorf("%w: variadic parameter of type %s", ErrUnsupported, last)
	}
	return s.Elem(), nil
}

func arrayLen(a *types.Array) (string, error) {
	// A negative length means the length is unknown.
	if a.Len() < 0 {
		return "", fmt.Errorf("%w: array of unknown length", ErrUnsupported)
	}
	return strconv.FormatInt(a.Len(), 10), nil
}

// needsParen reports whether the element of a bidirectional channel must
// be parenthesized: chan <-chan T parses as chan<- (chan T).
func needsParen(c *types.Chan) bool {
	if c.Dir() != types.SendRecv {
		return false
	}
	elem, ok := c.Elem().(*types.Chan)
	return ok && elem.Dir() == types.RecvOnly
}

func tagLit(tag string) string {
	if strconv.CanBackquote(tag) {
		return "`" + tag + "`"
	}
	return strconv.Quote(tag)
}

func chanDir(dir types.ChanDir) (send, recv bool) {
	switch dir {
	case types.SendOnly:
		return true, false
	case types.RecvOnly:
		return false, true
	default:
		return true, true
	}
}
