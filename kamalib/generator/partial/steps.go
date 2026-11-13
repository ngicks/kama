package partial

import (
	"fmt"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

// genCtx is the context the generated functions render with.
type genCtx = genhelper.BaseContext

type (
	genStep  = genhelper.Step[genCtx]
	genSteps = genhelper.Steps[genCtx]
	genExpr  = genhelper.Gen[genCtx]
)

// ref is an argument of expr and stmt spelling the name a role refers to
// in the scope the text renders in.
type ref string

// decl is an argument of expr and stmt declaring a role in the scope the
// text renders in and spelling its name.
type decl string

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

// expr returns the text of format formatted with args. Arguments of type
// ref, decl and genExpr are rendered in the scope of the context, in the
// order they appear.
func expr(format string, args ...any) genExpr {
	return func(c genCtx) (string, error) {
		out := make([]any, len(args))
		for i, arg := range args {
			switch a := arg.(type) {
			case ref:
				name, ok := c.Scope().Lookup(string(a))
				if !ok {
					return "", fmt.Errorf("partial: %s is not declared", a)
				}
				out[i] = name
			case decl:
				out[i] = c.Scope().Declare(string(a))
			case genExpr:
				s, err := a(c)
				if err != nil {
					return "", err
				}
				out[i] = s
			default:
				out[i] = a
			}
		}
		return fmt.Sprintf(format, out...), nil
	}
}

// stmt is a step appending a line formatted as expr formats it.
func stmt(format string, args ...any) genStep {
	return text(expr(format, args...))
}

// text is a step appending a line of the text of gen.
func text(gen genExpr) genStep {
	return genhelper.FuncStep[genCtx](func(c genCtx, b []byte) (genCtx, []byte, error) {
		s, err := gen(c)
		if err != nil {
			return c, nil, err
		}
		p := c.Printer(b)
		p.Printf("%s\n", s)
		return c, p.Bytes(), nil
	})
}
