package validator

import (
	"go/ast"
	"go/types"
	"slices"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/validator/rule"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// callable reports whether generated code can call Validate on a value of
// named: named is a target of the run or an implementor.
func (s *selection) callable(named *types.Named) bool {
	return s.callableIn(s.trim(), named)
}

// callableIn is callable with the targets of trimmed. trimmed is the graph
// trim returns.
func (s *selection) callableIn(trimmed *typegraph.MatchedGraph, named *types.Named) bool {
	return s.targetIn(trimmed, originId(named)) || s.implementor(named)
}

// implementor reports whether named has a Validate written by hand that
// generated code can call: a method declared on the value receiver of
// named itself, not promoted from an embedded field, with the shape
// validateShape describes.
//
// A method carrying the marker of this generator in a loaded package does
// not count. It is dropped and generated again, and a type the run no
// longer targets must not keep validating through a stale file. A method
// another generator wrote counts as one written by hand.
// An interface type does not count either: its nil value would panic.
func (s *selection) implementor(named *types.Named) bool {
	origin := named.Origin()
	if types.IsInterface(origin) {
		return false
	}

	fn := declaredValidate(origin, origin)

	return fn != nil && validateShape(fn) && !s.generated(fn)
}

// shallowName is the name of the shallow form of Validate.
const shallowName = "ValidateShallow"

// evaluatedName is the name of the form of Validate recording annotations.
const evaluatedName = "ValidateEvaluated"

// hasEvaluated reports whether the target named gets ValidateEvaluated: it
// is raw JSON text, whose validators may evaluate members and elements, and
// not generic, so that the ref validator calls it alike on every target.
func hasEvaluated(named *types.Named) bool {
	return rule.IsRawJson(named) && named.TypeParams().Len() == 0
}

// decoded reports whether //kama:validate decoded, or its option file
// counterpart, tells that decoding a value of named validates it.
func (s *selection) decoded(named *types.Named) bool {
	to, _ := s.loadedOptions.Option.Type(originId(named))
	return to.Type.Decoded || to.Group.Decoded
}

// hasShallow reports whether named, which callable accepts, has a
// ValidateShallow: an implementor declaring ValidateShallow of the shape of
// its Validate, a target whose ValidateShallow differs from its Validate,
// as shallowDiffers tells, or a target marked decoded. The decoder of a
// type marked decoded calls the method on the type itself, so such a
// target gets one even where it equals its Validate.
func (s *selection) hasShallow(named *types.Named) bool {
	if s.implementor(named) {
		return s.shallowImplementor(named)
	}

	return s.decoded(named) || s.shallowDiffers(named)
}

// shallowDiffers reports whether the target named reaches a value its
// ValidateShallow treats otherwise than its Validate does. That is a value
// of a type marked decoded or of another type with a ValidateShallow that
// differs, directly or through the walk of another target. Any other
// target gets no ValidateShallow, or one calling its Validate when it is
// marked decoded.
func (s *selection) shallowDiffers(named *types.Named) bool {
	// seen holds the targets the search entered, so that a cycle of types
	// and types reached on several paths cost one walk each.
	seen := []*types.Named{named}

	var search func(t *types.Named) bool

	search = func(t *types.Named) bool {
		return s.reaches(t.Underlying(), func(n *types.Named) bool {
			switch {
			case !s.callable(n):
				return false
			case s.decoded(n):
				return true
			case s.implementor(n):
				return s.shallowImplementor(n)
			case onPath(seen, n):
				return false
			}

			seen = append(seen, n)

			return search(n)
		})
	}

	return search(named)
}

// shallowImplementor reports whether the implementor named declares
// ValidateShallow on its value receiver with the shape of its Validate.
func (s *selection) shallowImplementor(named *types.Named) bool {
	origin := named.Origin()
	fn := declaredMethod(origin, origin, shallowName)

	return fn != nil && validateShape(fn) && !s.generated(fn)
}

// pointerImplementor reports whether named has a Validate of the shape of
// an implementor on its pointer receiver only. Generated code holds values,
// which such a method cannot be called on in general, so it is reported as
// unsupported.
func (s *selection) pointerImplementor(named *types.Named) bool {
	origin := named.Origin()
	if types.IsInterface(origin) || declaredValidate(origin, origin) != nil {
		return false
	}

	fn := declaredValidate(types.NewPointer(origin), origin)

	return fn != nil && validateShape(fn) && !s.generated(fn)
}

// declaredValidate returns the method Validate of the method set of t, which
// is named or a pointer to it, when named declares it itself.
func declaredValidate(t types.Type, named *types.Named) *types.Func {
	return declaredMethod(t, named, "Validate")
}

// declaredMethod returns the method name of the method set of t, which is
// named or a pointer to it, when named declares it itself.
func declaredMethod(t types.Type, named *types.Named, name string) *types.Func {
	sel := types.NewMethodSet(t).Lookup(named.Obj().Pkg(), name)
	if sel == nil || len(sel.Index()) != 1 {
		return nil
	}

	return sel.Obj().(*types.Func)
}

// validateShape reports whether fn has the signature
//
//	func(bool, func(T1, bool) error, ..., func(Tn, bool) error) error
//
// with one function parameter per type parameter T1, ..., Tn of its
// receiver, in order.
func validateShape(fn *types.Func) bool {
	sig := fn.Type().(*types.Signature)
	// The type parameters of a method are those of its receiver, which are
	// distinct objects from the type parameters of the type declaration.
	tparams := sig.RecvTypeParams()

	if sig.Variadic() || sig.Params().Len() != tparams.Len()+1 || !returnsError(sig) ||
		!types.Identical(sig.Params().At(0).Type(), types.Typ[types.Bool]) {
		return false
	}

	for i := range tparams.Len() {
		if !isValidateFunc(sig.Params().At(i+1).Type(), tparams.At(i)) {
			return false
		}
	}

	return true
}

// isValidateFunc reports whether t is func(elem, bool) error.
func isValidateFunc(t types.Type, elem types.Type) bool {
	sig, ok := t.Underlying().(*types.Signature)
	if !ok || sig.Variadic() || sig.TypeParams().Len() > 0 || sig.Params().Len() != 2 {
		return false
	}

	return types.Identical(sig.Params().At(0).Type(), elem) &&
		types.Identical(sig.Params().At(1).Type(), types.Typ[types.Bool]) &&
		returnsError(sig)
}

// returnsError reports whether sig has the single result error.
func returnsError(sig *types.Signature) bool {
	return sig.Results().Len() == 1 &&
		types.Identical(sig.Results().At(0).Type(), types.Universe.Lookup("error").Type())
}

// generated reports whether the declaration of obj, declared in a loaded
// package, carries the marker of this generator.
func (s *selection) generated(obj types.Object) bool {
	pkg := base.DeclaringPackage(s.pkgs, obj)
	if pkg == nil {
		return false
	}

	return slices.ContainsFunc(base.DeclDocs(pkg, obj), func(doc *ast.CommentGroup) bool {
		name, ok := s.markers.Generated(doc)
		return ok && name == GeneratorId().String()
	})
}
