package partial

import (
	"go/types"
	"slices"
	"strconv"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// fieldMaskLocals are the identifiers the field mask methods declare
// besides localNames. The locals of each field holding child paths are
// named after the field and are not listed.
var fieldMaskLocals = []string{"paths", "path", "head", "rest", "nested", "y"}

// holdsMasked reports whether a field matched as m holds a partial value of
// a struct type with field masks. A path addresses such a field as a whole
// and through the paths of the fields of its value.
//
// The run knows no option of an implementor, so the methods of its partial
// variant tell whether it has field masks.
func (s *selection) holdsMasked(m match) bool {
	if m.embedded || m.kind != leafPartial || !isStruct(m.named) {
		return false
	}

	if m.implementor {
		return declaresFieldMask(m.named)
	}

	return s.fieldMasked(s.node(m.named).Id)
}

// fieldMaskMethods writes FieldMask and FromValueWithMask when the target
// has field masks enabled. It writes nothing otherwise.
func (w *methodWriter) fieldMaskMethods(st *types.Struct) error {
	if !w.p.sel.fieldMasked(w.n.Id) {
		return nil
	}

	err := w.function(
		docComment("FieldMask returns the sorted paths of the fields %s sets. It returns\n"+
			"nil when %s sets no field. An undefined field gives no path. A null or\n"+
			"defined field gives its own path. A defined field holding a partial\n"+
			"value with field masks gives the paths of the fields of that value\n"+
			"instead. The paths of an embedded partial value are paths of %s.",
			lang.Name("p"), lang.Name("p"), lang.Name("p")),
		lang.Expr("func (%s %s) FieldMask() []string", decl("p"), w.recv),
		w.fieldMaskBody(st),
	)
	if err != nil {
		return err
	}

	body, err := w.fromValueWithMaskBody(st)
	if err != nil {
		return err
	}

	return w.function(
		docComment("FromValueWithMask sets %s to the partial form of the fields of %s that\n"+
			"%s names. Every other field of %s becomes undefined. A path joins\n"+
			"the segments of nested fields with dots. A selected field gets what\n"+
			"FromValue gives it. A path below a field holding a partial value with\n"+
			"field masks selects a field of that value. Such a path gives null when\n"+
			"the field of %s is a nil pointer. A path naming a whole field covers\n"+
			"the paths below it. Empty %s give the zero partial value. An empty,\n"+
			"unknown or malformed path is an error. FromValueWithMask leaves %s\n"+
			"unchanged on error.",
			lang.Name("p"), lang.Name("v"), lang.Name("paths"), lang.Name("p"),
			lang.Name("v"), lang.Name("paths"), lang.Name("p")),
		lang.Expr("func (%s *%s) FromValueWithMask(%s %s, %s []string) error",
			decl("p"), w.recv, decl("v"), w.source, decl("paths")),
		body,
	)
}

// pkgIdent returns the identifier of the package at path, recorded as
// spelled when it renders.
func (w *methodWriter) pkgIdent(path string) genExpr {
	return func(genCtx) (string, error) { return w.p.ident(path) }
}

// fieldMaskBody returns the body of FieldMask for the fields of st.
func (w *methodWriter) fieldMaskBody(st *types.Struct) genSteps {
	appendPath := func(path genExpr) genStep {
		return lang.Stmt("%s = append(%s, %s)", lang.Name("paths"), lang.Name("paths"), path)
	}
	out := genSteps{lang.Stmt("var %s []string", decl("paths"))}

	for v := range st.Fields() {
		m := w.p.matchField(w.n, v)
		if m.blank {
			continue
		}

		seg := w.p.sel.maskSegment(w.n, v)
		field := fieldOf("p", partialField(v, m))
		own := appendPath(lang.Expr("%s", strconv.Quote(seg)))

		children := lang.ForRange("", "path", lang.Expr("%s.Value().FieldMask()", field),
			appendPath(lang.Expr("%s+%s", strconv.Quote(seg+"."), lang.Name("path"))))

		switch {
		case m.embedded:
			out = append(out,
				lang.Stmt("%s = append(%s, %s.FieldMask()...)",
					lang.Name("paths"), lang.Name("paths"), field))
		case w.p.sel.holdsMasked(m) && m.ref:
			// A defined nil pointer converts back to nil as null does.
			out = append(
				out,
				lang.If(
					lang.Expr("!%s.IsUndefined() && %s.Value() == nil", field, field),
					own,
					nil,
				),
				lang.If(lang.Expr("%s.Value() != nil", field), children, nil),
			)
		case w.p.sel.holdsMasked(m):
			// Value gives the zero partial value for undefined and null, which
			// sets no field.
			out = append(out, lang.If(lang.Expr("%s.IsNull()", field), own, nil), children)
		default:
			out = append(out, lang.If(lang.Expr("!%s.IsUndefined()", field), own, nil))
		}
	}

	return append(out,
		lang.Stmt("%s.Sort(%s)", w.pkgIdent(slicesPath), lang.Name("paths")),
		lang.Stmt("return %s", lang.Name("paths")),
	)
}

// subRole and wholeRole name the locals collecting the child paths of the
// field named field and telling whether a path selects it whole.
func subRole(field string) string   { return "sub" + field }
func wholeRole(field string) string { return "whole" + field }

// fromValueWithMaskBody returns the body of FromValueWithMask for the
// fields of st.
//
// The loop routes each path by its first segment: a leaf converts at once,
// which repeats harmlessly for a duplicate path, and the child paths of a
// branch or an embedded partial value are collected for one call of its
// FromValueWithMask after the loop. That call runs even when a path
// selects the branch whole, so that a child path is validated whatever the
// order of the paths and whatever v holds.
func (w *methodWriter) fromValueWithMaskBody(st *types.Struct) (genSteps, error) {
	fmtIdent := w.pkgIdent(fmtPath)
	fail := func(format string, args ...genExpr) genStep {
		return lang.Stmt("return %s.Errorf(%s)", fmtIdent, lang.Join(", ",
			slices.Concat([]genExpr{lang.Expr("%s", strconv.Quote(format))}, args)...))
	}

	var (
		decls, after       genSteps
		cases              []genhelper.Case[genCtx]
		useRest, useNested bool
	)

	for v := range st.Fields() {
		m := w.p.matchField(w.n, v)
		if m.blank {
			continue
		}

		name := v.Name()
		sub, whole := subRole(name), wholeRole(name)
		seg := lang.Expr("%s", strconv.Quote(w.p.sel.maskSegment(w.n, v)))
		from := w.from(m, fieldOf("x", partialField(v, m)), fieldOf("v", name))

		switch {
		case m.embedded:
			var heads []genExpr

			// The segments of an embedded partial value are those of its
			// fields, which route to it.
			err := w.p.sel.walkMaskSegments(
				w.p.sel.node(m.named),
				func(_ *typegraph.Node, _ *types.Var, seg string) error {
					heads = append(heads, lang.Expr("%s", strconv.Quote(seg)))
					return nil
				},
			)
			if err != nil {
				return nil, err
			}

			if len(heads) == 0 {
				continue
			}

			decls = append(decls, lang.Stmt("var %s []string", decl(sub)))

			cases = append(cases, lang.Case(
				lang.Join(", ", heads...),
				lang.Stmt("%s = append(%s, %s)", lang.Name(sub), lang.Name(sub), lang.Name("path")),
			))

			build, err := w.maskEmbedded(v, m)
			if err != nil {
				return nil, err
			}

			after = append(after, lang.If(lang.Expr("%s != nil", lang.Name(sub)), build, nil))
		case w.p.sel.holdsMasked(m):
			useNested, useRest = true, true

			decls = append(decls,
				lang.Stmt("var %s []string", decl(sub)),
				lang.Stmt("var %s bool", decl(whole)),
			)
			cases = append(cases, lang.Case(seg, lang.Switch(nil,
				lang.Case(
					lang.Expr("!%s", lang.Name("nested")),
					lang.Stmt("%s = true", lang.Name(whole)),
				),
				lang.Case(
					lang.Expr(`%s == ""`, lang.Name("rest")),
					fail("%s: empty segment", lang.Name("path")),
				),
				lang.Default(
					lang.Stmt(
						"%s = append(%s, %s)",
						lang.Name(sub),
						lang.Name(sub),
						lang.Name("rest"),
					),
				),
			)))

			build, err := w.maskBranch(v, m, fmtIdent)
			if err != nil {
				return nil, err
			}

			after = append(after,
				lang.If(lang.Expr("%s != nil", lang.Name(sub)), build, nil),
				lang.If(lang.Name(whole), from, nil),
			)
		default:
			useNested = true

			cases = append(cases, lang.Case(seg, genSteps{
				lang.If(lang.Name("nested"),
					fail("%s: field %s has no subfields",
						lang.Name("path"), lang.Name("head")), nil),
				from,
			}))
		}
	}

	cases = append(cases,
		lang.Case(lang.Expr(`""`), genSteps{
			lang.If(lang.Expr(`%s == ""`, lang.Name("path")), fail("empty path"), nil),
			fail("%s: empty segment", lang.Name("path")),
		}),
		lang.Default(fail("%s: unknown field %s", lang.Name("path"), lang.Name("head"))),
	)

	var rest, nested any = "_", "_"
	if useRest {
		rest = decl("rest")
	}

	if useNested {
		nested = decl("nested")
	}

	out := genSteps{lang.Stmt("var %s %s", decl("x"), w.recv)}
	out = append(out, decls...)
	out = append(out,
		lang.ForRange("", "path", lang.Name("paths"), genSteps{
			lang.Stmt(`%s, %s, %s := %s.Cut(%s, ".")`, decl("head"), rest, nested,
				w.pkgIdent(stringsPath), lang.Name("path")),
			lang.Switch(lang.Name("head"), cases...),
		}),
	)
	out = append(out, after...)

	return append(out,
		lang.Stmt("*%s = %s", lang.Name("p"), lang.Name("x")),
		lang.Stmt("return nil"),
	), nil
}

// checkErr returns the steps calling call into err and returning wrap of
// err when it is not nil.
func checkErr(call, wrap genExpr) genSteps {
	return genSteps{
		lang.Stmt("%s := %s", decl("err"), call),
		lang.If(lang.Expr("%s != nil", lang.Name("err")), lang.Stmt("return %s", wrap), nil),
	}
}

// maskBranch returns the steps setting the partial field of the field v,
// matched as m, of x to the partial value its child paths select from v.
func (w *methodWriter) maskBranch(v *types.Var, m match, fmtIdent genExpr) (genStep, error) {
	part, err := w.p.partialString(m.named)
	if err != nil {
		return nil, err
	}

	und := w.pkgIdent(undPath)
	sub := lang.Name(subRole(v.Name()))
	src := fieldOf("v", v.Name())
	dst := fieldOf("x", partialField(v, m))

	call := func(in genExpr) genSteps {
		return checkErr(
			lang.Expr("%s.FromValueWithMask(%s, %s)", lang.Name("y"), in, sub),
			lang.Expr("%s.Errorf(%s, %s, %s)", fmtIdent, strconv.Quote("%s.%w"),
				strconv.Quote(w.p.sel.maskSegment(w.n, v)), lang.Name("err")),
		)
	}
	if !m.pointer() {
		return genSteps{
			lang.Stmt("var %s %s", decl("y"), part),
			call(src),
			lang.Stmt("%s = %s.Defined(%s)", dst, und, lang.Name("y")),
		}, nil
	}

	leaf, err := w.p.TypeString(m.leaf)
	if err != nil {
		return nil, err
	}

	y, held := lang.Stmt("var %s %s", decl("y"), part), part
	if m.ref {
		y, held = lang.Stmt("%s := new(%s)", decl("y"), part), "*"+part
	}
	// The paths still name fields of the type of the field, so they are
	// validated against its zero value.
	return genSteps{
		y,
		lang.If(lang.Expr("%s == nil", src),
			genSteps{
				call(lang.Expr("*new(%s)", leaf)),
				lang.Stmt("%s = %s.Null[%s]()", dst, und, held),
			},
			genSteps{
				call(lang.Expr("*%s", src)),
				lang.Stmt("%s = %s.Defined(%s)", dst, und, lang.Name("y")),
			},
		),
	}, nil
}

// maskEmbedded returns the steps setting the embedded partial value of the
// field v, matched as m, of x to the partial value the paths routed to it
// select from v.
func (w *methodWriter) maskEmbedded(v *types.Var, m match) (genStep, error) {
	sub := lang.Name(subRole(v.Name()))
	src := fieldOf("v", v.Name())
	dst := fieldOf("x", partialField(v, m))

	call := func(recv, in genExpr) genSteps {
		return checkErr(lang.Expr("%s.FromValueWithMask(%s, %s)", recv, in, sub), lang.Name("err"))
	}
	if !m.pointer() {
		return call(dst, src), nil
	}

	part, err := w.p.partialString(m.named)
	if err != nil {
		return nil, err
	}

	leaf, err := w.p.TypeString(m.leaf)
	if err != nil {
		return nil, err
	}
	// FromValue leaves the embedded partial value of a nil pointer zero, and
	// the paths are validated into a value thrown away.
	return lang.If(lang.Expr("%s == nil", src),
		call(lang.Expr("new(%s)", part), lang.Expr("*new(%s)", leaf)),
		call(dst, lang.Expr("*%s", src)),
	), nil
}
