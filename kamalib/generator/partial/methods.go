package partial

import (
	"fmt"
	"go/types"
	"slices"
	"strings"

	"github.com/dave/dst"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
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
	// scope is the scope of the method being written.
	scope *genhelper.Scope
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

func (w *methodWriter) line(format string, args ...any) {
	fmt.Fprintf(&w.b, format+"\n", args...)
}

// beginFunc starts the scope of the next method and returns the context
// positioned at it.
func (w *methodWriter) beginFunc() genCtx {
	c := w.p.funcContext(w.tparams)
	w.scope = c.Scope()
	return c
}

// local returns the name of role in the method being written, declaring
// it in the scope of the method on first use. The blocks of a method
// reuse the name.
func (w *methodWriter) local(role string) string {
	name, _ := w.scope.LookupOrDeclare(role)
	return name
}

// function writes the method name documented by doc, whose text body
// renders. body starts at the scope of the method, where its receiver and
// parameters are declared.
func (w *methodWriter) function(name, doc string, body ...genStep) error {
	c := w.beginFunc()
	w.doc(name, doc)
	_, b, err := genSteps(body).Render(c, nil)
	if err != nil {
		return err
	}
	w.b.Write(b)
	return nil
}

// call returns the call of h with args. See mapper.call.
func (w *methodWriter) call(h *helper, args ...string) (string, error) {
	return w.p.call(h, args...)
}

func (w *methodWriter) doc(name, text string) {
	w.line("\n// %s %s\n//\n%s", name, text, genmark.Marker(GeneratorName))
}

// from returns the statement setting dst to the partial form of the source
// value src.
func (w *methodWriter) from(m mapped, dst, src string) (string, error) {
	assign := func(expr string, err error) (string, error) { return dst + " = " + expr, err }
	und := func(format string) (string, error) {
		ident, err := w.p.ident(undPath)
		return assign(fmt.Sprintf(format, ident, src), err)
	}
	switch m.conv {
	case convPlain:
		return und("%s.Defined(%s)")
	case convSlice:
		return und("%s.WrapSlice(%s).CollapseToNull()")
	case convMap:
		return und("%s.WrapMap(%s).CollapseToNull()")
	case convPointer:
		return und("%s.FromPointer(%s).CollapseToNull()")
	case convOption:
		return und("%s.FromOption(%s, true)")
	case convCopy:
		return assign(src, nil)
	case convTarget, convTargetPointer:
		return assign(
			w.call(&helper{op: opFromTarget, typ: m.source, route: m.route, named: m.named}, src),
		)
	case convTargetRef:
		return assign(
			w.call(
				&helper{op: opFromTargetRef, typ: m.source, route: m.route, named: m.named},
				src,
			),
		)
	case convEmbedded:
		return dst + ".FromValue(" + src + ")", nil
	case convEmbeddedPointer:
		return assign(
			w.call(&helper{op: opFromEmbedded, typ: m.source, route: m.route, named: m.named}, src),
		)
	}
	return "", fmt.Errorf("partial: unknown conversion %d", m.conv)
}

// to returns the statements setting dst to the value the partial value src
// describes. A conditional conversion leaves dst as it is when src is
// undefined or null, so dst must hold the zero value.
func (w *methodWriter) to(m mapped, dst, src string) (string, error) {
	assign := func(expr string, err error) (string, error) { return dst + " = " + expr, err }
	switch m.conv {
	case convPlain, convSlice, convMap:
		return assign(src+".Value()", nil)
	case convPointer:
		// opt.Option.Pointer has a pointer receiver, which the result of
		// Flatten does not provide.
		o := w.local("o")
		return "if " + src + ".IsDefined() {\n" + o + " := " + src + ".Value()\n" +
			dst + " = &" + o + "\n}", nil
	case convOption:
		return assign(src+".Flatten()", nil)
	case convCopy:
		return assign(src, nil)
	case convTarget:
		// Value gives the zero partial value for undefined and null, which
		// converts back to the zero value.
		return assign(src+".Value().ToValue()", nil)
	case convTargetPointer:
		return assign(
			w.call(&helper{op: opToTarget, typ: m.source, route: m.route, named: m.named}, src),
		)
	case convTargetRef:
		return assign(
			w.call(&helper{op: opToTargetRef, typ: m.source, route: m.route, named: m.named}, src),
		)
	case convEmbedded:
		return assign(src+".ToValue()", nil)
	case convEmbeddedPointer:
		return assign(
			w.call(&helper{op: opToEmbedded, typ: m.source, route: m.route, named: m.named}, src),
		)
	}
	return "", fmt.Errorf("partial: unknown conversion %d", m.conv)
}

// conditional reports whether the statements of to for c leave their
// destination unset for some values.
func conditional(c conv) bool { return c == convPointer }

// merge returns the statement merging the field r onto the field l.
func (w *methodWriter) merge(f fieldInfo, l, r string) (string, error) {
	overwrite := "if !" + r + ".IsUndefined() {\n" + l + " = " + r + "\n}"
	assign := func(expr string, err error) (string, error) { return l + " = " + expr, err }
	switch {
	case f.class == classEmbedded && f.strategy == string(MergeNestedRecursive):
		return assign(l+".Merge("+r+")", nil)
	case f.class == classEmbedded:
		// An embedded partial has no undefined state of its own.
		test, err := w.call(&helper{op: opAllUndefined, typ: f.named, named: f.named}, r)
		return "if !" + test + " {\n" + l + " = " + r + "\n}", err
	case f.class == classElastic:
		return overwrite, nil
	case f.strategy == string(MergeAppend) && (f.class == classSubSlice || f.class == classSubMap):
		return assign(
			w.call(&helper{op: opMergeAppend, typ: f.named, named: f.named, partial: true}, l, r),
		)
	case f.strategy == string(MergeAppend):
		return assign(w.call(&helper{op: opMergeAppend, typ: f.leaf}, l, r))
	case f.strategy == string(MergeNestedRecursive) && f.conv == convTargetRef:
		return assign(w.call(&helper{
			op: opMergeRecursive, typ: f.source, route: f.route, named: f.named, partial: true,
		}, l, r))
	case f.strategy == string(MergeNestedRecursive):
		return assign(
			w.call(
				&helper{op: opMergeRecursive, typ: f.named, named: f.named, partial: true},
				l,
				r,
			),
		)
	}
	return overwrite, nil
}

func (w *methodWriter) applyTo() {
	w.beginFunc()
	p, v, base := w.local("p"), w.local("v"), w.local("base")
	w.doc("ApplyTo", "returns v with the change p describes applied. It is\n"+
		"// FromValue of v merged with p, converted back with ToValue.")
	w.line("func (%s %s) ApplyTo(%s %s) %s {", p, w.recv, v, w.source, w.source)
	w.line("var %s %s", base, w.recv)
	w.line("%s.FromValue(%s)", base, v)
	w.line("return %s.Merge(%s).ToValue()", base, p)
	w.line("}")
}

// structMethods writes the methods of a struct partial type.
func (w *methodWriter) structMethods(fields []fieldInfo) error {
	w.beginFunc()
	p, v := w.local("p"), w.local("v")
	w.doc("FromValue", "sets every field of p to the partial form of the field of v.\n"+
		"// A nil pointer, slice or map and a none option become null.")
	w.line("func (%s *%s) FromValue(%s %s) {", p, w.recv, v, w.source)
	for _, f := range fields {
		if f.conv == convSkip {
			continue
		}
		stmt, err := w.from(f.mapped, p+"."+f.partial, v+"."+f.name)
		if err != nil {
			return err
		}
		w.line("%s", stmt)
	}
	w.line("}")

	w.beginFunc()
	p, v = w.local("p"), w.local("v")
	w.doc("ToValue", "returns the value p describes. An undefined or null field\n"+
		"// gives the zero value of the field.")
	w.line("func (%s %s) ToValue() %s {", p, w.recv, w.source)
	w.line("var %s %s", v, w.source)
	for _, f := range fields {
		if f.conv == convSkip {
			continue
		}
		stmt, err := w.to(f.mapped, v+"."+f.name, p+"."+f.partial)
		if err != nil {
			return err
		}
		w.line("%s", stmt)
	}
	w.line("return %s", v)
	w.line("}")

	w.beginFunc()
	p, r := w.local("p"), w.local("r")
	w.doc("Merge", "returns p with the fields of r merged onto it. An undefined\n"+
		"// field of r keeps the field of p. Merge writes to neither p nor r.")
	w.line("func (%s %s) Merge(%s %s) %s {", p, w.recv, r, w.recv, w.recv)
	for _, f := range fields {
		if f.conv == convSkip {
			continue
		}
		stmt, err := w.merge(f, p+"."+f.partial, r+"."+f.partial)
		if err != nil {
			return err
		}
		w.line("%s", stmt)
	}
	w.line("return %s", p)
	w.line("}")

	w.applyTo()
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
				convStmt(conv, elem, expr("%s[%s]", dst, ref(idx)), expr("%s", ref("e"))),
			)
		}
		return genhelper.ForRange(idx, "e", src, genSteps{
			stmt("var %s %s", decl("x"), typ),
			convStmt(conv, elem, expr("%s", ref("x")), expr("%s", ref("e"))),
			stmt("%s[%s] = %s", dst, ref(idx), ref("x")),
		})
	}

	err := w.function("FromValue", "sets p to the partial forms of the elements of v. A nil v\n"+
		"// gives a nil p.",
		stmt("func (%s *%s) FromValue(%s %s) {", decl("p"), w.recv, decl("v"), w.source),
		genhelper.If(expr("%s == nil", ref("v")), genSteps{
			stmt("*%s = nil", ref("p")),
			stmt("return"),
		}, nil),
		stmt("*%s = make(%s, len(%s))", ref("p"), w.recv, ref("v")),
		elemLoop(expr("(*%s)", ref("p")), expr("%s", ref("v")), w.from, false, ""),
		stmt("}"),
	)
	if err != nil {
		return err
	}

	elemType, err := w.p.typeString(source)
	if err != nil {
		return err
	}
	err = w.function("ToValue", "returns the value p describes. An undefined or null element\n"+
		"// gives the zero value of the element, and a nil p gives nil.",
		stmt("func (%s %s) ToValue() %s {", decl("p"), w.recv, w.source),
		genhelper.If(expr("%s == nil", ref("p")), stmt("return nil"), nil),
		stmt("%s := make(%s, len(%s))", decl("v"), w.source, ref("p")),
		elemLoop(expr("%s", ref("v")), expr("%s", ref("p")), w.to, true, elemType),
		stmt("return %s", ref("v")),
		stmt("}"),
	)
	if err != nil {
		return err
	}

	mergeDoc := "returns r, or p when r is nil."
	if w.p.sel.opts.Merge == MergeAppend {
		mergeDoc = "returns the elements of p then r in a new slice. A nil\n" +
			"// p or r gives the other."
		if isMap {
			mergeDoc = "returns the entries of p with the entries of r set in a\n" +
				"// new map. A nil p or r gives the other."
		}
	}
	merge := genSteps{
		stmt("func (%s %s) Merge(%s %s) %s {", decl("p"), w.recv, decl("r"), w.recv, w.recv),
		genhelper.If(expr("%s == nil", ref("r")), stmt("return %s", ref("p")), nil),
	}
	switch {
	case w.p.sel.opts.Merge != MergeAppend:
		merge = append(merge, stmt("return %s", ref("r")))
	case isMap:
		mp, err := w.p.ident(mapsPath)
		if err != nil {
			return err
		}
		merge = append(merge,
			genhelper.If(expr("%s == nil", ref("p")), stmt("return %s", ref("r")), nil),
			stmt("%s := make(%s, len(%s)+len(%s))", decl("o"), w.recv, ref("p"), ref("r")),
			stmt("%s.Copy(%s, %s)", mp, ref("o"), ref("p")),
			stmt("%s.Copy(%s, %s)", mp, ref("o"), ref("r")),
			stmt("return %s", ref("o")),
		)
	default:
		merge = append(merge,
			genhelper.If(expr("%s == nil", ref("p")), stmt("return %s", ref("r")), nil),
			stmt("%s := make(%s, 0, len(%s)+len(%s))", decl("o"), w.recv, ref("p"), ref("r")),
			stmt("%s = append(%s, %s...)", ref("o"), ref("o"), ref("p")),
			stmt("return append(%s, %s...)", ref("o"), ref("r")),
		)
	}
	merge = append(merge, stmt("}"))
	if err := w.function(
		"Merge",
		mergeDoc+" Merge writes to neither p nor r.",
		merge...); err != nil {
		return err
	}

	w.applyTo()
	return nil
}

// convFunc returns the statement setting dst to the conversion of src
// that m describes, as from and to do.
type convFunc func(m mapped, dst, src string) (string, error)

// convStmt is a step appending the statement conv returns for m, with dst
// and src rendered in the scope of the step.
func convStmt(conv convFunc, m mapped, dst, src genExpr) genStep {
	return text(func(c genCtx) (string, error) {
		d, err := dst(c)
		if err != nil {
			return "", err
		}
		s, err := src(c)
		if err != nil {
			return "", err
		}
		return conv(m, d, s)
	})
}

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
