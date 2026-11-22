package partial

import (
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"

	"github.com/dave/dst"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/gofragment"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/structtag"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/opiter"
)

const (
	optPath     = "github.com/ngicks/option/opt"
	undPath     = "github.com/ngicks/option/und"
	elasticPath = "github.com/ngicks/option/elastic"
)

// mapper builds the partial declarations of a file. Its FileScope spells
// the packages and the types of the file.
type mapper struct {
	sel *selection
	*base.FileScope
	// helpers are the helpers of the package, shared by its files.
	helpers *helperSet
	// calls are the helpers the file calls.
	calls map[*helper]bool
	// support are the paths of supportPaths the file spells.
	support map[string]bool
	// refs are the identifiers of partial types and helpers of the package
	// a declaration refers to unqualified.
	refs map[string]bool
}

// ident returns the identifier the file refers to the package at path by.
func (p *mapper) ident(path string) (string, error) {
	ident, ok := p.Ident(path)
	if !ok {
		return "", fmt.Errorf("partial: unknown package %q", path)
	}

	if slices.Contains(supportPaths, path) {
		p.support[path] = true
	}

	return ident, nil
}

// partialString spells the partial variant of the target named.
func (p *mapper) partialString(named *types.Named) (string, error) {
	e, err := p.partial(named)
	if err != nil {
		return "", err
	}

	b, err := gofragment.PrintDstExpr(e)
	if err != nil {
		return "", fmt.Errorf("partial: %w", err)
	}

	return string(b), nil
}

func (p *mapper) typeParams(n *typegraph.Node, list *types.TypeParamList) (*dst.FieldList, error) {
	if list.Len() == 0 {
		return nil, nil
	}

	fields := &dst.FieldList{Opening: true, Closing: true}

	for tp := range list.TypeParams() {
		expr, err := p.TypeExpr(constraintType(tp))
		if err != nil {
			return nil, fmt.Errorf(
				"%s (%s): type parameter %s: %w",
				n.Id.String(), p.Position(tp.Obj().Pos()), tp.Obj().Name(), err,
			)
		}

		fields.List = append(fields.List, &dst.Field{
			Names: []*dst.Ident{dst.NewIdent(tp.Obj().Name())},
			Type:  expr,
		})
	}

	return fields, nil
}

// constraintType returns the constraint of tp as it is written. A
// constraint written as ~int | string is an implicit interface; printing it
// as such would spell interface{ ~int | string }.
func constraintType(tp *types.TypeParam) types.Type {
	c := tp.Constraint()
	if iface, ok := c.(*types.Interface); ok && iface.IsImplicit() && iface.NumEmbeddeds() == 1 {
		return iface.EmbeddedType(0)
	}

	return c
}

// constraintString spells the constraint of tp.
func (p *mapper) constraintString(tp *types.TypeParam) (string, error) {
	return p.TypeString(constraintType(tp))
}

// matchField matches the struct field v for the package of p.
func (p *mapper) matchField(v *types.Var) match { return p.sel.matchField(v, p.Pkg.PkgPath) }

// matchValue matches t, the type of a slice element or a map value, for
// the package of p.
func (p *mapper) matchValue(t types.Type) match { return p.sel.matchValue(t, p.Pkg.PkgPath) }

// structType returns the partial struct type of the target n. A target
// declared by a struct literal gets a clone of the literal with its field
// types replaced, as sourceStruct describes; any other target gets a
// struct built from s.
//
// It reports every error of the fields of s, so the method writers, which
// match the fields again, meet none.
func (p *mapper) structType(n *typegraph.Node, s *types.Struct) (dst.Expr, error) {
	synth := make([]*dst.Field, s.NumFields())

	var flags []*flagField

	seen := map[string]*types.Var{}

	for i := range s.NumFields() {
		v := s.Field(i)
		// A target defined on a struct type of another package, as in
		// type A sub.S, has the fields of sub.S, and the methods cannot
		// spell an unexported one.
		if v.Name() != "_" && !v.Exported() && v.Pkg() != nil && v.Pkg().Path() != p.Pkg.PkgPath {
			return nil, fmt.Errorf(
				"field %s (%s): %w: unexported field of package %s cannot be converted in package %s",
				v.Name(),
				p.Position(v.Pos()),
				ErrUnsupported,
				v.Pkg().Path(),
				p.Pkg.PkgPath,
			)
		}

		m := p.matchField(v)

		f, flag, err := p.field(n, v, m, s.Tag(i))
		if err == nil && !m.blank {
			name := partialField(v, m)

			switch prev := seen[name]; {
			case prev != nil:
				err = fmt.Errorf(
					"%w: partial field %s is given by %s too (%s)",
					ErrConflict, name, prev.Name(), p.Position(prev.Pos()),
				)
			case slices.Contains(p.sel.partialMethodNames(n), name):
				err = fmt.Errorf(
					"%w: partial field %s collides with the method %s",
					ErrConflict, name, name,
				)
			}

			seen[name] = v
		}

		if err != nil {
			return nil, fmt.Errorf("field %s (%s): %w", v.Name(), p.Position(v.Pos()), err)
		}

		synth[i] = f

		if flag != nil {
			flags = append(flags, flag)
		}
	}

	if err := checkFlags(n, flags); err != nil {
		return nil, err
	}

	st, err := p.sourceStruct(n, s, synth)
	if err != nil {
		return nil, err
	}

	if st != nil {
		return st, nil
	}

	fields := &dst.FieldList{Opening: true, Closing: true}
	fields.List = synth

	return &dst.StructType{Fields: fields}, nil
}

// field returns the partial field of the struct field v of the target n,
// matched as m, and the flag of the field, or nil.
//
// An embedded struct target, or pointer to one, stays embedded as its
// partial variant and keeps its tag: adding omitzero to it would stop
// encoders from inlining it. Any other embedded type becomes a field named
// after it, since its partial value is an und.Und, which cannot be
// embedded by the field name of the source. Encoders do not inline an
// embedded slice or map either, so keeping its name keeps its wire name.
func (p *mapper) field(
	n *typegraph.Node,
	v *types.Var,
	m match,
	tag string,
) (*dst.Field, *flagField, error) {
	if m.blank {
		// Nothing refers to a blank field, so it has no partial value and
		// the methods skip it. The partial struct keeps it with its type and
		// tag as written: a blank field may direct the compiler, as
		// _ structs.HostLayout does.
		if _, err := p.flag(n, v, m); err != nil {
			return nil, nil, err
		}

		expr, err := p.TypeExpr(v.Type())
		if err != nil {
			return nil, nil, err
		}

		return withTag(
			&dst.Field{Names: []*dst.Ident{dst.NewIdent("_")}, Type: expr},
			tag,
		), nil, nil
	}

	expr, err := p.partialExpr(m)
	if err != nil {
		return nil, nil, err
	}

	f := &dst.Field{Type: expr}

	if !m.embedded {
		tag, err = partialTag(tag, v.Exported())
		if err != nil {
			return nil, nil, err
		}

		f.Names = []*dst.Ident{dst.NewIdent(v.Name())}
	}

	if _, err := p.strategy(m, fieldKeyOf(n.Id, v.Name())); err != nil {
		return nil, nil, err
	}

	flag, err := p.flag(n, v, m)
	if err != nil {
		return nil, nil, err
	}

	return withTag(f, tag), flag, nil
}

// strategy returns the merge strategy of the field k matched as m, set by
// its directive or defaulted by the options.
//
// A value held whole and an elastic.Elastic accept overwrite only. A slice
// or a map accepts overwrite and append. A partial value accepts recursive
// and overwrite, and append too when it is the partial variant of a slice
// or a map. An embedded partial value accepts recursive and overwrite.
func (p *mapper) strategy(m match, k fieldKey) (string, error) {
	var (
		def     string
		allowed []string
		o       = p.sel.merges[k]
	)

	switch {
	case m.embedded:
		def = string(p.sel.opts.MergeNested)
		allowed = []string{string(MergeNestedRecursive), string(MergeNestedOverwrite)}
	case m.kind == leafElastic:
		def, allowed = string(MergeOverwrite), []string{string(MergeOverwrite)}
	case m.kind == leafTarget && m.collection():
		def = string(p.sel.opts.MergeNested)
		allowed = []string{
			string(MergeNestedRecursive), string(MergeNestedOverwrite), string(MergeAppend),
		}
	case m.kind == leafTarget:
		def = string(p.sel.opts.MergeNested)
		allowed = []string{string(MergeNestedRecursive), string(MergeNestedOverwrite)}
	case m.collection():
		def = string(p.sel.opts.Merge)
		allowed = []string{string(MergeOverwrite), string(MergeAppend)}
	default:
		def, allowed = string(MergeOverwrite), []string{string(MergeOverwrite)}
	}

	if o.strategy == "" {
		return def, nil
	}

	if !slices.Contains(allowed, o.strategy) {
		return "", fmt.Errorf(
			"%w: %s: %s %s on field %s; want %s",
			ErrDirective, o.src.origin(), o.src.spelling(), o.strategy, k,
			strings.Join(allowed, " or "),
		)
	}

	return o.strategy, nil
}

func isStruct(named *types.Named) bool {
	_, ok := named.Underlying().(*types.Struct)
	return ok
}

func withTag(f *dst.Field, tag string) *dst.Field {
	if tag != "" {
		f.Tag = &dst.BasicLit{Kind: token.STRING, Value: structtag.Literal(tag)}
	}

	return f
}

// partialExpr spells the partial form of a type matched as m:
//
//	T              und.Und[T]
//	M              und.Und[MPartial]  (M a target)
//	*T, *M         as T and M
//	*M             und.Und[*MPartial] (ref, closing a cycle)
//	opt.Option[T]  und.Und[T]
//	und.Und[T]     und.Und[T]
//	elastic.Elastic[T] elastic.Elastic[T]
//	embedded M, *M MPartial
//
// Any other type, a slice, map or array included, maps as a whole as T
// does. So does a target whose partial variant cannot be spelled in the
// package of the mapper.
func (p *mapper) partialExpr(m match) (dst.Expr, error) {
	if m.embedded {
		return p.partial(m.named)
	}

	if len(m.route) > 1 {
		return nil, fmt.Errorf("%w: pointer to pointer %s", ErrUnsupported, m.source)
	}

	switch m.kind {
	case leafUnd:
		p.support[undPath] = true
		return p.TypeExpr(m.source)
	case leafElastic:
		return p.TypeExpr(m.source)
	case leafTarget:
		inner, err := p.partial(m.named)
		if err != nil {
			return nil, err
		}

		if m.ref {
			inner = &dst.StarExpr{X: inner}
		}

		return p.und(inner)
	}

	inner, err := p.TypeExpr(m.leaf)
	if err != nil {
		return nil, err
	}

	return p.und(inner)
}

func (p *mapper) und(inner dst.Expr) (dst.Expr, error) {
	sel := p.Imports.DstExpr(importdecl.QualifiedType{PackagePath: undPath, TypeName: "Und"})
	if sel == nil {
		return nil, fmt.Errorf("partial: unknown package %q", undPath)
	}

	p.support[undPath] = true

	return &dst.IndexExpr{X: sel, Index: inner}, nil
}

// partial returns the partial variant of the target named, instantiated
// with the type arguments of named.
func (p *mapper) partial(named *types.Named) (dst.Expr, error) {
	obj := named.Obj()

	var x dst.Expr = dst.NewIdent(partialName(obj.Name()))
	if pkgPath := obj.Pkg().Path(); pkgPath != p.Pkg.PkgPath {
		ident, ok := p.Ident(pkgPath)
		if !ok {
			return nil, fmt.Errorf("partial: unknown package %q", pkgPath)
		}

		x = &dst.SelectorExpr{X: dst.NewIdent(ident), Sel: dst.NewIdent(partialName(obj.Name()))}
	} else {
		p.refs[partialName(obj.Name())] = true
	}

	args, err := opiter.TryCollect(
		opiter.WrapSeq(named.TypeArgs().Types()).
			Disassemble(p.TypeExpr),
	)
	if err != nil {
		return nil, err
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
