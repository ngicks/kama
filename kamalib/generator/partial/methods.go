package partial

import (
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/dave/dst"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// methodNames are the methods every partial type gets.
var methodNames = []string{"FromValue", "ToValue", "Merge", "ApplyTo"}

// localNames are the identifiers the generated methods and helpers
// declare.
var localNames = []string{
	"p", "v", "r", "base", "i", "k", "e", "x", "f", "err", "l", "lv", "rv", "o",
}

// partialMethodNames returns the methods the partial type of n gets.
func (s *selection) partialMethodNames(n *typegraph.Node) []string {
	out := methodNames
	if s.fieldMasked(n.Id) {
		out = slices.Concat(out, fieldMaskMethodNames)
	}

	if s.flagged(n.Id) {
		out = slices.Concat(out, flagMethodNames)
	}

	return out
}

// flagged reports whether a field of the target id holds a flag option.
func (s *selection) flagged(id typegraph.QualifiedType) bool {
	to, _ := s.typeOption(id)
	fields, _ := to.Fields.Unpack()

	return opiter.WrapSeq(maps.Values(fields)).
		Any(func(o FieldOptions) bool { return o.Flag != nil })
}

// methodWriter writes the methods of one partial type as source text.
type methodWriter struct {
	p *mapper
	n *typegraph.Node
	b strings.Builder
	// tparams are the names the methods spell the type parameters by.
	tparams []string
	// recv and source spell the partial type and the source type with the
	// type parameters of the source.
	recv, source string
}

func (p *mapper) newMethodWriter(n *typegraph.Node) *methodWriter {
	named := n.Detail.Type

	var (
		tparams []string
		targs   string
	)

	if named.TypeParams().Len() > 0 {
		// No field type refers to a blank parameter, so only the receiver and
		// the source type spell the name it gets.
		tparams = base.MethodTypeParams(p.Pkg.Types.Scope(), named.TypeParams(), localNames)
		targs = "[" + strings.Join(tparams, ", ") + "]"
	}

	return &methodWriter{
		p:       p,
		n:       n,
		tparams: tparams,
		recv:    partialName(named.Obj().Name()) + targs,
		source:  named.Obj().Name() + targs,
	}
}

// function writes a method documented by doc. signature declares its
// receiver and parameters in the scope of the method, where body renders.
func (w *methodWriter) function(doc, signature genExpr, body ...genStep) error {
	s, err := lang.Render(
		w.p.FuncContext(w.tparams),
		lang.DocFunc(doc, signature, body...),
	)
	if err != nil {
		return err
	}

	w.b.WriteString(s)

	return nil
}

// from returns the statement setting dst to the partial form of the source
// value src, whose type matches as m.
func (w *methodWriter) from(m match, dst, src genExpr) genStep {
	assign := func(rhs genExpr) genStep { return lang.Stmt("%s = %s", dst, rhs) }
	und := func(genCtx) (string, error) { return w.p.ident(typequery.UndPath) }
	call := func(op helperOp) genStep {
		return assign(w.p.call(&helper{op: op, typ: m.source, route: m.route, named: m.named}, src))
	}

	switch {
	case m.embedded && m.pointer():
		return call(opFromEmbedded)
	case m.embedded:
		return lang.Stmt("%s.FromValue(%s)", dst, src)
	case m.ref:
		return call(opFromTargetRef)
	case m.kind == leafPartial:
		return call(opFromTarget)
	case m.pointer():
		return assign(lang.Expr("%s.FromPointer(%s).CollapseToNull()", und, src))
	case m.kind == leafOption:
		return assign(lang.Expr("%s.FromOption(%s, true)", und, src))
	case m.kind == leafUnd, m.kind == leafElastic:
		return assign(src)
	}

	switch m.leaf.Underlying().(type) {
	case *types.Slice:
		return assign(lang.Expr("%s.WrapSlice(%s).CollapseToNull()", und, src))
	case *types.Map:
		return assign(lang.Expr("%s.WrapMap(%s).CollapseToNull()", und, src))
	}

	return assign(lang.Expr("%s.Defined(%s)", und, src))
}

// to returns the statements setting dst to the value the partial value src
// describes, for a source type matching as m. A conditional conversion
// leaves dst as it is when src is undefined or null, so dst must hold the
// zero value.
func (w *methodWriter) to(m match, dst, src genExpr) genStep {
	assign := func(rhs genExpr) genStep { return lang.Stmt("%s = %s", dst, rhs) }
	call := func(op helperOp) genStep {
		return assign(w.p.call(&helper{op: op, typ: m.source, route: m.route, named: m.named}, src))
	}

	switch {
	case m.embedded && m.pointer():
		return call(opToEmbedded)
	case m.embedded:
		return assign(lang.Expr("%s.ToValue()", src))
	case m.ref:
		return call(opToTargetRef)
	case m.kind == leafPartial && m.pointer():
		return call(opToTarget)
	case m.kind == leafPartial:
		// Value gives the zero partial value for undefined and null, which
		// converts back to the zero value.
		return assign(lang.Expr("%s.Value().ToValue()", src))
	case m.pointer():
		// opt.Option.Pointer has a pointer receiver, which the result of
		// Flatten does not provide.
		return lang.If(lang.Expr("%s.IsDefined()", src), genSteps{
			lang.Stmt("%s := %s.Value()", decl("o"), src),
			lang.Stmt("%s = &%s", dst, lang.Name("o")),
		}, nil)
	case m.kind == leafOption:
		return assign(lang.Expr("%s.Flatten()", src))
	case m.kind == leafUnd, m.kind == leafElastic:
		return assign(src)
	}

	return assign(lang.Expr("%s.Value()", src))
}

// conditional reports whether the statements of to for a type matching as
// m leave their destination unset for some values.
func conditional(m match) bool { return m.pointer() && m.kind != leafPartial }

// merge returns the statement merging the field r onto the field l, both
// partial fields of the field v matched as m.
func (w *methodWriter) merge(v *types.Var, m match, l, r genExpr) genStep {
	strategy, err := w.p.strategy(m, fieldKeyOf(w.n.Id, v.Name()))
	if err != nil {
		return lang.Fail(err)
	}

	overwrite := lang.If(lang.Expr("!%s.IsUndefined()", r), lang.Stmt("%s = %s", l, r), nil)
	assign := func(rhs genExpr) genStep { return lang.Stmt("%s = %s", l, rhs) }

	switch {
	case m.embedded && strategy == string(MergeNestedRecursive):
		return assign(lang.Expr("%s.Merge(%s)", l, r))
	case m.embedded:
		// An embedded partial has no undefined state of its own.
		test := w.p.call(&helper{op: opAllUndefined, typ: m.named, named: m.named}, r)
		return lang.If(lang.Expr("!%s", test), lang.Stmt("%s = %s", l, r), nil)
	case m.kind == leafElastic:
		return overwrite
	case strategy == string(MergeAppend) && m.kind == leafPartial:
		return assign(w.p.call(
			&helper{op: opMergeAppend, typ: m.named, named: m.named, partial: true}, l, r,
		))
	case strategy == string(MergeAppend):
		return assign(w.p.call(&helper{op: opMergeAppend, typ: m.leaf}, l, r))
	case strategy == string(MergeNestedRecursive) && m.ref:
		return assign(w.p.call(&helper{
			op: opMergeRecursive, typ: m.source, route: m.route, named: m.named, partial: true,
		}, l, r))
	case strategy == string(MergeNestedRecursive):
		return assign(w.p.call(
			&helper{op: opMergeRecursive, typ: m.named, named: m.named, partial: true}, l, r,
		))
	}

	return overwrite
}

func (w *methodWriter) applyTo() error {
	return w.function(
		docComment("ApplyTo returns %s with the change %s describes applied. It is\n"+
			"FromValue of %s merged with %s, converted back with ToValue.",
			lang.Name("v"), lang.Name("p"), lang.Name("v"), lang.Name("p")),
		lang.Expr("func (%s %s) ApplyTo(%s %s) %s",
			decl("p"), w.recv, decl("v"), w.source, w.source),
		lang.Stmt("var %s %s", decl("base"), w.recv),
		lang.Stmt("%s.FromValue(%s)", lang.Name("base"), lang.Name("v")),
		lang.Stmt("return %s.Merge(%s).ToValue()", lang.Name("base"), lang.Name("p")),
	)
}

// fieldSteps returns the step each field of st that is not blank gets.
// step takes the field and what it matches as.
func (w *methodWriter) fieldSteps(
	st *types.Struct,
	step func(v *types.Var, m match) genStep,
) genSteps {
	return opiter.WrapSeq(st.Fields()).
		Filter(func(v *types.Var) bool { return v.Name() != "_" }).
		Map(func(v *types.Var) genStep { return step(v, w.p.matchField(w.n, v)) }).
		CollectSlice()
}

// fieldOf returns the field sel of the value role refers to.
func fieldOf(role, sel string) genExpr {
	return lang.Expr("%s.%s", lang.Name(role), sel)
}

// structMethods writes the methods of the partial type of the struct st.
func (w *methodWriter) structMethods(st *types.Struct) error {
	err := w.function(
		docComment("FromValue sets every field of %s to the partial form of the field of %s.\n"+
			"A nil pointer, slice or map and a none option become null.",
			lang.Name("p"), lang.Name("v")),
		lang.Expr("func (%s *%s) FromValue(%s %s)", decl("p"), w.recv, decl("v"), w.source),
		w.fieldSteps(st, func(v *types.Var, m match) genStep {
			return w.from(m, fieldOf("p", partialField(v, m)), fieldOf("v", v.Name()))
		}),
	)
	if err != nil {
		return err
	}

	err = w.function(
		docComment("ToValue returns the value %s describes. An undefined or null field\n"+
			"gives the zero value of the field.", lang.Name("p")),
		lang.Expr("func (%s %s) ToValue() %s", decl("p"), w.recv, w.source),
		lang.Stmt("var %s %s", decl("v"), w.source),
		w.fieldSteps(st, func(v *types.Var, m match) genStep {
			return w.to(m, fieldOf("v", v.Name()), fieldOf("p", partialField(v, m)))
		}),
		lang.Stmt("return %s", lang.Name("v")),
	)
	if err != nil {
		return err
	}

	err = w.function(
		docComment("Merge returns %s with the fields of %s merged onto it. An undefined\n"+
			"field of %s keeps the field of %s. Merge writes to neither %s nor %s.",
			lang.Name("p"), lang.Name("r"), lang.Name("r"),
			lang.Name("p"), lang.Name("p"), lang.Name("r")),
		lang.Expr("func (%s %s) Merge(%s %s) %s", decl("p"), w.recv, decl("r"), w.recv, w.recv),
		w.fieldSteps(st, func(v *types.Var, m match) genStep {
			name := partialField(v, m)
			return w.merge(v, m, fieldOf("p", name), fieldOf("r", name))
		}),
		lang.Stmt("return %s", lang.Name("p")),
	)
	if err != nil {
		return err
	}

	if err := w.applyTo(); err != nil {
		return err
	}

	if err := w.fieldMaskMethods(st); err != nil {
		return err
	}

	return w.flagMethods(st)
}

// collectionMethods writes the methods of a slice or map partial type,
// whose elements are of the source type source.
func (w *methodWriter) collectionMethods(isMap bool, source types.Type) error {
	elem := w.p.matchValue(source)

	idx := "i"
	if isMap {
		idx = "k"
	}

	// A map element missing from the map is not the zero value, so a
	// conversion that may leave its destination unset goes through a local.
	// back reports whether conv converts back to source values, as to does,
	// whose conditional statements need a zero destination. typ is the
	// element type of dst then.
	elemLoop := func(
		dst, src genExpr, conv convFunc, back bool, typ string,
	) genStep {
		if !isMap || !back || !conditional(elem) {
			return lang.ForRange(idx, "e", src,
				conv(elem, lang.Expr("%s[%s]", dst, lang.Name(idx)), lang.Name("e")),
			)
		}

		return lang.ForRange(idx, "e", src, genSteps{
			lang.Stmt("var %s %s", decl("x"), typ),
			conv(elem, lang.Name("x"), lang.Name("e")),
			lang.Stmt("%s[%s] = %s", dst, lang.Name(idx), lang.Name("x")),
		})
	}

	err := w.function(
		docComment("FromValue sets %s to the partial forms of the elements of %s. A nil %s\n"+
			"gives a nil %s.", lang.Name("p"), lang.Name("v"), lang.Name("v"), lang.Name("p")),
		lang.Expr("func (%s *%s) FromValue(%s %s)", decl("p"), w.recv, decl("v"), w.source),
		lang.If(lang.Expr("%s == nil", lang.Name("v")), genSteps{
			lang.Stmt("*%s = nil", lang.Name("p")),
			lang.Stmt("return"),
		}, nil),
		lang.Stmt("*%s = make(%s, len(%s))", lang.Name("p"), w.recv, lang.Name("v")),
		elemLoop(lang.Expr("(*%s)", lang.Name("p")), lang.Name("v"), w.from, false, ""),
	)
	if err != nil {
		return err
	}

	elemType, err := w.p.TypeString(source)
	if err != nil {
		return err
	}

	err = w.function(
		docComment("ToValue returns the value %s describes. An undefined or null element\n"+
			"gives the zero value of the element, and a nil %s gives nil.",
			lang.Name("p"), lang.Name("p")),
		lang.Expr("func (%s %s) ToValue() %s", decl("p"), w.recv, w.source),
		lang.If(lang.Expr("%s == nil", lang.Name("p")), lang.Stmt("return nil"), nil),
		lang.Stmt("%s := make(%s, len(%s))", decl("v"), w.source, lang.Name("p")),
		elemLoop(lang.Name("v"), lang.Name("p"), w.to, true, elemType),
		lang.Stmt("return %s", lang.Name("v")),
	)
	if err != nil {
		return err
	}

	mergeDoc := lang.Expr("returns %s, or %s when %s is nil.",
		lang.Name("r"), lang.Name("p"), lang.Name("r"))
	if w.p.sel.opts.Merge == MergeAppend {
		mergeDoc = lang.Expr("returns the elements of %s then %s in a new slice. A nil\n"+
			"%s or %s gives the other.",
			lang.Name("p"), lang.Name("r"), lang.Name("p"), lang.Name("r"))
		if isMap {
			mergeDoc = lang.Expr("returns the entries of %s with the entries of %s set in a\n"+
				"new map. A nil %s or %s gives the other.",
				lang.Name("p"), lang.Name("r"), lang.Name("p"), lang.Name("r"))
		}
	}

	merge := genSteps{
		lang.If(lang.Expr("%s == nil", lang.Name("r")),
			lang.Stmt("return %s", lang.Name("p")), nil),
	}

	switch {
	case w.p.sel.opts.Merge != MergeAppend:
		merge = append(merge, lang.Stmt("return %s", lang.Name("r")))
	case isMap:
		mp, err := w.p.ident(mapsPath)
		if err != nil {
			return err
		}

		merge = append(merge,
			lang.If(lang.Expr("%s == nil", lang.Name("p")),
				lang.Stmt("return %s", lang.Name("r")), nil),
			lang.Stmt("%s := make(%s, len(%s)+len(%s))",
				decl("o"), w.recv, lang.Name("p"), lang.Name("r")),
			lang.Stmt("%s.Copy(%s, %s)", mp, lang.Name("o"), lang.Name("p")),
			lang.Stmt("%s.Copy(%s, %s)", mp, lang.Name("o"), lang.Name("r")),
			lang.Stmt("return %s", lang.Name("o")),
		)
	default:
		merge = append(merge,
			lang.If(lang.Expr("%s == nil", lang.Name("p")),
				lang.Stmt("return %s", lang.Name("r")), nil),
			lang.Stmt("%s := make(%s, 0, len(%s)+len(%s))",
				decl("o"), w.recv, lang.Name("p"), lang.Name("r")),
			lang.Stmt("%s = append(%s, %s...)", lang.Name("o"), lang.Name("o"), lang.Name("p")),
			lang.Stmt("return append(%s, %s...)", lang.Name("o"), lang.Name("r")),
		)
	}

	err = w.function(
		docComment("Merge %s Merge writes to neither %s nor %s.",
			mergeDoc, lang.Name("p"), lang.Name("r")),
		lang.Expr("func (%s %s) Merge(%s %s) %s", decl("p"), w.recv, decl("r"), w.recv, w.recv),
		merge...,
	)
	if err != nil {
		return err
	}

	return w.applyTo()
}

// convFunc returns the statements setting dst to the conversion of src,
// whose source type matches as m, as from and to do.
type convFunc func(m match, dst, src genExpr) genStep

// decls parses the methods written.
func (w *methodWriter) decls() ([]dst.Decl, error) {
	decls, err := genhelper.ParseDecls([]byte(w.b.String()))
	if err != nil {
		return nil, fmt.Errorf("partial: parse methods of %s: %w", w.recv, err)
	}

	return decls, nil
}
