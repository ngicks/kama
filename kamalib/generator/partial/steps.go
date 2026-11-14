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

func expr(format string, args ...any) genExpr {
	return genhelper.Expr[genCtx](format, args...)
}

func stmt(format string, args ...any) genStep {
	return genhelper.Stmt[genCtx](format, args...)
}

func name(role string) genExpr {
	return genhelper.Name[genCtx](role)
}

// joined returns the texts of parts separated by sep.
func joined(sep string, parts []genExpr) genExpr {
	return func(c genCtx) (string, error) {
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			s, err := part(c)
			if err != nil {
				return "", err
			}
			out = append(out, s)
		}
		return strings.Join(out, sep), nil
	}
}

// docComment returns the doc comment of a generated declaration: an empty
// line, each line of the text of format and args after "// ", an empty
// comment line and the marker of the generator. An empty line of the text
// gives an empty comment line.
func docComment(format string, args ...any) genExpr {
	doc := expr(format, args...)
	return func(c genCtx) (string, error) {
		s, err := doc(c)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString("\n")
		for line := range strings.SplitSeq(s, "\n") {
			if line == "" {
				b.WriteString("//\n")
				continue
			}
			b.WriteString("// " + line + "\n")
		}
		b.WriteString("//\n" + genmark.Marker(GeneratorName) + "\n")
		return b.String(), nil
	}
}

// documented returns signature preceded by doc, as the signature of
// [genhelper.Func]. doc renders after signature, in the scope of the
// function, so that it can spell the names of the parameters.
func documented(doc, signature genExpr) genExpr {
	return func(c genCtx) (string, error) {
		sig, err := signature(c)
		if err != nil {
			return "", err
		}
		d, err := doc(c)
		if err != nil {
			return "", err
		}
		return d + sig, nil
	}
}

// renderFunc renders a function documented by doc in c: its signature,
// which declares the receiver, the parameters and the named results, and
// body.
func renderFunc(c genCtx, doc, signature genExpr, body ...genStep) (string, error) {
	_, b, err := genhelper.Func(documented(doc, signature), body...).Render(c, nil)
	if err != nil {
		return "", err
	}
	return string(b), nil
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
