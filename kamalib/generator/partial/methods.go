package partial

import (
	"fmt"
	"go/types"
	"slices"
	"strings"

	"github.com/dave/dst"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
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
	if s.flagged[n.Id] {
		return slices.Concat(methodNames, flagMethodNames)
	}
	return methodNames
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
		tparams: tparams,
		recv:    partialName(named.Obj().Name()) + targs,
		source:  named.Obj().Name() + targs,
	}
}

// function writes a method documented by doc. signature declares its
// receiver and parameters in the scope of the method, where body renders.
func (w *methodWriter) function(doc, signature genExpr, body ...genStep) error {
	s, err := renderFunc(w.p.funcContext(w.tparams), doc, signature, body...)
	if err != nil {
		return err
	}
	w.b.WriteString(s)
	return nil
}

// from returns the statement setting dst to the partial form of the source
// value src.
func (w *methodWriter) from(m mapped, dst, src genExpr) genStep {
	assign := func(rhs genExpr) genStep { return stmt("%s = %s", dst, rhs) }
	und := func(genCtx) (string, error) { return w.p.ident(undPath) }
	call := func(op helperOp) genStep {
		return assign(w.p.call(&helper{op: op, typ: m.source, route: m.route, named: m.named}, src))
	}
	switch m.conv {
	case convPlain:
		return assign(expr("%s.Defined(%s)", und, src))
	case convSlice:
		return assign(expr("%s.WrapSlice(%s).CollapseToNull()", und, src))
	case convMap:
		return assign(expr("%s.WrapMap(%s).CollapseToNull()", und, src))
	case convPointer:
		return assign(expr("%s.FromPointer(%s).CollapseToNull()", und, src))
	case convOption:
		return assign(expr("%s.FromOption(%s, true)", und, src))
	case convCopy:
		return assign(src)
	case convTarget, convTargetPointer:
		return call(opFromTarget)
	case convTargetRef:
		return call(opFromTargetRef)
	case convEmbedded:
		return stmt("%s.FromValue(%s)", dst, src)
	case convEmbeddedPointer:
		return call(opFromEmbedded)
	}
	return unknownConv(m.conv)
}

// to returns the statements setting dst to the value the partial value src
// describes. A conditional conversion leaves dst as it is when src is
// undefined or null, so dst must hold the zero value.
func (w *methodWriter) to(m mapped, dst, src genExpr) genStep {
	assign := func(rhs genExpr) genStep { return stmt("%s = %s", dst, rhs) }
	call := func(op helperOp) genStep {
		return assign(w.p.call(&helper{op: op, typ: m.source, route: m.route, named: m.named}, src))
	}
	switch m.conv {
	case convPlain, convSlice, convMap:
		return assign(expr("%s.Value()", src))
	case convPointer:
		// opt.Option.Pointer has a pointer receiver, which the result of
		// Flatten does not provide.
		return genhelper.If(expr("%s.IsDefined()", src), genSteps{
			stmt("%s := %s.Value()", decl("o"), src),
			stmt("%s = &%s", dst, name("o")),
		}, nil)
	case convOption:
		return assign(expr("%s.Flatten()", src))
	case convCopy:
		return assign(src)
	case convTarget:
		// Value gives the zero partial value for undefined and null, which
		// converts back to the zero value.
		return assign(expr("%s.Value().ToValue()", src))
	case convTargetPointer:
		return call(opToTarget)
	case convTargetRef:
		return call(opToTargetRef)
	case convEmbedded:
		return assign(expr("%s.ToValue()", src))
	case convEmbeddedPointer:
		return call(opToEmbedded)
	}
	return unknownConv(m.conv)
}

// unknownConv is the step failing for the unknown conversion c.
func unknownConv(c conv) genStep {
	return genhelper.FuncStep[genCtx](func(ctx genCtx, _ []byte) (genCtx, []byte, error) {
		return ctx, nil, fmt.Errorf("partial: unknown conversion %d", c)
	})
}

// conditional reports whether the statements of to for c leave their
// destination unset for some values.
func conditional(c conv) bool { return c == convPointer }

// merge returns the statement merging the field r onto the field l.
func (w *methodWriter) merge(f fieldInfo, l, r genExpr) genStep {
	overwrite := genhelper.If(expr("!%s.IsUndefined()", r), stmt("%s = %s", l, r), nil)
	assign := func(rhs genExpr) genStep { return stmt("%s = %s", l, rhs) }
	switch {
	case f.class == classEmbedded && f.strategy == string(MergeNestedRecursive):
		return assign(expr("%s.Merge(%s)", l, r))
	case f.class == classEmbedded:
		// An embedded partial has no undefined state of its own.
		test := w.p.call(&helper{op: opAllUndefined, typ: f.named, named: f.named}, r)
		return genhelper.If(expr("!%s", test), stmt("%s = %s", l, r), nil)
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
		docComment("ApplyTo returns v with the change p describes applied. It is\n"+
			"FromValue of v merged with p, converted back with ToValue."),
		expr("func (%s %s) ApplyTo(%s %s) %s", decl("p"), w.recv, decl("v"), w.source, w.source),
		stmt("var %s %s", decl("base"), w.recv),
		stmt("%s.FromValue(%s)", name("base"), name("v")),
		stmt("return %s.Merge(%s).ToValue()", name("base"), name("p")),
	)
}

// fieldSteps returns the step each field of fields not skipped gets.
func fieldSteps(fields []fieldInfo, step func(f fieldInfo) genStep) genSteps {
	var out genSteps
	for _, f := range fields {
		if f.conv != convSkip {
			out = append(out, step(f))
		}
	}
	return out
}

// fieldOf returns the field sel of the value role refers to.
func fieldOf(role, sel string) genExpr {
	return expr("%s.%s", name(role), sel)
}

// structMethods writes the methods of a struct partial type.
func (w *methodWriter) structMethods(fields []fieldInfo) error {
	err := w.function(
		docComment("FromValue sets every field of p to the partial form of the field of v.\n"+
			"A nil pointer, slice or map and a none option become null."),
		expr("func (%s *%s) FromValue(%s %s)", decl("p"), w.recv, decl("v"), w.source),
		fieldSteps(fields, func(f fieldInfo) genStep {
			return w.from(f.mapped, fieldOf("p", f.partial), fieldOf("v", f.name))
		}),
	)
	if err != nil {
		return err
	}

	err = w.function(
		docComment("ToValue returns the value p describes. An undefined or null field\n"+
			"gives the zero value of the field."),
		expr("func (%s %s) ToValue() %s", decl("p"), w.recv, w.source),
		stmt("var %s %s", decl("v"), w.source),
		fieldSteps(fields, func(f fieldInfo) genStep {
			return w.to(f.mapped, fieldOf("v", f.name), fieldOf("p", f.partial))
		}),
		stmt("return %s", name("v")),
	)
	if err != nil {
		return err
	}

	err = w.function(
		docComment("Merge returns p with the fields of r merged onto it. An undefined\n"+
			"field of r keeps the field of p. Merge writes to neither p nor r."),
		expr("func (%s %s) Merge(%s %s) %s", decl("p"), w.recv, decl("r"), w.recv, w.recv),
		fieldSteps(fields, func(f fieldInfo) genStep {
			return w.merge(f, fieldOf("p", f.partial), fieldOf("r", f.partial))
		}),
		stmt("return %s", name("p")),
	)
	if err != nil {
		return err
	}

	if err := w.applyTo(); err != nil {
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
			return genhelper.ForRange(idx, "e", src,
				conv(elem, expr("%s[%s]", dst, name(idx)), name("e")),
			)
		}
		return genhelper.ForRange(idx, "e", src, genSteps{
			stmt("var %s %s", decl("x"), typ),
			conv(elem, name("x"), name("e")),
			stmt("%s[%s] = %s", dst, name(idx), name("x")),
		})
	}

	err := w.function(
		docComment("FromValue sets p to the partial forms of the elements of v. A nil v\n"+
			"gives a nil p."),
		expr("func (%s *%s) FromValue(%s %s)", decl("p"), w.recv, decl("v"), w.source),
		genhelper.If(expr("%s == nil", name("v")), genSteps{
			stmt("*%s = nil", name("p")),
			stmt("return"),
		}, nil),
		stmt("*%s = make(%s, len(%s))", name("p"), w.recv, name("v")),
		elemLoop(expr("(*%s)", name("p")), name("v"), w.from, false, ""),
	)
	if err != nil {
		return err
	}

	elemType, err := w.p.typeString(source)
	if err != nil {
		return err
	}
	err = w.function(
		docComment("ToValue returns the value p describes. An undefined or null element\n"+
			"gives the zero value of the element, and a nil p gives nil."),
		expr("func (%s %s) ToValue() %s", decl("p"), w.recv, w.source),
		genhelper.If(expr("%s == nil", name("p")), stmt("return nil"), nil),
		stmt("%s := make(%s, len(%s))", decl("v"), w.source, name("p")),
		elemLoop(name("v"), name("p"), w.to, true, elemType),
		stmt("return %s", name("v")),
	)
	if err != nil {
		return err
	}

	mergeDoc := "returns r, or p when r is nil."
	if w.p.sel.opts.Merge == MergeAppend {
		mergeDoc = "returns the elements of p then r in a new slice. A nil\n" +
			"p or r gives the other."
		if isMap {
			mergeDoc = "returns the entries of p with the entries of r set in a\n" +
				"new map. A nil p or r gives the other."
		}
	}
	merge := genSteps{
		genhelper.If(expr("%s == nil", name("r")), stmt("return %s", name("p")), nil),
	}
	switch {
	case w.p.sel.opts.Merge != MergeAppend:
		merge = append(merge, stmt("return %s", name("r")))
	case isMap:
		mp, err := w.p.ident(mapsPath)
		if err != nil {
			return err
		}
		merge = append(merge,
			genhelper.If(expr("%s == nil", name("p")), stmt("return %s", name("r")), nil),
			stmt("%s := make(%s, len(%s)+len(%s))", decl("o"), w.recv, name("p"), name("r")),
			stmt("%s.Copy(%s, %s)", mp, name("o"), name("p")),
			stmt("%s.Copy(%s, %s)", mp, name("o"), name("r")),
			stmt("return %s", name("o")),
		)
	default:
		merge = append(merge,
			genhelper.If(expr("%s == nil", name("p")), stmt("return %s", name("r")), nil),
			stmt("%s := make(%s, 0, len(%s)+len(%s))", decl("o"), w.recv, name("p"), name("r")),
			stmt("%s = append(%s, %s...)", name("o"), name("o"), name("p")),
			stmt("return append(%s, %s...)", name("o"), name("r")),
		)
	}
	err = w.function(
		docComment("Merge %s Merge writes to neither p nor r.", mergeDoc),
		expr("func (%s %s) Merge(%s %s) %s", decl("p"), w.recv, decl("r"), w.recv, w.recv),
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
	decls, err := genhelper.ParseDstDecls([]byte(w.b.String()))
	if err != nil {
		return nil, fmt.Errorf("partial: parse methods of %s: %w", w.recv, err)
	}
	for _, d := range decls {
		d.Decorations().Before = dst.EmptyLine
	}
	return decls, nil
}
