package validator

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/packages"

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
// A method carrying the marker of a registered generator in a loaded
// package does not count. It is dropped and generated again, and a type
// the run no longer targets must not keep validating through a stale file.
// An interface type does not count either: its nil value would panic.
func (s *selection) implementor(named *types.Named) bool {
	origin := named.Origin()
	if types.IsInterface(origin) {
		return false
	}

	fn := declaredValidate(origin, origin)

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
	sel := types.NewMethodSet(t).Lookup(named.Obj().Pkg(), "Validate")
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
// package, carries the marker of a registered generator.
func (s *selection) generated(obj types.Object) bool {
	if obj.Pkg() == nil {
		return false
	}

	for _, pkg := range s.pkgs {
		if pkg.Types != obj.Pkg() {
			continue
		}

		return slices.ContainsFunc(declDocs(pkg, obj), func(doc *ast.CommentGroup) bool {
			_, ok := s.opts.Markers.Generated(doc)
			return ok
		})
	}

	return false
}

// declDocs returns the doc comments of the declaration of obj, a type, a
// function, a method, a variable or a constant declared in pkg at the top
// level: the doc of the declaration and the doc of its spec. It returns nil
// for an object pkg does not declare at the top level.
func declDocs(pkg *packages.Package, obj types.Object) []*ast.CommentGroup {
	pos := obj.Pos()

	for _, f := range pkg.Syntax {
		if pos < f.FileStart || pos >= f.FileEnd {
			continue
		}

		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.Pos() == pos {
					return []*ast.CommentGroup{d.Doc}
				}
			case *ast.GenDecl:
				if docs := genDeclDocs(d, pos); docs != nil {
					return docs
				}
			}
		}
	}

	return nil
}

// genDeclDocs returns the doc comments of the spec of d declaring the name
// at pos, or nil.
func genDeclDocs(d *ast.GenDecl, pos token.Pos) []*ast.CommentGroup {
	for _, spec := range d.Specs {
		switch sp := spec.(type) {
		case *ast.TypeSpec:
			if sp.Name.Pos() == pos {
				return []*ast.CommentGroup{d.Doc, sp.Doc}
			}
		case *ast.ValueSpec:
			declares := func(name *ast.Ident) bool { return name.Pos() == pos }
			if slices.ContainsFunc(sp.Names, declares) {
				return []*ast.CommentGroup{d.Doc, sp.Doc}
			}
		}
	}

	return nil
}
