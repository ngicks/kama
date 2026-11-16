package partial

import (
	"fmt"
	"go/types"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/dave/dst"

	"github.com/ngicks/kama/kamalib/pkg/gofragment"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
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
	if s.flagged[n.Id] {
		out = slices.Concat(out, flagMethodNames)
	}
	return out
}

// methodTypeParams returns the names the methods spell the type parameters
// of tparams by. A named parameter keeps its name. A blank one, which the
// methods cannot spell, gets a name taken by no package-level identifier
// of scope, no other type parameter, no local name and no predeclared
// identifier. No field type refers to a blank parameter, so only the
// receiver and the source type spell the new name.
func methodTypeParams(scope *types.Scope, tparams *types.TypeParamList) []string {
	taken := map[string]bool{}
	for _, name := range scope.Names() {
		taken[name] = true
	}
	for _, name := range types.Universe.Names() {
		taken[name] = true
	}
	for tp := range tparams.TypeParams() {
		taken[tp.Obj().Name()] = true
	}
	for _, name := range localNames {
		taken[name] = true
	}
	var out []string
	for i, tp := range slices.Collect(tparams.TypeParams()) {
		name := tp.Obj().Name()
		if name == "_" {
			name = fmt.Sprintf("T%d", i)
			for taken[name] {
				name += "_"
			}
			taken[name] = true
		}
		out = append(out, name)
	}
	return out
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
	var tparams []string
	var targs string
	if named.TypeParams().Len() > 0 {
		tparams = methodTypeParams(p.pkg.Types.Scope(), named.TypeParams())
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
		w.p.funcContext(w.tparams),
		lang.DocFunc(doc, signature, body...),
	)
	if err != nil {
		return err
	}
	w.b.WriteString(s)
	return nil
}

// from returns the statement setting dst to the partial form of the source
// value src.
func (w *methodWriter) from(m mapped, dst, src genExpr) genStep {
	assign := func(rhs genExpr) genStep { return lang.Stmt("%s = %s", dst, rhs) }
	und := func(genCtx) (string, error) { return w.p.ident(undPath) }
	call := func(op helperOp) genStep {
		return assign(w.p.call(&helper{op: op, typ: m.source, route: m.route, named: m.named}, src))
	}
	switch m.conv {
	case convPlain:
		return assign(lang.Expr("%s.Defined(%s)", und, src))
	case convSlice:
		return assign(lang.Expr("%s.WrapSlice(%s).CollapseToNull()", und, src))
	case convMap:
		return assign(lang.Expr("%s.WrapMap(%s).CollapseToNull()", und, src))
	case convPointer:
		return assign(lang.Expr("%s.FromPointer(%s).CollapseToNull()", und, src))
	case convOption:
		return assign(lang.Expr("%s.FromOption(%s, true)", und, src))
	case convCopy:
		return assign(src)
	case convTarget, convTargetPointer:
		return call(opFromTarget)
	case convTargetRef:
		return call(opFromTargetRef)
	case convEmbedded:
		return lang.Stmt("%s.FromValue(%s)", dst, src)
	case convEmbeddedPointer:
		return call(opFromEmbedded)
	}
	return lang.Fail(unknownConv(m.conv))
}

// to returns the statements setting dst to the value the partial value src
// describes. A conditional conversion leaves dst as it is when src is
// undefined or null, so dst must hold the zero value.
func (w *methodWriter) to(m mapped, dst, src genExpr) genStep {
	assign := func(rhs genExpr) genStep { return lang.Stmt("%s = %s", dst, rhs) }
	call := func(op helperOp) genStep {
		return assign(w.p.call(&helper{op: op, typ: m.source, route: m.route, named: m.named}, src))
	}
	switch m.conv {
	case convPlain, convSlice, convMap:
		return assign(lang.Expr("%s.Value()", src))
	case convPointer:
		// opt.Option.Pointer has a pointer receiver, which the result of
		// Flatten does not provide.
		return lang.If(lang.Expr("%s.IsDefined()", src), genSteps{
			lang.Stmt("%s := %s.Value()", decl("o"), src),
			lang.Stmt("%s = &%s", dst, lang.Name("o")),
		}, nil)
	case convOption:
		return assign(lang.Expr("%s.Flatten()", src))
	case convCopy:
		return assign(src)
	case convTarget:
		// Value gives the zero partial value for undefined and null, which
		// converts back to the zero value.
		return assign(lang.Expr("%s.Value().ToValue()", src))
	case convTargetPointer:
		return call(opToTarget)
	case convTargetRef:
		return call(opToTargetRef)
	case convEmbedded:
		return assign(lang.Expr("%s.ToValue()", src))
	case convEmbeddedPointer:
		return call(opToEmbedded)
	}
	return lang.Fail(unknownConv(m.conv))
}

// unknownConv returns the error for the unknown conversion c.
func unknownConv(c conv) error {
	return fmt.Errorf("partial: unknown conversion %d", c)
}

// conditional reports whether the statements of to for c leave their
// destination unset for some values.
func conditional(c conv) bool { return c == convPointer }

// merge returns the statement merging the field r onto the field l.
func (w *methodWriter) merge(f fieldInfo, l, r genExpr) genStep {
	overwrite := lang.If(lang.Expr("!%s.IsUndefined()", r), lang.Stmt("%s = %s", l, r), nil)
	assign := func(rhs genExpr) genStep { return lang.Stmt("%s = %s", l, rhs) }
	switch {
	case f.class == classEmbedded && f.strategy == string(MergeNestedRecursive):
		return assign(lang.Expr("%s.Merge(%s)", l, r))
	case f.class == classEmbedded:
		// An embedded partial has no undefined state of its own.
		test := w.p.call(&helper{op: opAllUndefined, typ: f.named, named: f.named}, r)
		return lang.If(lang.Expr("!%s", test), lang.Stmt("%s = %s", l, r), nil)
	case f.class == classElastic:
		return overwrite
	case f.strategy == string(MergeAppend) && (f.class == classSubSlice || f.class == classSubMap):
		return assign(w.p.call(
			&helper{op: opMergeAppend, typ: f.named, named: f.named, partial: true}, l, r,
		))
	case f.strategy == string(MergeAppend):
		return assign(w.p.call(&helper{op: opMergeAppend, typ: f.leaf}, l, r))
	case f.strategy == string(MergeNestedRecursive) && f.conv == convTargetRef:
		return assign(w.p.call(&helper{
			op: opMergeRecursive, typ: f.source, route: f.route, named: f.named, partial: true,
		}, l, r))
	case f.strategy == string(MergeNestedRecursive):
		return assign(w.p.call(
			&helper{op: opMergeRecursive, typ: f.named, named: f.named, partial: true}, l, r,
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

// fieldSteps returns the step each field of fields not skipped gets.
func fieldSteps(fields []fieldInfo, step func(f fieldInfo) genStep) genSteps {
	return slices.Collect(opiter.WrapSeq(slices.Values(fields)).Filter(notSkipped).Map(step).Iter())
}

func notSkipped(f fieldInfo) bool { return f.conv != convSkip }

// fieldOf returns the field sel of the value role refers to.
func fieldOf(role, sel string) genExpr {
	return lang.Expr("%s.%s", lang.Name(role), sel)
}

// structMethods writes the methods of a struct partial type.
func (w *methodWriter) structMethods(fields []fieldInfo) error {
	err := w.function(
		docComment("FromValue sets every field of %s to the partial form of the field of %s.\n"+
			"A nil pointer, slice or map and a none option become null.",
			lang.Name("p"), lang.Name("v")),
		lang.Expr("func (%s *%s) FromValue(%s %s)", decl("p"), w.recv, decl("v"), w.source),
		fieldSteps(fields, func(f fieldInfo) genStep {
			return w.from(f.mapped, fieldOf("p", f.partial), fieldOf("v", f.name))
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
		fieldSteps(fields, func(f fieldInfo) genStep {
			return w.to(f.mapped, fieldOf("v", f.name), fieldOf("p", f.partial))
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
		fieldSteps(fields, func(f fieldInfo) genStep {
			return w.merge(f, fieldOf("p", f.partial), fieldOf("r", f.partial))
		}),
		lang.Stmt("return %s", lang.Name("p")),
	)
	if err != nil {
		return err
	}

	if err := w.applyTo(); err != nil {
		return err
	}
	if err := w.fieldMaskMethods(fields); err != nil {
		return err
	}
	return w.flagMethods(fields)
}

// collectionMethods writes the methods of a slice or map partial type,
// whose elements map as elem from the source element type source.
func (w *methodWriter) collectionMethods(isMap bool, elem mapped, source types.Type) error {
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
		if !isMap || !back || !conditional(elem.conv) {
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

	elemType, err := w.p.typeString(source)
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

// convFunc returns the statements setting dst to the conversion of src
// that m describes, as from and to do.
type convFunc func(m mapped, dst, src genExpr) genStep

// decls parses the methods written.
func (w *methodWriter) decls() ([]dst.Decl, error) {
	decls, err := gofragment.ParseDstDecls([]byte(w.b.String()))
	if err != nil {
		return nil, fmt.Errorf("partial: parse methods of %s: %w", w.recv, err)
	}
	for _, d := range decls {
		d.Decorations().Before = dst.EmptyLine
	}
	return decls, nil
}
