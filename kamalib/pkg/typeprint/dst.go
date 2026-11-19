package typeprint

import (
	"fmt"
	"go/token"
	"go/types"

	"github.com/dave/dst"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/structtag"
	"github.com/ngicks/opiter"
)

// Dst returns the dst type expression spelling ty. It qualifies types the
// same way Ast does. Qualified names are plain selector expressions, so
// the expression prints the same with or without an import resolver.
func Dst(ty types.Type, m *importdecl.Map, currentPkgPath string) (dst.Expr, error) {
	return dstPrinter{printer{m: m, currentPkgPath: currentPkgPath}}.expr(ty)
}

type dstPrinter struct {
	printer
}

func (p dstPrinter) expr(ty types.Type) (dst.Expr, error) {
	switch ty := ty.(type) {
	case *types.Basic:
		qual, name, err := p.basicName(ty)
		if err != nil {
			return nil, err
		}

		return dstName(qual, name), nil
	case *types.Alias:
		return p.named(ty.Obj(), ty.TypeParams(), ty.TypeArgs())
	case *types.Named:
		return p.named(ty.Obj(), ty.TypeParams(), ty.TypeArgs())
	case *types.TypeParam:
		return dst.NewIdent(ty.Obj().Name()), nil
	case *types.Pointer:
		elem, err := p.expr(ty.Elem())
		if err != nil {
			return nil, err
		}

		return &dst.StarExpr{X: elem}, nil
	case *types.Slice:
		elem, err := p.expr(ty.Elem())
		if err != nil {
			return nil, err
		}

		return &dst.ArrayType{Elt: elem}, nil
	case *types.Array:
		n, err := arrayLen(ty)
		if err != nil {
			return nil, err
		}

		elem, err := p.expr(ty.Elem())
		if err != nil {
			return nil, err
		}

		return &dst.ArrayType{Len: &dst.BasicLit{Kind: token.INT, Value: n}, Elt: elem}, nil
	case *types.Map:
		key, err := p.expr(ty.Key())
		if err != nil {
			return nil, err
		}

		value, err := p.expr(ty.Elem())
		if err != nil {
			return nil, err
		}

		return &dst.MapType{Key: key, Value: value}, nil
	case *types.Chan:
		return p.chanType(ty)
	case *types.Signature:
		return p.signature(ty)
	case *types.Struct:
		return p.structType(ty)
	case *types.Interface:
		return p.interfaceType(ty)
	case *types.Union:
		return p.union(ty)
	default:
		return nil, fmt.Errorf("%w: %T %s", ErrUnsupported, ty, ty)
	}
}

func dstName(qual, name string) dst.Expr {
	if qual == "" {
		return dst.NewIdent(name)
	}

	return &dst.SelectorExpr{X: dst.NewIdent(qual), Sel: dst.NewIdent(name)}
}

func (p dstPrinter) named(
	obj *types.TypeName,
	tparams *types.TypeParamList,
	targs *types.TypeList,
) (dst.Expr, error) {
	if err := checkInstantiated(obj, tparams, targs); err != nil {
		return nil, err
	}

	qual, name, err := p.typeName(obj)
	if err != nil {
		return nil, err
	}

	x := dstName(qual, name)

	indices, err := opiter.TryCollect(
		opiter.WrapSeq(targs.Types()).
			Disassemble(p.expr),
	)
	if err != nil {
		return nil, err
	}

	switch len(indices) {
	case 0:
		return x, nil
	case 1:
		return &dst.IndexExpr{X: x, Index: indices[0]}, nil
	default:
		return &dst.IndexListExpr{X: x, Indices: indices}, nil
	}
}

func (p dstPrinter) chanType(c *types.Chan) (dst.Expr, error) {
	elem, err := p.expr(c.Elem())
	if err != nil {
		return nil, err
	}

	if needsParen(c) {
		elem = &dst.ParenExpr{X: elem}
	}

	var dir dst.ChanDir

	send, recv := chanDir(c.Dir())
	if send {
		dir |= dst.SEND
	}

	if recv {
		dir |= dst.RECV
	}

	return &dst.ChanType{Dir: dir, Value: elem}, nil
}

func (p dstPrinter) signature(sig *types.Signature) (*dst.FuncType, error) {
	if err := checkSignature(sig); err != nil {
		return nil, err
	}

	params, err := p.tuple(sig.Params())
	if err != nil {
		return nil, err
	}

	params.Opening, params.Closing = true, true

	if sig.Variadic() {
		elem, err := variadicElem(sig)
		if err != nil {
			return nil, err
		}

		elemExpr, err := p.expr(elem)
		if err != nil {
			return nil, err
		}

		params.List[len(params.List)-1].Type = &dst.Ellipsis{Elt: elemExpr}
	}

	results, err := p.tuple(sig.Results())
	if err != nil {
		return nil, err
	}

	switch {
	case len(results.List) == 0:
		results = nil
	// A single unnamed result goes without parentheses, as the decorator
	// gives for parsed source.
	case len(results.List) > 1 || results.List[0].Names != nil:
		results.Opening, results.Closing = true, true
	}

	return &dst.FuncType{Func: true, Params: params, Results: results}, nil
}

func (p dstPrinter) tuple(t *types.Tuple) (*dst.FieldList, error) {
	fields, err := opiter.TryCollect(
		opiter.WrapSeq(t.Variables()).
			Disassemble(p.tupleField),
	)
	if err != nil {
		return nil, err
	}

	return &dst.FieldList{List: fields}, nil
}

func (p dstPrinter) tupleField(v *types.Var) (*dst.Field, error) {
	ty, err := p.expr(v.Type())
	if err != nil {
		return nil, err
	}

	f := &dst.Field{Type: ty}
	if v.Name() != "" {
		f.Names = []*dst.Ident{dst.NewIdent(v.Name())}
	}

	return f, nil
}

func (p dstPrinter) structType(s *types.Struct) (*dst.StructType, error) {
	fields := &dst.FieldList{Opening: true, Closing: true}

	for i := range s.NumFields() {
		v := s.Field(i)
		if err := p.checkMemberName(v); err != nil {
			return nil, err
		}

		ty, err := p.expr(v.Type())
		if err != nil {
			return nil, err
		}

		f := &dst.Field{Type: ty}
		if !v.Embedded() {
			f.Names = []*dst.Ident{dst.NewIdent(v.Name())}
		}

		if tag := s.Tag(i); tag != "" {
			f.Tag = &dst.BasicLit{Kind: token.STRING, Value: structtag.Literal(tag)}
		}

		fields.List = append(fields.List, f)
	}

	return &dst.StructType{Fields: fields}, nil
}

func (p dstPrinter) interfaceType(iface *types.Interface) (*dst.InterfaceType, error) {
	methods := &dst.FieldList{Opening: true, Closing: true}

	for embedded := range iface.EmbeddedTypes() {
		ty, err := p.expr(embedded)
		if err != nil {
			return nil, err
		}

		methods.List = append(methods.List, &dst.Field{Type: ty})
	}

	for fn := range iface.ExplicitMethods() {
		if err := p.checkMemberName(fn); err != nil {
			return nil, err
		}

		sig, err := p.signature(fn.Signature())
		if err != nil {
			return nil, err
		}
		// Methods are spelled without the func keyword.
		sig.Func = false
		methods.List = append(methods.List, &dst.Field{
			Names: []*dst.Ident{dst.NewIdent(fn.Name())},
			Type:  sig,
		})
	}

	return &dst.InterfaceType{Methods: methods}, nil
}

func (p dstPrinter) union(u *types.Union) (dst.Expr, error) {
	var x dst.Expr

	for term := range u.Terms() {
		ty, err := p.expr(term.Type())
		if err != nil {
			return nil, err
		}

		if term.Tilde() {
			ty = &dst.UnaryExpr{Op: token.TILDE, X: ty}
		}

		if x == nil {
			x = ty
		} else {
			x = &dst.BinaryExpr{X: x, Op: token.OR, Y: ty}
		}
	}

	if x == nil {
		return nil, fmt.Errorf("%w: empty union", ErrUnsupported)
	}

	return x, nil
}
