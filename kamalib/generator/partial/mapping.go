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
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/gofragment"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/structtag"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typeprint"
	"github.com/ngicks/opiter"
)

const (
	optPath     = "github.com/ngicks/option/opt"
	undPath     = "github.com/ngicks/option/und"
	elasticPath = "github.com/ngicks/option/elastic"
)

// mapper builds the partial declarations of a file.
type mapper struct {
	sel *selection
	pkg *packages.Package
	// parser gave m, the imports of the file.
	parser *importdecl.Parser
	m      *importdecl.Map
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
	ident, ok := p.m.Ident(path)
	if !ok {
		return "", fmt.Errorf("partial: unknown package %q", path)
	}

	if slices.Contains(supportPaths, path) {
		p.support[path] = true
	}

	return ident, nil
}

// typeString spells t.
func (p *mapper) typeString(t types.Type) (string, error) {
	e, err := p.print(t)
	if err != nil {
		return "", err
	}

	b, err := gofragment.PrintDstExpr(e)
	if err != nil {
		return "", fmt.Errorf("partial: %w", err)
	}

	return string(b), nil
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
		expr, err := p.print(constraintType(tp))
		if err != nil {
			return nil, fmt.Errorf(
				"%s (%s): type parameter %s: %w",
				n.Id.String(), p.position(tp.Obj().Pos()), tp.Obj().Name(), err,
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
	return p.typeString(constraintType(tp))
}

// conv selects how a value converts between its source and partial form.
type conv int

const (
	// convPlain: T <-> und.Und[T].
	convPlain conv = iota
	// convSlice and convMap: T <-> und.Und[T], nil <-> null.
	convSlice
	convMap
	// convPointer: *T <-> und.Und[T], nil <-> null.
	convPointer
	// convOption: opt.Option[T] <-> und.Und[T], none <-> null.
	convOption
	// convCopy: und.Und and elastic.Elastic are copied.
	convCopy
	// convTarget: M <-> und.Und[MPartial].
	convTarget
	// convTargetPointer: *M <-> und.Und[MPartial], nil <-> null.
	convTargetPointer
	// convTargetRef: *M <-> und.Und[*MPartial], nil <-> null.
	convTargetRef
	// convEmbedded: embedded M <-> embedded MPartial.
	convEmbedded
	// convEmbeddedPointer: embedded *M <-> embedded MPartial, nil <-> zero.
	convEmbeddedPointer
	// convSkip: a blank field, which the partial struct copies and the
	// methods do not touch.
	convSkip
)

// class groups values by the merge strategies they accept.
type class int

const (
	// classPlain accepts overwrite only.
	classPlain class = iota
	// classElastic accepts overwrite only.
	classElastic
	// classSlice and classMap accept overwrite and append.
	classSlice
	classMap
	// classSub is a partial variant of a struct and accepts recursive and
	// overwrite.
	classSub
	// classSubSlice and classSubMap are partial variants of slices and
	// maps, and accept recursive, overwrite and append.
	classSubSlice
	classSubMap
	// classEmbedded is an embedded partial variant and accepts recursive
	// and overwrite.
	classEmbedded
)

// mapped is the partial form of a field, slice element or map value.
type mapped struct {
	expr  dst.Expr
	conv  conv
	class class
	// source is the source type. route are the pointer steps from source to
	// leaf, the source type the partial form holds: the target, the type
	// argument of an option or the source type itself.
	source types.Type
	route  []typegraph.EdgeRouteNode
	leaf   types.Type
	// named is the target leaf refers to, or nil.
	named *types.Named
}

// fieldInfo is a mapped struct field.
type fieldInfo struct {
	mapped
	// name is the name of the source field; partial is the name of the
	// partial field.
	name, partial string
	// strategy is the merge strategy of the field.
	strategy string
	// flag is the flag of the field, or nil.
	flag *flagField
}

// structType returns the partial struct type of the target n and its
// mapped fields. A target declared by a struct literal gets a clone of the
// literal with its field types replaced, as sourceStruct describes; any
// other target gets a struct built from s.
func (p *mapper) structType(n *typegraph.Node, s *types.Struct) (dst.Expr, []fieldInfo, error) {
	synth := make([]*dst.Field, s.NumFields())

	var infos []fieldInfo

	seen := map[string]*types.Var{}

	for i := range s.NumFields() {
		v := s.Field(i)
		// A target defined on a struct type of another package, as in
		// type A sub.S, has the fields of sub.S, and the methods cannot
		// spell an unexported one.
		if v.Name() != "_" && !v.Exported() && v.Pkg() != nil && v.Pkg().Path() != p.pkg.PkgPath {
			return nil, nil, fmt.Errorf(
				"field %s (%s): %w: unexported field of package %s cannot be converted in package %s",
				v.Name(),
				p.position(v.Pos()),
				ErrUnsupported,
				v.Pkg().Path(),
				p.pkg.PkgPath,
			)
		}

		f, info, err := p.field(n, v, s.Tag(i))
		if err == nil && info.partial != "_" {
			switch prev := seen[info.partial]; {
			case prev != nil:
				err = fmt.Errorf(
					"%w: partial field %s is given by %s too (%s)",
					ErrConflict, info.partial, prev.Name(), p.position(prev.Pos()),
				)
			case slices.Contains(p.sel.partialMethodNames(n), info.partial):
				err = fmt.Errorf(
					"%w: partial field %s collides with the method %s",
					ErrConflict, info.partial, info.partial,
				)
			}

			seen[info.partial] = v
		}

		if err != nil {
			return nil, nil, fmt.Errorf("field %s (%s): %w", v.Name(), p.position(v.Pos()), err)
		}

		synth[i] = f

		infos = append(infos, info)
	}

	if err := checkFlags(n, infos); err != nil {
		return nil, nil, err
	}

	st, err := p.sourceStruct(n, s, synth)
	if err != nil {
		return nil, nil, err
	}

	if st != nil {
		return st, infos, nil
	}

	fields := &dst.FieldList{Opening: true, Closing: true}

	fields.List = synth

	return &dst.StructType{Fields: fields}, infos, nil
}

// field maps a struct field of the target n.
//
// An embedded struct target, or pointer to one, stays embedded as its
// partial variant and keeps its tag: adding omitzero to it would stop
// encoders from inlining it. Any other embedded type becomes a field named
// after it, since its partial value is an und.Und, which cannot be
// embedded by the field name of the source. Encoders do not inline an
// embedded slice or map either, so keeping its name keeps its wire name.
func (p *mapper) field(n *typegraph.Node, v *types.Var, tag string) (*dst.Field, fieldInfo, error) {
	info := fieldInfo{source: v.Type(), name: v.Name(), partial: v.Name()}
	key := fieldKeyOf(n.Id, v.Name())

	var (
		f   *dst.Field
		err error
	)

	if v.Name() == "_" {
		// Nothing refers to a blank field, so it has no partial value and
		// the methods skip it. The partial struct keeps it with its type and
		// tag as written: a blank field may direct the compiler, as
		// _ structs.HostLayout does.
		info.conv = convSkip

		info.flag, err = p.flag(n, info, p.sel.flags[key])
		if err != nil {
			return nil, fieldInfo{}, err
		}

		expr, err := p.print(v.Type())
		if err != nil {
			return nil, fieldInfo{}, err
		}

		return withTag(
			&dst.Field{Names: []*dst.Ident{dst.NewIdent("_")}, Type: expr},
			tag,
		), info, nil
	}

	if named, ok := p.sel.partialOf(deref(v.Type()), p.pkg.PkgPath); ok && v.Embedded() &&
		isStruct(named) {
		info.partial = partialName(named.Obj().Name())
		info.class = classEmbedded
		info.conv = convEmbedded
		info.named = named

		info.leaf, info.route = typegraph.PointerRoute(v.Type())
		if len(info.route) > 0 {
			info.conv = convEmbeddedPointer
		}

		info.expr, err = p.partial(named)
		if err != nil {
			return nil, fieldInfo{}, err
		}

		f = withTag(&dst.Field{Type: info.expr}, tag)
	} else {
		info.mapped, err = p.value(v.Type(), p.sel.refs[v])
		if err != nil {
			return nil, fieldInfo{}, err
		}

		tag, err = partialTag(tag, v.Exported())
		if err != nil {
			return nil, fieldInfo{}, err
		}

		f = withTag(&dst.Field{Names: []*dst.Ident{dst.NewIdent(v.Name())}, Type: info.expr}, tag)
	}

	info.strategy, err = p.strategy(info.class, key, p.sel.merges[key])
	if err != nil {
		return nil, fieldInfo{}, err
	}

	info.flag, err = p.flag(n, info, p.sel.flags[key])
	if err != nil {
		return nil, fieldInfo{}, err
	}

	return f, info, nil
}

// strategy returns the merge strategy of the field k holding a value of
// class c, set by o or defaulted by the options.
func (p *mapper) strategy(c class, k fieldKey, o mergeOption) (string, error) {
	var (
		def     string
		allowed []string
	)

	switch c {
	case classPlain, classElastic:
		def, allowed = string(MergeOverwrite), []string{string(MergeOverwrite)}
	case classSlice, classMap:
		def = string(p.sel.opts.Merge)
		allowed = []string{string(MergeOverwrite), string(MergeAppend)}
	case classSub, classEmbedded:
		def = string(p.sel.opts.MergeNested)
		allowed = []string{string(MergeNestedRecursive), string(MergeNestedOverwrite)}
	case classSubSlice, classSubMap:
		def = string(p.sel.opts.MergeNested)
		allowed = []string{
			string(MergeNestedRecursive), string(MergeNestedOverwrite), string(MergeAppend),
		}
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

// value maps the type of a struct field, a slice element or a map value:
//
//	T              und.Und[T]
//	M              und.Und[MPartial]  (M a target)
//	*T, *M         as T and M
//	*M             und.Und[*MPartial] (ref, closing a cycle)
//	opt.Option[T]  und.Und[T]
//	und.Und[T]     und.Und[T]
//	elastic.Elastic[T] elastic.Elastic[T]
//
// Any other type, a slice, map or array included, maps as a whole as T
// does. So does a target whose partial variant cannot be spelled in the
// package of the mapper.
func (p *mapper) value(t types.Type, ref bool) (mapped, error) {
	leaf, route := typegraph.PointerRoute(t)
	if len(route) > 1 {
		return mapped{}, fmt.Errorf("%w: pointer to pointer %s", ErrUnsupported, t)
	}

	if len(route) == 1 {
		out, err := p.wrap(leaf, ref)
		switch out.conv {
		case convTarget:
			out.conv = convTargetPointer
			if ref {
				out.conv = convTargetRef
			}
		default:
			out.conv = convPointer
		}

		out.source, out.route = t, route

		return out, err
	}

	switch {
	case isNamed(t, optPath, "Option"):
		arg := types.Unalias(t).(*types.Named).TypeArgs().At(0)

		elem, err := p.print(arg)
		if err != nil {
			return mapped{}, err
		}

		expr, err := p.und(elem)

		return mapped{
			expr: expr, conv: convOption, class: plainClass(arg), source: t, leaf: arg,
		}, err
	case isNamed(t, undPath, "Und"):
		p.support[undPath] = true
		expr, err := p.print(t)
		arg := types.Unalias(t).(*types.Named).TypeArgs().At(0)

		return mapped{
			expr: expr, conv: convCopy, class: plainClass(arg), source: t, leaf: arg,
		}, err
	case isNamed(t, elasticPath, "Elastic"):
		expr, err := p.print(t)
		return mapped{expr: expr, conv: convCopy, class: classElastic, source: t, leaf: t}, err
	}

	return p.wrap(t, false)
}

// wrap returns und.Und of t, or of the partial variant of t when it is a
// target. ref wraps a pointer to the partial variant.
func (p *mapper) wrap(t types.Type, ref bool) (mapped, error) {
	named, ok := p.sel.partialOf(t, p.pkg.PkgPath)
	if !ok {
		inner, err := p.print(t)
		if err != nil {
			return mapped{}, err
		}

		expr, err := p.und(inner)

		out := mapped{expr: expr, conv: convPlain, class: plainClass(t), source: t, leaf: t}
		switch out.class {
		case classSlice:
			out.conv = convSlice
		case classMap:
			out.conv = convMap
		}

		return out, err
	}

	inner, err := p.partial(named)
	if err != nil {
		return mapped{}, err
	}

	if ref {
		inner = &dst.StarExpr{X: inner}
	}

	expr, err := p.und(inner)

	out := mapped{
		expr: expr, conv: convTarget, class: classSub, source: t, leaf: t, named: named,
	}
	switch named.Underlying().(type) {
	case *types.Slice:
		out.class = classSubSlice
	case *types.Map:
		out.class = classSubMap
	}

	return out, err
}

// plainClass is the class of a value of type t held whole.
func plainClass(t types.Type) class {
	switch t.Underlying().(type) {
	case *types.Slice:
		return classSlice
	case *types.Map:
		return classMap
	}

	return classPlain
}

func (p *mapper) und(inner dst.Expr) (dst.Expr, error) {
	sel := p.m.DstExpr(importdecl.QualifiedType{PackagePath: undPath, TypeName: "Und"})
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
	if pkgPath := obj.Pkg().Path(); pkgPath != p.pkg.PkgPath {
		ident, ok := p.m.Ident(pkgPath)
		if !ok {
			return nil, fmt.Errorf("partial: unknown package %q", pkgPath)
		}

		x = &dst.SelectorExpr{X: dst.NewIdent(ident), Sel: dst.NewIdent(partialName(obj.Name()))}
	} else {
		p.refs[partialName(obj.Name())] = true
	}

	args, err := opiter.TryCollect(
		opiter.WrapSeq(named.TypeArgs().Types()).
			Disassemble(p.print),
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
