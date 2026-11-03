package typeprint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/ngicks/kama/kamalib/pkg/importdecl"
)

// Ast returns the go/ast type expression spelling ty. Types declared in
// the package at currentPkgPath are unqualified. Types of any other
// package are qualified through m, which records the packages the file
// does not import yet as missing.
//
// The nodes carry no positions, so go/printer lays out every struct and
// interface over several lines, as in struct {\n}. Dst gives the layout
// gofmt gives.
func Ast(ty types.Type, m *importdecl.Map, currentPkgPath string) (ast.Expr, error) {
	return astPrinter{printer{m: m, currentPkgPath: currentPkgPath}}.expr(ty)
}

type astPrinter struct {
	printer
}

func (p astPrinter) expr(ty types.Type) (ast.Expr, error) {
	switch ty := ty.(type) {
	case *types.Basic:
		qual, name, err := p.basicName(ty)
		if err != nil {
			return nil, err
		}
		return astName(qual, name), nil
	case *types.Alias:
		return p.named(ty.Obj(), ty.TypeParams(), ty.TypeArgs())
	case *types.Named:
		return p.named(ty.Obj(), ty.TypeParams(), ty.TypeArgs())
	case *types.TypeParam:
		return ast.NewIdent(ty.Obj().Name()), nil
	case *types.Pointer:
		elem, err := p.expr(ty.Elem())
		if err != nil {
			return nil, err
		}
		return &ast.StarExpr{X: elem}, nil
	case *types.Slice:
		elem, err := p.expr(ty.Elem())
		if err != nil {
			return nil, err
		}
		return &ast.ArrayType{Elt: elem}, nil
	case *types.Array:
		n, err := arrayLen(ty)
		if err != nil {
			return nil, err
		}
		elem, err := p.expr(ty.Elem())
		if err != nil {
			return nil, err
		}
		return &ast.ArrayType{Len: &ast.BasicLit{Kind: token.INT, Value: n}, Elt: elem}, nil
	case *types.Map:
		key, err := p.expr(ty.Key())
		if err != nil {
			return nil, err
		}
		value, err := p.expr(ty.Elem())
		if err != nil {
			return nil, err
		}
		return &ast.MapType{Key: key, Value: value}, nil
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

func astName(qual, name string) ast.Expr {
	if qual == "" {
		return ast.NewIdent(name)
	}
	return &ast.SelectorExpr{X: ast.NewIdent(qual), Sel: ast.NewIdent(name)}
}

func (p astPrinter) named(
	obj *types.TypeName,
	tparams *types.TypeParamList,
	targs *types.TypeList,
) (ast.Expr, error) {
	if err := checkInstantiated(obj, tparams, targs); err != nil {
		return nil, err
	}
	qual, name, err := p.typeName(obj)
	if err != nil {
		return nil, err
	}
	x := astName(qual, name)

	indices := make([]ast.Expr, 0, targs.Len())
	for t := range targs.Types() {
		index, err := p.expr(t)
		if err != nil {
			return nil, err
		}
		indices = append(indices, index)
	}

	switch len(indices) {
	case 0:
		return x, nil
	case 1:
		return &ast.IndexExpr{X: x, Index: indices[0]}, nil
	default:
		return &ast.IndexListExpr{X: x, Indices: indices}, nil
	}
}

func (p astPrinter) chanType(c *types.Chan) (ast.Expr, error) {
	elem, err := p.expr(c.Elem())
	if err != nil {
		return nil, err
	}
	if needsParen(c) {
		elem = &ast.ParenExpr{X: elem}
	}

	var dir ast.ChanDir
	send, recv := chanDir(c.Dir())
	if send {
		dir |= ast.SEND
	}
	if recv {
		dir |= ast.RECV
	}
	return &ast.ChanType{Dir: dir, Value: elem}, nil
}

func (p astPrinter) signature(sig *types.Signature) (*ast.FuncType, error) {
	if err := checkSignature(sig); err != nil {
		return nil, err
	}

	params, err := p.tuple(sig.Params())
	if err != nil {
		return nil, err
	}
	if sig.Variadic() {
		elem, err := variadicElem(sig)
		if err != nil {
			return nil, err
		}
		elemExpr, err := p.expr(elem)
		if err != nil {
			return nil, err
		}
		params.List[len(params.List)-1].Type = &ast.Ellipsis{Elt: elemExpr}
	}

	results, err := p.tuple(sig.Results())
	if err != nil {
		return nil, err
	}
	if len(results.List) == 0 {
		results = nil
	}

	return &ast.FuncType{Params: params, Results: results}, nil
}

func (p astPrinter) tuple(t *types.Tuple) (*ast.FieldList, error) {
	list := &ast.FieldList{}
	for v := range t.Variables() {
		ty, err := p.expr(v.Type())
		if err != nil {
			return nil, err
		}
		f := &ast.Field{Type: ty}
		if v.Name() != "" {
			f.Names = []*ast.Ident{ast.NewIdent(v.Name())}
		}
		list.List = append(list.List, f)
	}
	return list, nil
}

func (p astPrinter) structType(s *types.Struct) (*ast.StructType, error) {
	fields := &ast.FieldList{}
	for i := range s.NumFields() {
		v := s.Field(i)
		if err := p.checkMemberName(v); err != nil {
			return nil, err
		}
		ty, err := p.expr(v.Type())
		if err != nil {
			return nil, err
		}
		f := &ast.Field{Type: ty}
		if !v.Embedded() {
			f.Names = []*ast.Ident{ast.NewIdent(v.Name())}
		}
		if tag := s.Tag(i); tag != "" {
			f.Tag = &ast.BasicLit{Kind: token.STRING, Value: tagLit(tag)}
		}
		fields.List = append(fields.List, f)
	}
	return &ast.StructType{Fields: fields}, nil
}

func (p astPrinter) interfaceType(iface *types.Interface) (*ast.InterfaceType, error) {
	methods := &ast.FieldList{}
	for embedded := range iface.EmbeddedTypes() {
		ty, err := p.expr(embedded)
		if err != nil {
			return nil, err
		}
		methods.List = append(methods.List, &ast.Field{Type: ty})
	}
	for fn := range iface.ExplicitMethods() {
		if err := p.checkMemberName(fn); err != nil {
			return nil, err
		}
		sig, err := p.signature(fn.Signature())
		if err != nil {
			return nil, err
		}
		methods.List = append(methods.List, &ast.Field{
			Names: []*ast.Ident{ast.NewIdent(fn.Name())},
			Type:  sig,
		})
	}
	return &ast.InterfaceType{Methods: methods}, nil
}

func (p astPrinter) union(u *types.Union) (ast.Expr, error) {
	var x ast.Expr
	for term := range u.Terms() {
		ty, err := p.expr(term.Type())
		if err != nil {
			return nil, err
		}
		if term.Tilde() {
			ty = &ast.UnaryExpr{Op: token.TILDE, X: ty}
		}
		if x == nil {
			x = ty
		} else {
			x = &ast.BinaryExpr{X: x, Op: token.OR, Y: ty}
		}
	}
	if x == nil {
		return nil, fmt.Errorf("%w: empty union", ErrUnsupported)
	}
	return x, nil
}
