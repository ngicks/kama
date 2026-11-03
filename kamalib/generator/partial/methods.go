package partial

import (
	"bytes"
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// methodNames are the methods every partial type gets.
var methodNames = []string{"FromValue", "ToValue", "Merge", "ApplyTo"}

// localNames are the identifiers the generated methods declare.
var localNames = []string{"p", "v", "r", "base", "i", "k", "e", "x"}

// locals maps each of localNames to an identifier that shadows neither a
// type parameter of the receiver nor a package-level identifier of scope,
// both of which the methods may spell.
func locals(scope *types.Scope, tparams *types.TypeParamList) map[string]string {
	taken := map[string]bool{}
	for _, name := range scope.Names() {
		taken[name] = true
	}
	for tp := range tparams.TypeParams() {
		taken[tp.Obj().Name()] = true
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
		var names []string
		for tp := range named.TypeParams().TypeParams() {
			names = append(names, tp.Obj().Name())
		}
		targs = "[" + strings.Join(names, ", ") + "]"
	}
	return &methodWriter{
		p:      p,
		l:      locals(p.pkg.Types.Scope(), named.TypeParams()),
		recv:   partialName(named.Obj().Name()) + targs,
		source: named.Obj().Name() + targs,
	}
}

func (w *methodWriter) line(format string, args ...any) {
	fmt.Fprintf(&w.b, format+"\n", args...)
}

// call writes a call of the helper name and records it as used.
func (w *methodWriter) call(name string, args ...string) string {
	w.p.helpers[name] = true
	return name + "(" + strings.Join(args, ", ") + ")"
}

func (w *methodWriter) doc(name, text string) {
	w.line("\n// %s %s\n//\n%s", name, text, genmark.Marker(GeneratorName))
}

// from returns the statement setting the partial value lhs to the
// partial form of the source value src. lhs is addressable.
func (w *methodWriter) from(m mapped, lhs, src string) string {
	switch c := m.conv; {
	case c == convTarget && m.class == classSubSlice:
		return w.call("kamaPartialFromSliceTarget", "&"+lhs, src)
	case c == convTarget && m.class == classSubMap:
		return w.call("kamaPartialFromMapTarget", "&"+lhs, src)
	}
	switch m.conv {
	case convPlain:
		return lhs + " = " + w.call("kamaPartialFromValue", src)
	case convSlice:
		return lhs + " = " + w.call("kamaPartialFromSlice", src)
	case convMap:
		return lhs + " = " + w.call("kamaPartialFromMap", src)
	case convPointer:
		return lhs + " = " + w.call("kamaPartialFromPointer", src)
	case convOption:
		return lhs + " = " + w.call("kamaPartialFromOption", src)
	case convCopy:
		return lhs + " = " + src
	case convTarget:
		return w.call("kamaPartialFromTarget", "&"+lhs, src)
	case convTargetPointer:
		return w.call("kamaPartialFromTargetPointer", "&"+lhs, src)
	case convTargetRef:
		return w.call("kamaPartialFromTargetRef", "&"+lhs, src)
	case convEmbedded:
		return lhs + ".FromValue(" + src + ")"
	case convEmbeddedPointer:
		return w.call("kamaPartialFromEmbeddedPointer", "&"+lhs, src)
	}
	return ""
}

// to returns the statement setting the source value lhs to the value the
// partial value src describes. lhs is addressable.
func (w *methodWriter) to(m mapped, lhs, src string) string {
	switch m.conv {
	case convPlain, convSlice, convMap:
		return lhs + " = " + w.call("kamaPartialToValue", src)
	case convPointer:
		return lhs + " = " + w.call("kamaPartialToPointer", src)
	case convOption:
		return lhs + " = " + w.call("kamaPartialToOption", src)
	case convCopy:
		return lhs + " = " + src
	case convTarget:
		return w.call("kamaPartialToTarget", "&"+lhs, src)
	case convTargetPointer:
		return w.call("kamaPartialToTargetPointer", "&"+lhs, src)
	case convTargetRef:
		return w.call("kamaPartialToTargetRef", "&"+lhs, src)
	case convEmbedded:
		return lhs + " = " + src + ".ToValue()"
	case convEmbeddedPointer:
		return w.call("kamaPartialToEmbeddedPointer", "&"+lhs, src)
	}
	return ""
}

// addressed reports whether the statements of c take the address of
// their destination.
func addressed(c conv) bool {
	switch c {
	case convTarget, convTargetPointer, convTargetRef, convEmbeddedPointer:
		return true
	}
	return false
}

// merge returns the expression merging the field values l and r.
func (w *methodWriter) merge(f fieldInfo, l, r string) string {
	switch {
	case f.class == classEmbedded && f.strategy == string(MergeNestedRecursive):
		return l + ".Merge(" + r + ")"
	case f.class == classEmbedded:
		return r
	case f.class == classElastic:
		return w.call("kamaPartialMergeOverwriteElastic", l, r)
	case f.strategy == string(MergeAppend) && (f.class == classSlice || f.class == classSubSlice):
		return w.call("kamaPartialMergeAppendSlice", l, r)
	case f.strategy == string(MergeAppend):
		return w.call("kamaPartialMergeAppendMap", l, r)
	case f.strategy == string(MergeNestedRecursive) && f.conv == convTargetRef:
		return w.call("kamaPartialMergeRecursiveRef", l, r)
	case f.strategy == string(MergeNestedRecursive):
		return w.call("kamaPartialMergeRecursive", l, r)
	}
	return w.call("kamaPartialMergeOverwrite", l, r)
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
func (w *methodWriter) structMethods(fields []fieldInfo) {
	p, v, r := w.l["p"], w.l["v"], w.l["r"]

	w.doc("FromValue", "sets every field of p to the partial form of the field of v.\n"+
		"// A nil pointer, slice or map and a none option become null.")
	w.line("func (%s *%s) FromValue(%s %s) {", p, w.recv, v, w.source)
	for _, f := range fields {
		if f.conv != convSkip {
			w.line("%s", w.from(f.mapped, p+"."+f.partial, v+"."+f.name))
		}
	}
	w.line("}")

	w.doc("ToValue", "returns the value p describes. An undefined or null field\n"+
		"// gives the zero value of the field.")
	w.line("func (%s %s) ToValue() %s {", p, w.recv, w.source)
	w.line("var %s %s", v, w.source)
	for _, f := range fields {
		if f.conv != convSkip {
			w.line("%s", w.to(f.mapped, v+"."+f.name, p+"."+f.partial))
		}
	}
	w.line("return %s", v)
	w.line("}")

	w.doc("Merge", "returns p with the fields of r merged onto it. An undefined\n"+
		"// field of r keeps the field of p. Merge writes to neither p nor r.")
	w.line("func (%s %s) Merge(%s %s) %s {", p, w.recv, r, w.recv, w.recv)
	for _, f := range fields {
		if f.conv != convSkip {
			l := p + "." + f.partial
			w.line("%s = %s", l, w.merge(f, l, r+"."+f.partial))
		}
	}
	w.line("return %s", p)
	w.line("}")

	w.applyTo()
}

// collectionMethods writes the methods of a slice or map partial type,
// whose elements map as elem from the source element type source.
func (w *methodWriter) collectionMethods(isMap bool, elem mapped, source types.Type) error {
	p, v, r, i, k, e, x := w.l["p"], w.l["v"], w.l["r"], w.l["i"], w.l["k"], w.l["e"], w.l["x"]

	makeHelper, idx, overwrite, appendHelper := "kamaPartialMakeSlice", i,
		"kamaPartialOverwriteSlice", "kamaPartialAppendSlice"
	if isMap {
		makeHelper, idx, overwrite, appendHelper = "kamaPartialMakeMap", k,
			"kamaPartialOverwriteMap", "kamaPartialAppendMap"
	}

	// A map element is not addressable, so a destination whose address is
	// taken is a local copied in after.
	elemLoop := func(
		lhs, src string, stmt func(m mapped, lhs, src string) string, typ dst.Expr,
	) error {
		w.line("for %s, %s := range %s {", idx, e, src)
		if isMap && addressed(elem.conv) {
			s, err := exprString(typ)
			if err != nil {
				return err
			}
			w.line("var %s %s", x, s)
			w.line("%s", stmt(elem, x, e))
			w.line("%s[%s] = %s", lhs, idx, x)
		} else {
			w.line("%s", stmt(elem, lhs+"["+idx+"]", e))
		}
		w.line("}")
		return nil
	}

	w.doc("FromValue", "sets p to the partial forms of the elements of v. A nil v\n"+
		"// gives a nil p.")
	w.line("func (%s *%s) FromValue(%s %s) {", p, w.recv, v, w.source)
	w.line("%s", w.call(makeHelper, p, v))
	if err := elemLoop("(*"+p+")", v, w.from, elem.expr); err != nil {
		return err
	}
	w.line("}")

	sourceExpr, err := w.p.print(source)
	if err != nil {
		return err
	}
	w.doc("ToValue", "returns the value p describes. An undefined or null element\n"+
		"// gives the zero value of the element, and a nil p gives nil.")
	w.line("func (%s %s) ToValue() %s {", p, w.recv, w.source)
	w.line("var %s %s", v, w.source)
	w.line("%s", w.call(makeHelper, "&"+v, p))
	if err := elemLoop(v, p, w.to, sourceExpr); err != nil {
		return err
	}
	w.line("return %s", v)
	w.line("}")

	helper, text := overwrite, "returns r, or p when r is nil."
	if w.p.sel.opts.Merge == MergeAppend {
		helper, text = appendHelper, "returns the elements of p then r in a new slice. A nil\n"+
			"// p or r gives the other."
		if isMap {
			text = "returns the entries of p with the entries of r set in a\n" +
				"// new map. A nil p or r gives the other."
		}
	}
	w.doc("Merge", text+" Merge writes to neither p nor r.")
	w.line("func (%s %s) Merge(%s %s) %s {", p, w.recv, r, w.recv, w.recv)
	w.line("return %s", w.call(helper, p, r))
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
