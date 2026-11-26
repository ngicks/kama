package textmod

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"iter"
	"strings"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Formats of a string value.
const (
	FormatVer      = "ver"
	FormatDateTime = "date-time"
)

// tag is a //kama:textmod directive comment and the spec it is on.
type tag struct {
	comment *ast.Comment
	args    string
	// spec is nil when the comment is on no single var or const spec, and
	// misplaced tells where it is.
	spec      *ast.ValueSpec
	misplaced string
}

// tags yields the //kama:textmod directive comments of f in the order of
// the source, function bodies included.
func tags(f *ast.File) iter.Seq[tag] {
	return func(yield func(tag) bool) {
		owners := map[*ast.CommentGroup]*ast.ValueSpec{}
		groups := map[*ast.CommentGroup]int{}

		ast.Inspect(f, func(n ast.Node) bool {
			d, ok := n.(*ast.GenDecl)
			if !ok || (d.Tok != token.CONST && d.Tok != token.VAR) {
				return true
			}

			if d.Doc != nil {
				if len(d.Specs) == 1 {
					owners[d.Doc] = d.Specs[0].(*ast.ValueSpec)
				} else {
					groups[d.Doc] = len(d.Specs)
				}
			}

			for _, s := range d.Specs {
				vs := s.(*ast.ValueSpec)
				if vs.Doc != nil {
					owners[vs.Doc] = vs
				}

				if vs.Comment != nil {
					owners[vs.Comment] = vs
				}
			}

			return true
		})

		for _, cg := range f.Comments {
			for _, c := range cg.List {
				d, ok := dircmnt.Parse(c.Text)
				if !ok || d.ToolName != base.ToolName || d.Directive != GeneratorId().Name {
					continue
				}

				t := tag{comment: c, args: d.Args, spec: owners[cg]}

				switch {
				case t.spec != nil:
				case groups[cg] > 0:
					t.misplaced = fmt.Sprintf(
						"the directive is on a group of %d specs; tag one spec of the group",
						groups[cg],
					)
				default:
					t.misplaced = "the directive is on no var or const spec"
				}

				if !yield(t) {
					return
				}
			}
		}
	}
}

// directive is the parsed args of a //kama:textmod directive.
type directive struct {
	id     string
	format string
	// unknown are the keywords that name nothing.
	unknown []string
}

// parseDirective parses args: the id, a bare word or a string, then the
// keyword format.
func parseDirective(args string) (directive, error) {
	items, err := expr.Parse(args)
	if err != nil {
		if first, _, _ := strings.Cut(args, ","); strings.Contains(first, "-") &&
			!strings.ContainsAny(first, `"'`) {
			return directive{}, fmt.Errorf(
				"%w; quote an id holding a -, as %q", err, strings.TrimSpace(first),
			)
		}

		return directive{}, err
	}

	var (
		d   directive
		ids int
	)

	for _, a := range items {
		if a.Name == "" {
			ids++

			if ids > 1 {
				return directive{}, fmt.Errorf("a second id %s", a.Value)
			}

			id, err := idOf(a.Value)
			if err != nil {
				return directive{}, err
			}

			d.id = id

			continue
		}

		if a.Name != "format" {
			d.unknown = append(d.unknown, a.Name)
			continue
		}

		s, ok := a.Value.(expr.String)
		if !ok || (s.Value != FormatVer && s.Value != FormatDateTime) {
			return directive{}, fmt.Errorf(
				"unknown format %s; the formats are %q and %q",
				a.Value, FormatVer, FormatDateTime,
			)
		}

		d.format = s.Value
	}

	if ids == 0 {
		return directive{}, errors.New("no id; the first item of the directive is the id")
	}

	return d, nil
}

func idOf(n expr.Node) (string, error) {
	switch n := n.(type) {
	case expr.Call:
		if !n.Parens {
			return n.Name, nil
		}
	case expr.String:
		if n.Value != "" {
			return n.Value, nil
		}

		return "", errors.New("an empty id")
	}

	return "", fmt.Errorf("the id %s is not a word or a string", n)
}

// literal is the value of a spec as the source spells it.
type literal struct {
	// expr is the value, its sign included.
	expr ast.Expr
	// lit is the basic literal of expr, nil for true and false.
	lit *ast.BasicLit
	// neg tells a leading -.
	neg  bool
	text string
	// typed tells a spec spelling its type.
	typed bool
}

// literalOf returns the value of spec when it is one basic literal.
func literalOf(spec *ast.ValueSpec) (literal, error) {
	switch {
	case len(spec.Names) != 1:
		return literal{}, fmt.Errorf(
			"the spec declares %d names; a tagged spec declares one",
			len(spec.Names),
		)
	case len(spec.Values) == 0:
		return literal{}, errors.New("the spec has no value")
	case len(spec.Values) != 1:
		return literal{}, fmt.Errorf("the spec has %d values", len(spec.Values))
	}

	v := spec.Values[0]
	out := literal{expr: v, typed: spec.Type != nil}

	if u, ok := v.(*ast.UnaryExpr); ok && (u.Op == token.SUB || u.Op == token.ADD) {
		if b, ok := u.X.(*ast.BasicLit); ok && (b.Kind == token.INT || b.Kind == token.FLOAT) {
			out.neg = u.Op == token.SUB
			v = b
		}
	}

	switch v := v.(type) {
	case *ast.BasicLit:
		switch v.Kind {
		case token.INT, token.FLOAT, token.STRING:
			out.lit = v
			out.text = v.Value

			if out.neg {
				out.text = "-" + v.Value
			} else if v != spec.Values[0] {
				out.text = "+" + v.Value
			}

			return out, nil
		case token.CHAR:
			return literal{}, errors.New("a rune literal is not supported")
		default:
			return literal{}, errors.New("an imaginary literal is not supported")
		}
	case *ast.Ident:
		switch v.Name {
		case "true", "false":
			out.text = v.Name
			return out, nil
		case "iota":
			return literal{}, errors.New("the value is iota; a tagged value is a literal")
		default:
			return literal{}, fmt.Errorf(
				"the value is the identifier %s; a tagged value is a literal",
				v.Name,
			)
		}
	case *ast.CallExpr:
		return literal{}, errors.New("the value is a call; a tagged value is a literal")
	case *ast.BinaryExpr:
		return literal{}, errors.New(
			"the value is a binary expression; a tagged value is a literal",
		)
	case *ast.CompositeLit:
		return literal{}, errors.New(
			"the value is a composite literal; a tagged value is a literal",
		)
	default:
		return literal{}, errors.New("the value is an expression; a tagged value is a literal")
	}
}
