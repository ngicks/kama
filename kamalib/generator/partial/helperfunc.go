package partial

import (
	"fmt"
	"go/types"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// call returns the call of h with args, recording h in the helpers of the
// package and as called by the file when it renders.
//
// Until the helpers are named, the first call from a file renders the
// function of h once: the helpers its body calls are called by the file
// too, and the packages of supportPaths it spells are recorded on h, while
// the imports of the file stay those it spells itself.
func (p *mapper) call(h *helper, args ...genExpr) genExpr {
	return func(c genCtx) (string, error) {
		in, err := opiter.TryCollect(
			opiter.WrapSeq(slices.Values(args)).
				Disassemble(func(arg genExpr) (string, error) { return arg(c) }),
		)
		if err != nil {
			return "", err
		}

		h, err := p.helpers.add(h)
		if err != nil {
			return "", err
		}

		fn := p.helpers.name(h)
		// Once named, the calls and imports of every helper are known, and
		// the calling file may not know the packages the helper spells.
		if p.helpers.names == nil && !p.calls[h] {
			p.calls[h] = true
			// The helpers file imports what the helper spells, which the
			// calling file may not, so the rendering goes to a separate map.
			fresh, err := p.Fresh()
			if err != nil {
				return "", err
			}

			file, support := p.FileScope, p.support
			p.FileScope, p.support = fresh, map[string]bool{}
			_, err = p.helperDecl(fn, h)
			h.spelled = p.support
			p.FileScope, p.support = file, support

			if err != nil {
				return "", err
			}
		}

		p.refs[fn] = true

		var targs string

		if tps := h.typeParams(); len(tps) > 0 {
			names := opiter.WrapSeq(slices.Values(tps)).
				Map(typeParamName).
				CollectSlice()

			// Type arguments are given rather than inferred: a parameter only a
			// constraint mentions cannot be inferred from the arguments.
			targs = "[" + strings.Join(names, ", ") + "]"
		}

		return fn + targs + "(" + strings.Join(in, ", ") + ")", nil
	}
}

func typeParamName(tp *types.TypeParam) string { return tp.Obj().Name() }

// helperFunc is the function of a helper.
type helperFunc struct {
	// c is the context the function renders in.
	c genCtx
	// tparams spells the type parameter list, or is empty.
	tparams string
	// params and results render the parameter and result lists. params
	// declares the parameters.
	params, results genExpr
	body            genSteps
	// doc renders the doc text after the name of the helper. It renders in
	// the scope of the function, so it spells the parameters by role.
	doc genExpr
}

// helperDecl spells the declaration of h, named fn.
func (p *mapper) helperDecl(fn string, h *helper) (string, error) {
	f, err := p.helperFunc(h)
	if err != nil {
		return "", err
	}
	// The text is wrapped once rendered, since a parameter renamed to avoid
	// a type parameter makes a line longer.
	doc := func(c genCtx) (string, error) {
		text, err := f.doc(c)
		if err != nil {
			return "", err
		}

		return genhelper.WrapComment(fn+" "+text, commentWidth), nil
	}

	return lang.Render(f.c, lang.DocFunc(
		docComment("%s", doc),
		lang.Expr("func %s%s(%s) %s", fn, f.tparams, f.params, f.results),
		f.body...,
	))
}

// helperTypes are the spellings the builders of the function of a helper
// share.
type helperTypes struct {
	// src spells the source type, and part spells its partial variant or is
	// empty.
	src, part string
	// held spells the type the und.Und of the helper holds.
	held string
	// und spells the package und. It is empty for the helpers whose code does
	// not spell und.
	und string
	// undSpelled spells the und.Und of the helper.
	undSpelled string
	// in and out spell the type of the first parameter and of the result.
	in, out string
}

// helperFunc builds the function of h.
//
// The route of a conversion is followed from the source outwards to the
// leaf conversion of the target. Every route node is a pointer: a nil
// pointer gives null on the way to the partial form, and the way back
// allocates.
func (p *mapper) helperFunc(h *helper) (helperFunc, error) {
	tps := h.typeParams()
	declare := func(tp *types.TypeParam) (string, error) {
		c, err := p.constraintString(tp)
		return tp.Obj().Name() + " " + c, err
	}

	tparams, err := opiter.TryCollect(
		opiter.WrapSeq(slices.Values(tps)).
			Disassemble(declare),
	)
	if err != nil {
		return helperFunc{}, err
	}

	names := opiter.WrapSeq(slices.Values(tps)).
		Map(typeParamName).
		CollectSlice()

	// The FileScope of p is the one in effect now, which call swaps while
	// it renders.
	f := helperFunc{c: p.FuncContext(names)}
	if len(tparams) > 0 {
		f.tparams = "[" + strings.Join(tparams, ", ") + "]"
	}

	var t helperTypes

	t.src, err = p.TypeString(h.typ)
	if err != nil {
		return helperFunc{}, err
	}

	if h.named != nil {
		if t.part, err = p.partialString(h.named); err != nil {
			return helperFunc{}, err
		}
	}

	t.held = t.part
	switch pointer, plain := h.held(); {
	case plain:
		t.held = t.src
	case pointer:
		t.held = "*" + t.part
	}
	// The doc comment spells the types as the code does. Spelling und
	// records its import, so only the helpers whose code spells it do.
	switch h.op {
	case opFromEmbedded, opToEmbedded, opAllUndefined:
	default:
		if t.und, err = p.ident(typequery.UndPath); err != nil {
			return helperFunc{}, err
		}

		t.undSpelled = t.und + ".Und[" + t.held + "]"
	}

	sigIn, sigOut := signatureOf(h.op, t.src, t.part, t.undSpelled, "bool")
	t.in, t.out = sigIn[0], sigOut[0]

	switch h.op {
	case opFromTarget, opFromTargetRef:
		return fromTargetFunc(h, f, t), nil
	case opToTarget, opToTargetRef:
		return toTargetFunc(h, f, t), nil
	case opFromEmbedded, opToEmbedded:
		return p.embeddedFunc(h, f, t), nil
	case opAllUndefined:
		return p.allUndefinedFunc(h, f, t)
	case opMergeAppend:
		return p.mergeAppendFunc(h, f, t)
	case opMergeRecursive:
		return mergeRecursiveFunc(h, f, t), nil
	}

	return helperFunc{}, fmt.Errorf("partial: unknown helper op %d", h.op)
}

// fromTargetFunc builds f into the function of h converting a target into
// its partial form held by an und.Und.
func fromTargetFunc(h *helper, f helperFunc, t helperTypes) helperFunc {
	null := lang.Expr("%s.Null[%s]()", t.und, t.held)

	leaf := func(in genExpr) genStep {
		return genSteps{
			lang.Stmt("var %s %s", decl("x"), t.part),
			lang.Stmt("%s.FromValue(%s)", lang.Name("x"), in),
			lang.Stmt("return %s.Defined(%s)", t.und, lang.Name("x")),
		}
	}
	if h.op == opFromTargetRef {
		leaf = func(in genExpr) genStep {
			return genSteps{
				lang.Stmt("%s := new(%s)", decl("x"), t.part),
				lang.Stmt("%s.FromValue(%s)", lang.Name("x"), in),
				lang.Stmt("return %s.Defined(%s)", t.und, lang.Name("x")),
			}
		}
	}

	guard := func(in genExpr) genStep {
		return lang.If(lang.Expr("%s == nil", in), lang.Stmt("return %s", null), nil)
	}
	// A nil slice or map converts to null like a nil pointer does.
	nilable := len(h.route) > 0 || !isStruct(h.named)
	if len(h.route) == 0 && nilable {
		inner := leaf
		leaf = func(in genExpr) genStep { return genSteps{guard(in), inner(in)} }
	}

	wrap := func(in genExpr, next func(genExpr) genStep) genStep {
		return genSteps{guard(in), next(lang.Expr("*%s", in))}
	}
	f.params, f.results = lang.Expr("%s %s", decl("v"), t.src), lang.Expr("%s", t.undSpelled)

	f.body = genSteps{along(h.route, lang.Name("v"), leaf, wrap)}
	switch {
	case h.op == opFromTargetRef:
		f.doc = lang.Expr(
			"converts %s into %s, a newly allocated defined value, or null for a nil %s.",
			t.in, t.out, lang.Name("v"))
	case nilable:
		f.doc = lang.Expr("converts %s into %s, defined, or null for a nil %s.",
			t.in, t.out, lang.Name("v"))
	default:
		f.doc = lang.Expr("converts %s into %s, defined.", t.in, t.out)
	}

	return f
}

// toTargetFunc builds f into the function of h converting a partial value
// held by an und.Und back into a newly allocated target.
func toTargetFunc(h *helper, f helperFunc, t helperTypes) helperFunc {
	cond := func(in genExpr) genExpr { return lang.Expr("!%s.IsDefined()", in) }

	f.doc = lang.Expr("converts %s into %s, newly allocated, or nil when %s is undefined or null.",
		t.in, t.out, lang.Name("p"))
	if h.op == opToTargetRef {
		cond = func(in genExpr) genExpr {
			return lang.Expr("!%s.IsDefined() || %s.Value() == nil", in, in)
		}
		f.doc = lang.Expr(
			"converts %s into %s, newly allocated, or nil when %s is undefined, null or nil.",
			t.in, t.out, lang.Name("p"),
		)
	}

	wrap := func(in genExpr, next func(genExpr) genStep) genStep {
		return genSteps{
			lang.If(cond(in), lang.Stmt("return nil"), nil),
			next(lang.Expr("%s.Value()", in)),
		}
	}
	f.params, f.results = lang.Expr("%s %s", decl("p"), t.undSpelled), lang.Expr("%s", t.src)
	f.body = genSteps{along(h.route, lang.Name("p"), allocate, wrap)}

	return f
}

// embeddedFunc builds f into the function of h converting a pointer to an
// embedded target into its embedded partial value or back.
func (p *mapper) embeddedFunc(h *helper, f helperFunc, t helperTypes) helperFunc {
	if h.op == opFromEmbedded {
		leaf := func(in genExpr) genStep {
			return lang.Stmt("%s.FromValue(%s)", lang.Name("x"), in)
		}
		wrap := func(in genExpr, next func(genExpr) genStep) genStep {
			return lang.If(lang.Expr("%s != nil", in), next(lang.Expr("*%s", in)), nil)
		}
		f.params, f.results = lang.Expr("%s %s", decl("v"), t.src), lang.Expr("%s", t.part)
		f.body = genSteps{
			lang.Stmt("var %s %s", decl("x"), t.part),
			along(h.route, lang.Name("v"), leaf, wrap),
			lang.Stmt("return %s", lang.Name("x")),
		}
		f.doc = lang.Expr("converts %s into %s, or the zero partial value for a nil %s.",
			t.in, t.out, lang.Name("v"))

		return f
	}

	// An embedded partial has no undefined state of its own.
	wrap := func(in genExpr, next func(genExpr) genStep) genStep {
		test := p.call(&helper{op: opAllUndefined, typ: h.named, named: h.named}, in)
		return genSteps{lang.If(test, lang.Stmt("return nil"), nil), next(in)}
	}
	f.params, f.results = lang.Expr("%s %s", decl("p"), t.part), lang.Expr("%s", t.src)
	f.body = genSteps{along(h.route, lang.Name("p"), allocate, wrap)}
	f.doc = lang.Expr(
		"converts %s into %s, newly allocated, or nil when every field of %s is undefined.",
		t.in,
		t.out,
		lang.Name("p"),
	)

	return f
}

// allUndefinedFunc builds f into the function of h reporting whether every
// field of an embedded partial value is undefined.
func (p *mapper) allUndefinedFunc(h *helper, f helperFunc, t helperTypes) (helperFunc, error) {
	var tests []genExpr

	// target tells a target only for a type a target reaches. The fields of
	// an implementor reach a type of the run through no target, so the
	// targets themselves tell there whether that type is one.
	partialOf := p.sel.partialOf
	if _, ok := p.sel.target(h.named); !ok {
		targets := p.sel.targets()
		partialOf = func(t types.Type, pkgPath string) (*types.Named, bool) {
			named, ok := p.sel.partialOf(t, pkgPath)
			if !ok {
				return nil, false
			}

			_, isImplementor := p.sel.implementor(named)
			_, isTarget := targets[typegraph.QualifiedTypeFromObject(named.Obj())]

			return named, isImplementor || isTarget
		}
	}

	st := h.named.Underlying().(*types.Struct)
	for fv := range st.Fields() {
		if fv.Name() == "_" {
			continue
		}

		named, ok := partialOf(deref(fv.Type()), p.Pkg.PkgPath)
		if ok && fv.Embedded() && isStruct(named) {
			tests = append(tests, p.call(
				&helper{op: opAllUndefined, typ: named, named: named},
				lang.Expr("%s.%s", lang.Name("p"), partialName(named.Obj().Name())),
			))

			continue
		}
		// An unexported field of a partial type of another package cannot
		// be read here.
		if !fv.Exported() && fv.Pkg() != nil && fv.Pkg().Path() != p.Pkg.PkgPath {
			return helperFunc{}, fmt.Errorf(
				"%w: embedded %s of package %s has the unexported field %s, "+
					"which package %s cannot test for undefined",
				ErrUnsupported, t.part, fv.Pkg().Path(), fv.Name(), p.Pkg.PkgPath,
			)
		}

		if isFallback(h.named, fv) {
			tests = append(tests, lang.Expr("%s.%s == nil", lang.Name("p"), fv.Name()))
			continue
		}

		tests = append(tests, lang.Expr("%s.%s.IsUndefined()", lang.Name("p"), fv.Name()))
	}

	if len(tests) == 0 {
		tests = []genExpr{lang.Expr("true")}
	}

	f.params, f.results = lang.Expr("%s %s", decl("p"), t.part), lang.Expr("bool")
	f.body = genSteps{lang.Stmt("return %s", lang.Join(" &&\n", tests...))}
	f.doc = lang.Expr("reports whether every field of %s, a %s, is undefined, the fields "+
		"of embedded partial values included. Blank fields are skipped.", lang.Name("p"), t.in)

	return f, nil
}

// mergeAppendFunc builds f into the function of h merging two collections
// held by und.Und values into a new one.
func (p *mapper) mergeAppendFunc(h *helper, f helperFunc, t helperTypes) (helperFunc, error) {
	f.body = genSteps{
		lang.If(lang.Expr("%s.IsUndefined()", lang.Name("r")),
			lang.Stmt("return %s", lang.Name("l")), nil),
		lang.If(
			lang.Expr("!%s.IsDefined() || !%s.IsDefined()", lang.Name("l"), lang.Name("r")),
			lang.Stmt("return %s", lang.Name("r")), nil,
		),
		lang.Stmt("%s, %s := %s.Value(), %s.Value()",
			decl("lv"), decl("rv"), lang.Name("l"), lang.Name("r")),
		lang.If(
			lang.Expr("%s == nil && %s == nil", lang.Name("lv"), lang.Name("rv")),
			lang.Stmt("return %s", lang.Name("r")), nil,
		),
	}

	elems := lang.Expr("a new slice of the elements of %s then %s", lang.Name("l"), lang.Name("r"))
	if isMapType(h.mergeType()) {
		elems = lang.Expr("a new map of the entries of %s with the entries of %s set",
			lang.Name("l"), lang.Name("r"))

		mp, err := p.ident(mapsPath)
		if err != nil {
			return helperFunc{}, err
		}

		f.body = append(f.body,
			lang.Stmt("%s := make(%s, len(%s)+len(%s))",
				decl("o"), t.held, lang.Name("lv"), lang.Name("rv")),
			lang.Stmt("%s.Copy(%s, %s)", mp, lang.Name("o"), lang.Name("lv")),
			lang.Stmt("%s.Copy(%s, %s)", mp, lang.Name("o"), lang.Name("rv")),
			lang.Stmt("return %s.Defined(%s)", t.und, lang.Name("o")),
		)
	} else {
		f.body = append(f.body,
			lang.Stmt("%s := make(%s, 0, len(%s)+len(%s))",
				decl("o"), t.held, lang.Name("lv"), lang.Name("rv")),
			lang.Stmt("%s = append(%s, %s...)", lang.Name("o"), lang.Name("o"), lang.Name("lv")),
			lang.Stmt("return %s.Defined(append(%s, %s...))",
				t.und, lang.Name("o"), lang.Name("rv")),
		)
	}

	f.params = lang.Expr("%s, %s %s", decl("l"), decl("r"), t.undSpelled)
	f.results = lang.Expr("%s", t.undSpelled)
	f.doc = lang.Expr("merges %s onto %s, both %s: it returns %s when %s is undefined, "+
		"%s when both are defined, and %s otherwise. Two nil values give %s."+
		"\n\nA defined nil or empty side is not returned in place of the new value, "+
		"so the result never shares storage with %s or %s.",
		lang.Name("r"), lang.Name("l"), t.in, lang.Name("l"), lang.Name("r"),
		elems, lang.Name("r"), lang.Name("r"),
		lang.Name("l"), lang.Name("r"))

	return f, nil
}

// mergeRecursiveFunc builds f into the function of h merging two partial
// values held by und.Und values with their Merge methods.
func mergeRecursiveFunc(h *helper, f helperFunc, t helperTypes) helperFunc {
	f.body = genSteps{
		lang.If(lang.Expr("%s.IsUndefined()", lang.Name("r")),
			lang.Stmt("return %s", lang.Name("l")), nil),
	}
	if len(h.route) > 0 {
		f.body = append(f.body,
			lang.If(
				lang.Expr(
					"!%s.IsDefined() || !%s.IsDefined() || %s.Value() == nil || %s.Value() == nil",
					lang.Name("l"),
					lang.Name("r"),
					lang.Name("l"),
					lang.Name("r"),
				),
				lang.Stmt("return %s", lang.Name("r")), nil,
			),
			lang.Stmt("%s := %s.Value().Merge(*%s.Value())",
				decl("x"), lang.Name("l"), lang.Name("r")),
			lang.Stmt("return %s.Defined(&%s)", t.und, lang.Name("x")),
		)
		f.doc = lang.Expr("merges %s onto %s, both %s: it returns a new merge of their "+
			"values when both are defined and non-nil, %s when %s is undefined, and %s otherwise.",
			lang.Name("r"), lang.Name("l"), t.in, lang.Name("l"), lang.Name("r"), lang.Name("r"))
	} else {
		f.body = append(f.body,
			lang.If(
				lang.Expr("!%s.IsDefined() || !%s.IsDefined()", lang.Name("l"), lang.Name("r")),
				lang.Stmt("return %s", lang.Name("r")), nil,
			),
			lang.Stmt("return %s.Defined(%s.Value().Merge(%s.Value()))",
				t.und, lang.Name("l"), lang.Name("r")),
		)
		f.doc = lang.Expr("merges %s onto %s, both %s: it returns the merge of their "+
			"values when both are defined, %s when %s is undefined, and %s otherwise.",
			lang.Name("r"), lang.Name("l"), t.in, lang.Name("l"), lang.Name("r"), lang.Name("r"))
	}

	f.params = lang.Expr("%s, %s %s", decl("l"), decl("r"), t.undSpelled)
	f.results = lang.Expr("%s", t.undSpelled)

	return f
}

// allocate is the step returning a pointer to a new variable holding the
// value the partial value in describes.
func allocate(in genExpr) genStep {
	return genSteps{
		lang.Stmt("%s := %s.ToValue()", decl("x"), in),
		lang.Stmt("return &%s", lang.Name("x")),
	}
}

// mergeType is the type whose values a merge helper combines, past its
// pointers.
func (h *helper) mergeType() types.Type {
	if h.partial {
		return h.named
	}

	return h.typ
}
