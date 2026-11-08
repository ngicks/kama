package partial

import (
	"bytes"
	"fmt"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"

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

// locals maps each of localNames to an identifier that shadows neither a
// type parameter named in tparams nor a package-level identifier of scope,
// both of which the methods and helpers may spell.
func locals(scope *types.Scope, tparams []string) map[string]string {
	taken := map[string]bool{}
	for _, name := range scope.Names() {
		taken[name] = true
	}
	for _, name := range tparams {
		taken[name] = true
	}
	out := map[string]string{}
	for _, name := range localNames {
		ident := name
		for taken[ident] {
			ident += "_"
		}
		out[name] = ident
	}
	return out
}

// methodTypeParams returns the names the methods spell the type parameters
// of tparams by. A named parameter keeps its name. A blank one, which the
// methods cannot spell, gets a name taken by no package-level identifier
// of scope, no other type parameter, no local and no predeclared
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
	for _, l := range locals(scope, tparamNames(tparams)) {
		taken[l] = true
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
	// l maps localNames to the identifiers used.
	l map[string]string
	// recv and source spell the partial type and the source type with the
	// type parameters of the source.
	recv, source string
}

func (p *mapper) newMethodWriter(n *typegraph.Node) *methodWriter {
	named := n.Detail.Type
	var targs string
	if named.TypeParams().Len() > 0 {
		names := methodTypeParams(p.pkg.Types.Scope(), named.TypeParams())
		targs = "[" + strings.Join(names, ", ") + "]"
	}
	return &methodWriter{
		p:      p,
		l:      locals(p.pkg.Types.Scope(), tparamNames(named.TypeParams())),
		recv:   partialName(named.Obj().Name()) + targs,
		source: named.Obj().Name() + targs,
	}
}

func (w *methodWriter) line(format string, args ...any) {
	fmt.Fprintf(&w.b, format+"\n", args...)
}

// tparamNames returns the names of the type parameters of list.
func tparamNames(list *types.TypeParamList) []string {
	out := make([]string, 0, list.Len())
	for tp := range list.TypeParams() {
		out = append(out, tp.Obj().Name())
	}
	return out
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
		o := w.l["o"]
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
	p, v, base := w.l["p"], w.l["v"], w.l["base"]
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
	p, v, r := w.l["p"], w.l["v"], w.l["r"]

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
	p, v, r, e, x, o := w.l["p"], w.l["v"], w.l["r"], w.l["e"], w.l["x"], w.l["o"]
	idx := w.l["i"]
	if isMap {
		idx = w.l["k"]
	}

	// A map element missing from the map is not the zero value, so a
	// conversion that may leave its destination unset goes through a local.
	// back reports whether stmt converts back to source values, as to does,
	// whose conditional statements need a zero destination. typ is the
	// element type of dst then.
	elemLoop := func(
		dst, src string, stmt func(m mapped, dst, src string) (string, error), back bool, typ string,
	) error {
		w.line("for %s, %s := range %s {", idx, e, src)
		target := dst + "[" + idx + "]"
		if isMap && back && conditional(elem.conv) {
			w.line("var %s %s", x, typ)
			target = x
		}
		s, err := stmt(elem, target, e)
		if err != nil {
			return err
		}
		w.line("%s", s)
		if target == x {
			w.line("%s[%s] = %s", dst, idx, x)
		}
		w.line("}")
		return nil
	}

	w.doc("FromValue", "sets p to the partial forms of the elements of v. A nil v\n"+
		"// gives a nil p.")
	w.line("func (%s *%s) FromValue(%s %s) {", p, w.recv, v, w.source)
	w.line("if %s == nil {\n*%s = nil\nreturn\n}", v, p)
	w.line("*%s = make(%s, len(%s))", p, w.recv, v)
	if err := elemLoop("(*"+p+")", v, w.from, false, ""); err != nil {
		return err
	}
	w.line("}")

	elemType, err := w.p.typeString(source)
	if err != nil {
		return err
	}
	w.doc("ToValue", "returns the value p describes. An undefined or null element\n"+
		"// gives the zero value of the element, and a nil p gives nil.")
	w.line("func (%s %s) ToValue() %s {", p, w.recv, w.source)
	w.line("if %s == nil {\nreturn nil\n}", p)
	w.line("%s := make(%s, len(%s))", v, w.source, p)
	if err := elemLoop(v, p, w.to, true, elemType); err != nil {
		return err
	}
	w.line("return %s", v)
	w.line("}")

	text := "returns r, or p when r is nil."
	if w.p.sel.opts.Merge == MergeAppend {
		text = "returns the elements of p then r in a new slice. A nil\n" +
			"// p or r gives the other."
		if isMap {
			text = "returns the entries of p with the entries of r set in a\n" +
				"// new map. A nil p or r gives the other."
		}
	}
	w.doc("Merge", text+" Merge writes to neither p nor r.")
	w.line("func (%s %s) Merge(%s %s) %s {", p, w.recv, r, w.recv, w.recv)
	w.line("if %s == nil {\nreturn %s\n}", r, p)
	switch {
	case w.p.sel.opts.Merge != MergeAppend:
		w.line("return %s", r)
	case isMap:
		mp, err := w.p.ident(mapsPath)
		if err != nil {
			return err
		}
		w.line("if %s == nil {\nreturn %s\n}", p, r)
		w.line("%s := make(%s, len(%s)+len(%s))", o, w.recv, p, r)
		w.line("%s.Copy(%s, %s)", mp, o, p)
		w.line("%s.Copy(%s, %s)", mp, o, r)
		w.line("return %s", o)
	default:
		w.line("if %s == nil {\nreturn %s\n}", p, r)
		w.line("%s := make(%s, 0, len(%s)+len(%s))", o, w.recv, p, r)
		w.line("%s = append(%s, %s...)", o, o, p)
		w.line("return append(%s, %s...)", o, r)
	}
	w.line("}")

	w.applyTo()
	return nil
}

// decls parses the methods written.
func (w *methodWriter) decls(pkgName string) ([]dst.Decl, error) {
	f, err := decorator.Parse("package " + pkgName + "\n" + w.b.String())
	if err != nil {
		return nil, fmt.Errorf("partial: parse methods of %s: %w", w.recv, err)
	}
	for _, d := range f.Decls {
		d.Decorations().Before = dst.EmptyLine
	}
	return f.Decls, nil
}

// exprString prints the type expression e.
func exprString(e dst.Expr) (string, error) {
	f := &dst.File{
		Name: dst.NewIdent("x"),
		Decls: []dst.Decl{&dst.GenDecl{
			Tok: token.TYPE,
			Specs: []dst.Spec{
				&dst.TypeSpec{Name: dst.NewIdent("_"), Type: dst.Clone(e).(dst.Expr)},
			},
		}},
	}
	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, f); err != nil {
		return "", err
	}
	_, s, ok := strings.Cut(buf.String(), "type _ ")
	if !ok {
		return "", fmt.Errorf("partial: print %T", e)
	}
	return strings.TrimSpace(s), nil
}
