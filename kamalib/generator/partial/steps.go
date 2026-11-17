package partial

import (
	"strings"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// genCtx is the context the generated functions render with.
type genCtx = genhelper.BaseContext

type (
	genStep  = genhelper.Step[genCtx]
	genSteps = genhelper.Steps[genCtx]
	genExpr  = genhelper.Gen[genCtx]
)

type decl = genhelper.Decl

// lang is the vocabulary of steps for genCtx. It is zero-size and holds no
// state.
var lang genhelper.Lang[genCtx]

// funcContext returns a context positioned at a new root scope for one
// generated function, which spells the type parameters tparams. The scope
// avoids the predeclared identifiers and the package-level identifiers of
// the package, both of which the function may spell, and the identifiers
// of the imports of p.m.
func (p *mapper) funcContext(tparams []string) genCtx {
	s := genhelper.NewScope()
	s.ReserveUniverse()
	s.ReserveTypes(p.pkg.Types.Scope())
	s.Reserve(tparams...)

	return genhelper.NewBaseContext(s, &genhelper.Package{Imports: p.m, Path: p.pkg.PkgPath})
}

// docComment returns the doc comment of a generated declaration as one
// comment group: the text of format and args, an empty comment line and the
// marker of the generator.
func docComment(format string, args ...any) genExpr {
	return lang.Comment(
		lang.Expr(format, args...),
		lang.Expr(""),
		lang.Expr("%s", genmark.Marker(GeneratorName)),
	)
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

// along returns the steps converting the value in denotes along route:
// wrap once for each node of route, the first node outermost, then leaf.
// Each wrapper receives next, which returns the steps converting the value
// one node further along the route.
func along(
	route []typegraph.EdgeRouteNode,
	in genExpr,
	leaf func(in genExpr) genStep,
	wrap func(in genExpr, next func(in genExpr) genStep) genStep,
) genStep {
	if len(route) == 0 {
		return leaf(in)
	}

	return wrap(in, func(in genExpr) genStep { return along(route[1:], in, leaf, wrap) })
}
