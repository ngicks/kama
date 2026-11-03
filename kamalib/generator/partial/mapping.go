package partial

import (
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/structtag"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typeprint"
)

const (
	optPath     = "github.com/ngicks/option/opt"
	undPath     = "github.com/ngicks/option/und"
	elasticPath = "github.com/ngicks/option/elastic"
)

// mapper builds the partial type expressions of a file.
type mapper struct {
	sel *selection
	pkg *packages.Package
	m   *importdecl.Map
	// usedUnd reports whether an expression refers to und.
	usedUnd bool
}

func (p *mapper) position(pos token.Pos) token.Position { return p.pkg.Fset.Position(pos) }

func (p *mapper) print(t types.Type) (dst.Expr, error) {
	return typeprint.Dst(t, p.m, p.pkg.PkgPath)
}

func (p *mapper) typeParams(n *typegraph.Node, list *types.TypeParamList) (*dst.FieldList, error) {
	if list.Len() == 0 {
		return nil, nil
	}
	fields := &dst.FieldList{Opening: true, Closing: true}
	for tp := range list.TypeParams() {
		c := tp.Constraint()
		// A constraint written as ~int | string is an implicit interface;
		// printing it as such would spell interface{ ~int | string }.
		if iface, ok := c.(*types.Interface); ok && iface.IsImplicit() &&
			iface.NumEmbeddeds() == 1 {
			c = iface.EmbeddedType(0)
		}
		expr, err := p.print(c)
		if err != nil {
			return nil, fmt.Errorf(
				"%s (%s): type parameter %s: %w",
				nodeName(n), p.position(tp.Obj().Pos()), tp.Obj().Name(), err,
			)
		}
		fields.List = append(fields.List, &dst.Field{
			Names: []*dst.Ident{dst.NewIdent(tp.Obj().Name())},
			Type:  expr,
		})
	}
	return fields, nil
}

func (p *mapper) structType(n *typegraph.Node, s *types.Struct) (dst.Expr, error) {
	fields := &dst.FieldList{Opening: true, Closing: true}
	for i := range s.NumFields() {
		v := s.Field(i)
		f, err := p.field(v, s.Tag(i))
		if err != nil {
			return nil, fmt.Errorf("field %s (%s): %w", v.Name(), p.position(v.Pos()), err)
		}
		fields.List = append(fields.List, f)
	}
	return &dst.StructType{Fields: fields}, nil
}

// field maps a struct field of a target.
//
// An embedded target, or pointer to one, stays embedded as its partial
// variant and keeps its tag: adding omitzero to it would stop encoders from
// inlining it. Any other embedded type becomes a field named after it,
// since its partial value is an und.Und, which cannot be embedded by the
// field name of the source.
func (p *mapper) field(v *types.Var, tag string) (*dst.Field, error) {
	if v.Embedded() {
		if named, ok := p.sel.target(deref(v.Type())); ok {
			ty, err := p.partial(named)
			if err != nil {
				return nil, err
			}
			return withTag(&dst.Field{Type: ty}, tag), nil
		}
	}

	ty, err := p.value(v.Type())
	if err != nil {
		return nil, err
	}
	tag, err = partialTag(tag, v.Exported())
	if err != nil {
		return nil, err
	}
	return withTag(&dst.Field{Names: []*dst.Ident{dst.NewIdent(v.Name())}, Type: ty}, tag), nil
}

func withTag(f *dst.Field, tag string) *dst.Field {
	if tag != "" {
		lit := strconv.Quote(tag)
		if strconv.CanBackquote(tag) {
			lit = "`" + tag + "`"
		}
		f.Tag = &dst.BasicLit{Kind: token.STRING, Value: lit}
	}
	return f
}

// value maps the type of a struct field, a slice element or a map value:
//
//	T              und.Und[T]
//	M              und.Und[MPartial]  (M a target)
//	*T, *M         as T and M
//	opt.Option[T]  und.Und[T]
//	und.Und[T]     und.Und[T]
//	elastic.Elastic[T] elastic.Elastic[T]
//
// Any other type, a slice, map or array included, maps as a whole as T
// does.
func (p *mapper) value(t types.Type) (dst.Expr, error) {
	if ptr, ok := types.Unalias(t).(*types.Pointer); ok {
		if _, ok := types.Unalias(ptr.Elem()).(*types.Pointer); ok {
			return nil, fmt.Errorf("%w: pointer to pointer %s", ErrUnsupported, t)
		}
		return p.wrap(ptr.Elem())
	}

	switch {
	case isNamed(t, optPath, "Option"):
		elem, err := p.print(types.Unalias(t).(*types.Named).TypeArgs().At(0))
		if err != nil {
			return nil, err
		}
		return p.und(elem)
	case isNamed(t, undPath, "Und"):
		p.usedUnd = true
		return p.print(t)
	case isNamed(t, elasticPath, "Elastic"):
		return p.print(t)
	}
	return p.wrap(t)
}

// wrap returns und.Und of t, or of the partial variant of t when it is a
// target.
func (p *mapper) wrap(t types.Type) (dst.Expr, error) {
	var (
		inner dst.Expr
		err   error
	)
	if named, ok := p.sel.target(t); ok {
		inner, err = p.partial(named)
	} else {
		inner, err = p.print(t)
	}
	if err != nil {
		return nil, err
	}
	return p.und(inner)
}

func (p *mapper) und(inner dst.Expr) (dst.Expr, error) {
	sel := p.m.DstExpr(importdecl.QualifiedType{PackagePath: undPath, TypeName: "Und"})
	if sel == nil {
		return nil, fmt.Errorf("partial: unknown package %q", undPath)
	}
	p.usedUnd = true
	return &dst.IndexExpr{X: sel, Index: inner}, nil
}

// partial returns the partial variant of the target named, instantiated
// with the type arguments of named.
func (p *mapper) partial(named *types.Named) (dst.Expr, error) {
	obj := named.Obj()
	var x dst.Expr = dst.NewIdent(partialName(obj.Name()))
	if pkgPath := obj.Pkg().Path(); pkgPath != p.pkg.PkgPath {
		ident, ok := p.m.Ident(pkgPath)
		if !ok {
			return nil, fmt.Errorf("partial: unknown package %q", pkgPath)
		}
		x = &dst.SelectorExpr{X: dst.NewIdent(ident), Sel: dst.NewIdent(partialName(obj.Name()))}
	}

	args := make([]dst.Expr, 0, named.TypeArgs().Len())
	for t := range named.TypeArgs().Types() {
		arg, err := p.print(t)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	switch len(args) {
	case 0:
		return x, nil
	case 1:
		return &dst.IndexExpr{X: x, Index: args[0]}, nil
	default:
		return &dst.IndexListExpr{X: x, Indices: args}, nil
	}
}

func isNamed(t types.Type, pkgPath, name string) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == pkgPath && obj.Name() == name
}

// partialTag returns the tag of a partial field derived from the tag of a
// source field.
//
// An undefined und.Und is zero, so omitzero leaves it out. encoding/json/v2
// also omits a null value under omitempty, which would drop an explicit
// null, so omitempty is removed from json. go.yaml.in/yaml/v4 rejects
// omitzero and omits an undefined und.Und under omitempty. go-toml/v2
// omits it under omitzero. yaml and toml are only edited when the source
// field has them, so a field without them keeps the encoder's defaults.
// testdata/marshalers records these behaviors.
//
// json is added to a field without it only when the field is exported:
// encoding/json/v2 rejects a json tag on an unexported field.
func partialTag(tag string, exported bool) (string, error) {
	tags, err := structtag.Parse(reflect.StructTag(tag))
	if err != nil {
		return "", err
	}
	for _, r := range []struct {
		key, add, drop string
		always         bool
	}{
		{"json", "omitzero", "omitempty", exported},
		{"yaml", "omitempty", "omitzero", false},
		{"toml", "omitzero", "omitempty", false},
	} {
		tags, err = setOption(tags, r.key, r.add, r.drop, r.always)
		if err != nil {
			return "", err
		}
	}
	return string(tags.StructTag()), nil
}

// setOption adds the option add to the value of key and removes drop. A
// missing key is added only when always is set.
func setOption(tags structtag.Tags, key, add, drop string, always bool) (structtag.Tags, error) {
	i := slices.IndexFunc(tags, func(t structtag.Tag) bool { return t.Key == key })
	if i < 0 && !always {
		return tags, nil
	}
	if i >= 0 {
		// "-" skips the field, and an option after it would make "-" its
		// name.
		if tags[i].Value == "-" {
			return tags, nil
		}
		// structtag rejects the empty option a trailing comma leaves, as in
		// json:"-," naming a field "-".
		if v, ok := strings.CutSuffix(tags[i].Value, ","); ok {
			tags = slices.Clone(tags)
			if v == "-" {
				tags[i].Value = "-," + add
				return tags, nil
			}
			tags[i].Value = v
		}
		_, _, err := tags.Get(key, drop)
		switch {
		case err == nil:
			if tags, err = tags.Delete(key, drop); err != nil {
				return nil, err
			}
		case !errors.Is(err, structtag.ErrNotFound):
			return nil, err
		}
	}
	return tags.Add(key, add, "")
}
