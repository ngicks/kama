package partial

import (
	"go/types"
	"slices"
	"strconv"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// fieldMaskLocals are the identifiers the field mask methods declare
// besides localNames. The locals of each field holding child paths are
// named after the field and are not listed.
var fieldMaskLocals = []string{"paths", "path", "head", "rest", "nested", "y"}

// maskKind tells how a field takes part in the paths of a field mask.
type maskKind int

const (
	// maskLeaf is a field addressed as a whole.
	maskLeaf maskKind = iota
	// maskBranch is a field holding a partial value of a type with field
	// masks: addressed as a whole and through the paths of its fields.
	maskBranch
	// maskEmbedded is an embedded partial value, whose paths are those of
	// its parent.
	maskEmbedded
)

// maskField is a field of a partial type with field masks. It names the
// field; the writers match its type again.
type maskField struct {
	// v is the source field. name is its name and partial the name of
	// its partial field.
	v             *types.Var
	name, partial string

	kind maskKind
	// segment is the segment of a leaf or branch.
	segment string
	// heads are the segments of the fields of an embedded partial value.
	heads []string
}

// maskFields classifies the fields of st, the struct of the target n, that
// are not blank, for a declaration of the package at pkgPath.
func (s *selection) maskFields(
	n *typegraph.Node,
	st *types.Struct,
	pkgPath string,
) ([]maskField, error) {
	var out []maskField

	for v := range st.Fields() {
		m := s.matchField(v, pkgPath)
		if m.blank {
			continue
		}

		mf := maskField{v: v, name: v.Name(), partial: partialField(v, m), segment: v.Name()}

		if o, ok := s.maskNames[fieldKeyOf(n.Id, v.Name())]; ok {
			mf.segment = o.name
		}

		switch {
		case m.embedded:
			mf.kind = maskEmbedded

			segs, err := s.maskSegments(s.node(m.named))
			if err != nil {
				return nil, err
			}

			mf.heads = slices.AppendSeq(
				mf.heads,
				opiter.WrapSeq(slices.Values(segs)).
					Map(func(m maskSegment) string { return m.name }).
					Iter(),
			)
		case m.kind == leafTarget && isStruct(m.named) && s.fieldMasked(s.node(m.named).Id):
			mf.kind = maskBranch
		}

		out = append(out, mf)
	}

	return out, nil
}

// fieldMaskMethods writes FieldMask and FromValueWithMask when the target
// has field masks enabled. It writes nothing otherwise.
func (w *methodWriter) fieldMaskMethods(st *types.Struct) error {
	if !w.p.sel.fieldMasked(w.n.Id) {
		return nil
	}

	masks, err := w.p.sel.maskFields(w.n, st, w.p.Pkg.PkgPath)
	if err != nil {
		return err
	}

	err = w.function(
		docComment("FieldMask returns the sorted paths of the fields %s sets. It returns\n"+
			"nil when %s sets no field. An undefined field gives no path. A null or\n"+
			"defined field gives its own path. A defined field holding a partial\n"+
			"value with field masks gives the paths of the fields of that value\n"+
			"instead. The paths of an embedded partial value are paths of %s.",
			lang.Name("p"), lang.Name("p"), lang.Name("p")),
		lang.Expr("func (%s %s) FieldMask() []string", decl("p"), w.recv),
		w.fieldMaskBody(masks),
	)
	if err != nil {
		return err
	}

	body, err := w.fromValueWithMaskBody(masks)
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

// fieldMaskBody returns the body of FieldMask.
func (w *methodWriter) fieldMaskBody(masks []maskField) genSteps {
	appendPath := func(path genExpr) genStep {
		return lang.Stmt("%s = append(%s, %s)", lang.Name("paths"), lang.Name("paths"), path)
	}
	out := genSteps{lang.Stmt("var %s []string", decl("paths"))}

	for _, f := range masks {
		field := fieldOf("p", f.partial)
		own := appendPath(lang.Expr("%s", strconv.Quote(f.segment)))

		children := lang.ForRange("", "path", lang.Expr("%s.Value().FieldMask()", field),
			appendPath(lang.Expr("%s+%s", strconv.Quote(f.segment+"."), lang.Name("path"))))
		switch {
		case f.kind == maskEmbedded:
			out = append(out,
				lang.Stmt("%s = append(%s, %s.FieldMask()...)",
					lang.Name("paths"), lang.Name("paths"), field))
		case f.kind == maskBranch && w.p.matchField(f.v).ref:
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
		case f.kind == maskBranch:
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
// field f and telling whether a path selects it whole.
func subRole(f maskField) string   { return "sub" + f.name }
func wholeRole(f maskField) string { return "whole" + f.name }

// fromValueWithMaskBody returns the body of FromValueWithMask.
//
// The loop routes each path by its first segment: a leaf converts at once,
// which repeats harmlessly for a duplicate path, and the child paths of a
// branch or an embedded partial value are collected for one call of its
// FromValueWithMask after the loop. That call runs even when a path
// selects the branch whole, so that a child path is validated whatever the
// order of the paths and whatever v holds.
func (w *methodWriter) fromValueWithMaskBody(masks []maskField) (genSteps, error) {
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

	for _, f := range masks {
		seg := lang.Expr("%s", strconv.Quote(f.segment))
		switch f.kind {
		case maskLeaf:
			useNested = true

			cases = append(cases, lang.Case(seg, genSteps{
				lang.If(lang.Name("nested"),
					fail("%s: field %s has no subfields",
						lang.Name("path"), lang.Name("head")), nil),
				w.from(w.p.matchField(f.v), fieldOf("x", f.partial), fieldOf("v", f.name)),
			}))
		case maskBranch:
			useNested, useRest = true, true

			decls = append(decls,
				lang.Stmt("var %s []string", decl(subRole(f))),
				lang.Stmt("var %s bool", decl(wholeRole(f))),
			)
			cases = append(cases, lang.Case(seg, lang.Switch(nil,
				lang.Case(
					lang.Expr("!%s", lang.Name("nested")),
					lang.Stmt("%s = true", lang.Name(wholeRole(f))),
				),
				lang.Case(
					lang.Expr(`%s == ""`, lang.Name("rest")),
					fail("%s: empty segment", lang.Name("path")),
				),
				lang.Default(
					lang.Stmt(
						"%s = append(%s, %s)",
						lang.Name(subRole(f)),
						lang.Name(subRole(f)),
						lang.Name("rest"),
					),
				),
			)))

			build, err := w.maskBranch(f, fmtIdent)
			if err != nil {
				return nil, err
			}

			after = append(after,
				lang.If(lang.Expr("%s != nil", lang.Name(subRole(f))), build, nil),
				lang.If(
					lang.Name(wholeRole(f)),
					w.from(
						w.p.matchField(f.v),
						fieldOf("x", f.partial),
						fieldOf("v", f.name),
					),
					nil,
				),
			)
		case maskEmbedded:
			if len(f.heads) == 0 {
				continue
			}

			decls = append(decls, lang.Stmt("var %s []string", decl(subRole(f))))

			heads := opiter.WrapSeq(slices.Values(f.heads)).
				Map(func(s string) genExpr { return lang.Expr("%s", strconv.Quote(s)) }).
				CollectSlice()

			cases = append(cases, lang.Case(
				lang.Join(", ", heads...),
				lang.Stmt("%s = append(%s, %s)",
					lang.Name(subRole(f)), lang.Name(subRole(f)), lang.Name("path")),
			))

			build, err := w.maskEmbedded(f)
			if err != nil {
				return nil, err
			}

			after = append(after,
				lang.If(lang.Expr("%s != nil", lang.Name(subRole(f))), build, nil))
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

// maskBranch returns the steps setting the branch f of x to the partial
// value its child paths select from v.
func (w *methodWriter) maskBranch(f maskField, fmtIdent genExpr) (genStep, error) {
	m := w.p.matchField(f.v)

	part, err := w.p.partialString(m.named)
	if err != nil {
		return nil, err
	}

	und := w.pkgIdent(undPath)
	sub := lang.Name(subRole(f))
	src := fieldOf("v", f.name)
	dst := fieldOf("x", f.partial)

	call := func(in genExpr) genSteps {
		return checkErr(
			lang.Expr("%s.FromValueWithMask(%s, %s)", lang.Name("y"), in, sub),
			lang.Expr("%s.Errorf(%s, %s, %s)", fmtIdent, strconv.Quote("%s.%w"),
				strconv.Quote(f.segment), lang.Name("err")),
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

// maskEmbedded returns the steps setting the embedded partial value f of x
// to the partial value the paths routed to it select from v.
func (w *methodWriter) maskEmbedded(f maskField) (genStep, error) {
	m := w.p.matchField(f.v)
	sub := lang.Name(subRole(f))
	src := fieldOf("v", f.name)
	dst := fieldOf("x", f.partial)

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
