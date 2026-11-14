package partial

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

// call returns the call of h with args, recording h in the helpers of the
// package and as called by the file when it renders.
//
// The first call from a file renders the function of h once: the helpers
// its body calls are called by the file too, and the packages of
// supportPaths it spells are recorded on h, while the imports of the file
// stay those it spells itself.
func (p *mapper) call(h *helper, args ...genExpr) genExpr {
	return func(c genCtx) (string, error) {
		in := make([]string, 0, len(args))
		for _, arg := range args {
			s, err := arg(c)
			if err != nil {
				return "", err
			}
			in = append(in, s)
		}

		h, err := p.helpers.add(h)
		if err != nil {
			return "", err
		}
		fn := p.helpers.name(h)
		if !p.calls[h] {
			p.calls[h] = true
			// The file declaring the helper imports what it spells, which the
			// calling file may not, so the rendering goes to a separate map.
			m, err := p.parser.ParseDst(nil)
			if err != nil {
				return "", err
			}
			fileMap, support := p.m, p.support
			p.m, p.support = m, map[string]bool{}
			_, err = p.helperDecl(fn, h)
			h.spelled = p.support
			p.m, p.support = fileMap, support
			if err != nil {
				return "", err
			}
		}
		p.refs[fn] = true

		var targs string
		if tps := h.typeParams(); len(tps) > 0 {
			names := make([]string, 0, len(tps))
			for _, tp := range tps {
				names = append(names, tp.Obj().Name())
			}
			// Type arguments are given rather than inferred: a parameter only a
			// constraint mentions cannot be inferred from the arguments.
			targs = "[" + strings.Join(names, ", ") + "]"
		}
		return fn + targs + "(" + strings.Join(in, ", ") + ")", nil
	}
}

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
	// doc is the doc text after the name of the helper.
	doc string
}

// helperDecl spells the declaration of h, named fn.
func (p *mapper) helperDecl(fn string, h *helper) (string, error) {
	f, err := p.helperFunc(h)
	if err != nil {
		return "", err
	}
	return renderFunc(f.c,
		docComment("%s", wrapComment(fn+" "+f.doc)),
		expr("func %s%s(%s) %s", fn, f.tparams, f.params, f.results),
		f.body...,
	)
}

// helperFunc builds the function of h.
//
// The route of a conversion is followed from the source outwards to the
// leaf conversion of the target. Every route node is a pointer: a nil
// pointer gives null on the way to the partial form, and the way back
// allocates.
func (p *mapper) helperFunc(h *helper) (helperFunc, error) {
	tps := h.typeParams()
	names := make([]string, 0, len(tps))
	var tparams []string
	for _, tp := range tps {
		names = append(names, tp.Obj().Name())
		c, err := p.constraintString(tp)
		if err != nil {
			return helperFunc{}, err
		}
		tparams = append(tparams, tp.Obj().Name()+" "+c)
	}
	// p.m is the Map in effect now, which call swaps while it renders.
	f := helperFunc{c: p.funcContext(names)}
	if len(tparams) > 0 {
		f.tparams = "[" + strings.Join(tparams, ", ") + "]"
	}
	src, err := p.typeString(h.typ)
	if err != nil {
		return helperFunc{}, err
	}
	var part string
	if h.named != nil {
		if part, err = p.partialString(h.named); err != nil {
			return helperFunc{}, err
		}
	}
	// held is the type the und.Und of the helper holds.
	held := part
	switch pointer, plain := h.held(); {
	case plain:
		held = src
	case pointer:
		held = "*" + part
	}
	// The doc comment spells the types as the code does. Spelling und
	// records its import, so only the helpers whose code spells it do.
	var u, undSpelled string
	switch h.op {
	case opFromEmbedded, opToEmbedded, opAllUndefined:
	default:
		if u, err = p.ident(undPath); err != nil {
			return helperFunc{}, err
		}
		undSpelled = u + ".Und[" + held + "]"
	}
	sigIn, sigOut := signatureOf(h.op, src, part, undSpelled, "bool")
	f.doc = "converts " + sigIn[0] + " into " + sigOut[0]

	switch h.op {
	case opFromTarget, opFromTargetRef:
		null := expr("%s.Null[%s]()", u, held)
		leaf := func(in genExpr) genStep {
			return genSteps{
				stmt("var %s %s", decl("x"), part),
				stmt("%s.FromValue(%s)", name("x"), in),
				stmt("return %s.Defined(%s)", u, name("x")),
			}
		}
		if h.op == opFromTargetRef {
			leaf = func(in genExpr) genStep {
				return genSteps{
					stmt("%s := new(%s)", decl("x"), part),
					stmt("%s.FromValue(%s)", name("x"), in),
					stmt("return %s.Defined(%s)", u, name("x")),
				}
			}
		}
		guard := func(in genExpr) genStep {
			return genhelper.If(expr("%s == nil", in), stmt("return %s", null), nil)
		}
		// A nil slice or map converts to null like a nil pointer does.
		nilable := len(h.route) > 0 || !isStruct(h.named)
		if len(h.route) == 0 && nilable {
			inner := leaf
			leaf = func(in genExpr) genStep { return genSteps{guard(in), inner(in)} }
		}
		wrap := func(in genExpr, next func(genExpr) genStep) genStep {
			return genSteps{guard(in), next(expr("*%s", in))}
		}
		f.params, f.results = expr("%s %s", decl("v"), src), expr("%s", undSpelled)
		f.body = genSteps{along(h.route, name("v"), leaf, wrap)}
		switch {
		case h.op == opFromTargetRef:
			f.doc += ", a newly allocated defined value, or null for a nil v."
		case nilable:
			f.doc += ", defined, or null for a nil v."
		default:
			f.doc += ", defined."
		}
		return f, nil

	case opToTarget, opToTargetRef:
		cond := func(in genExpr) genExpr { return expr("!%s.IsDefined()", in) }
		f.doc += ", newly allocated, or nil when p is undefined or null."
		if h.op == opToTargetRef {
			cond = func(in genExpr) genExpr {
				return expr("!%s.IsDefined() || %s.Value() == nil", in, in)
			}
			f.doc = "converts " + sigIn[0] + " into " + sigOut[0] +
				", newly allocated, or nil when p is undefined, null or nil."
		}
		wrap := func(in genExpr, next func(genExpr) genStep) genStep {
			return genSteps{
				genhelper.If(cond(in), stmt("return nil"), nil),
				next(expr("%s.Value()", in)),
			}
		}
		f.params, f.results = expr("%s %s", decl("p"), undSpelled), expr("%s", src)
		f.body = genSteps{along(h.route, name("p"), allocate, wrap)}
		return f, nil

	case opFromEmbedded:
		leaf := func(in genExpr) genStep { return stmt("%s.FromValue(%s)", name("x"), in) }
		wrap := func(in genExpr, next func(genExpr) genStep) genStep {
			return genhelper.If(expr("%s != nil", in), next(expr("*%s", in)), nil)
		}
		f.params, f.results = expr("%s %s", decl("v"), src), expr("%s", part)
		f.body = genSteps{
			stmt("var %s %s", decl("x"), part),
			along(h.route, name("v"), leaf, wrap),
			stmt("return %s", name("x")),
		}
		f.doc += ", or the zero partial value for a nil v."
		return f, nil

	case opToEmbedded:
		// An embedded partial has no undefined state of its own.
		wrap := func(in genExpr, next func(genExpr) genStep) genStep {
			test := p.call(&helper{op: opAllUndefined, typ: h.named, named: h.named}, in)
			return genSteps{genhelper.If(test, stmt("return nil"), nil), next(in)}
		}
		f.params, f.results = expr("%s %s", decl("p"), part), expr("%s", src)
		f.body = genSteps{along(h.route, name("p"), allocate, wrap)}
		f.doc += ", newly allocated, or nil when every field of p is undefined."
		return f, nil

	case opAllUndefined:
		var tests []genExpr
		st := h.named.Underlying().(*types.Struct)
		for fv := range st.Fields() {
			if fv.Name() == "_" {
				continue
			}
			named, ok := p.sel.partialOf(deref(fv.Type()), p.pkg.PkgPath)
			if ok && fv.Embedded() && isStruct(named) {
				tests = append(tests, p.call(
					&helper{op: opAllUndefined, typ: named, named: named},
					expr("%s.%s", name("p"), partialName(named.Obj().Name())),
				))
				continue
			}
			// An unexported field of a partial type of another package cannot
			// be read here.
			if !fv.Exported() && fv.Pkg() != nil && fv.Pkg().Path() != p.pkg.PkgPath {
				return helperFunc{}, fmt.Errorf(
					"%w: embedded %s of package %s has the unexported field %s, "+
						"which package %s cannot test for undefined",
					ErrUnsupported, part, fv.Pkg().Path(), fv.Name(), p.pkg.PkgPath,
				)
			}
			tests = append(tests, expr("%s.%s.IsUndefined()", name("p"), fv.Name()))
		}
		if len(tests) == 0 {
			tests = []genExpr{expr("true")}
		}
		f.params, f.results = expr("%s %s", decl("p"), part), expr("bool")
		f.body = genSteps{stmt("return %s", joined(" &&\n", tests))}
		f.doc = "reports whether every field of p, a " + sigIn[0] + ", is undefined, the fields " +
			"of embedded partial values included. Blank fields are skipped."
		return f, nil

	case opMergeAppend:
		f.body = genSteps{
			genhelper.If(expr("%s.IsUndefined()", name("r")), stmt("return %s", name("l")), nil),
			genhelper.If(
				expr("!%s.IsDefined() || !%s.IsDefined()", name("l"), name("r")),
				stmt("return %s", name("r")), nil,
			),
			stmt("%s, %s := %s.Value(), %s.Value()", decl("lv"), decl("rv"), name("l"), name("r")),
			genhelper.If(
				expr("%s == nil && %s == nil", name("lv"), name("rv")),
				stmt("return %s", name("r")), nil,
			),
		}
		elems := "a new slice of the elements of l then r"
		if isMapType(h.mergeType()) {
			elems = "a new map of the entries of l with the entries of r set"
			mp, err := p.ident(mapsPath)
			if err != nil {
				return helperFunc{}, err
			}
			f.body = append(f.body,
				stmt("%s := make(%s, len(%s)+len(%s))", decl("o"), held, name("lv"), name("rv")),
				stmt("%s.Copy(%s, %s)", mp, name("o"), name("lv")),
				stmt("%s.Copy(%s, %s)", mp, name("o"), name("rv")),
				stmt("return %s.Defined(%s)", u, name("o")),
			)
		} else {
			f.body = append(f.body,
				stmt("%s := make(%s, 0, len(%s)+len(%s))", decl("o"), held, name("lv"), name("rv")),
				stmt("%s = append(%s, %s...)", name("o"), name("o"), name("lv")),
				stmt("return %s.Defined(append(%s, %s...))", u, name("o"), name("rv")),
			)
		}
		f.params = expr("%s, %s %s", decl("l"), decl("r"), undSpelled)
		f.results = expr("%s", undSpelled)
		f.doc = "merges r onto l, both " + sigIn[0] + ": it returns l when r is undefined, " +
			elems + " when both are defined, and r otherwise. Two nil values give r." +
			"\n\nA defined nil or empty side is not returned in place of the new value, " +
			"so the result never shares storage with l or r."
		return f, nil

	case opMergeRecursive:
		f.body = genSteps{
			genhelper.If(expr("%s.IsUndefined()", name("r")), stmt("return %s", name("l")), nil),
		}
		if len(h.route) > 0 {
			f.body = append(f.body,
				genhelper.If(
					expr(
						"!%s.IsDefined() || !%s.IsDefined() || %s.Value() == nil || %s.Value() == nil",
						name("l"),
						name("r"),
						name("l"),
						name("r"),
					),
					stmt("return %s", name("r")), nil,
				),
				stmt("%s := %s.Value().Merge(*%s.Value())", decl("x"), name("l"), name("r")),
				stmt("return %s.Defined(&%s)", u, name("x")),
			)
			f.doc = "merges r onto l, both " + sigIn[0] + ": it returns a new merge of their " +
				"values when both are defined and non-nil, l when r is undefined, and r otherwise."
		} else {
			f.body = append(f.body,
				genhelper.If(
					expr("!%s.IsDefined() || !%s.IsDefined()", name("l"), name("r")),
					stmt("return %s", name("r")), nil,
				),
				stmt("return %s.Defined(%s.Value().Merge(%s.Value()))", u, name("l"), name("r")),
			)
			f.doc = "merges r onto l, both " + sigIn[0] + ": it returns the merge of their " +
				"values when both are defined, l when r is undefined, and r otherwise."
		}
		f.params = expr("%s, %s %s", decl("l"), decl("r"), undSpelled)
		f.results = expr("%s", undSpelled)
		return f, nil
	}
	return helperFunc{}, fmt.Errorf("partial: unknown helper op %d", h.op)
}

// allocate is the step returning a pointer to a new variable holding the
// value the partial value in describes.
func allocate(in genExpr) genStep {
	return genSteps{
		stmt("%s := %s.ToValue()", decl("x"), in),
		stmt("return &%s", name("x")),
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

// commentWidth is the width doc comments of helpers are wrapped at.
const commentWidth = 76

// wrapComment returns text as lines that are at most commentWidth bytes
// long as // comment lines, except for words longer than that. Paragraphs
// of text are separated by an empty line.
func wrapComment(text string) string {
	// prefix is the length of "// " before each line.
	const prefix = 3
	var lines []string
	for i, para := range strings.Split(text, "\n\n") {
		if i > 0 {
			lines = append(lines, "")
		}
		line := ""
		for word := range strings.FieldsSeq(para) {
			if line != "" && prefix+len(line)+1+len(word) > commentWidth {
				lines = append(lines, line)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
