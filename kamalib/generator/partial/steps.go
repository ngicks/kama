package partial

import (
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
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

// docComment returns the doc comment of a generated declaration, the text
// of format and args.
func docComment(format string, args ...any) genExpr {
	return lang.Comment(lang.Expr(format, args...))
}

// commentWidth is the width doc comments of helpers are wrapped at.
const commentWidth = 76

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
