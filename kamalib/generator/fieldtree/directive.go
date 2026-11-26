package fieldtree

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// target is a struct type a tree is drawn for and the options of its
// directive or of its entry in the param.
type target struct {
	id    string
	obj   *types.TypeName
	key   string
	label string
	// pos tells where the target is named, for errors and warnings.
	pos string
}

// directiveTargets yields the types of pkgs tagged //kama:fieldtree, ordered
// by package path, file name and position, and the errors of the
// directives that do not parse or stand on no single type. warn takes the
// unknown keywords.
func directiveTargets(pkgs []*packages.Package, warn func(error)) iter.Seq2[target, error] {
	return func(yield func(target, error) bool) {
		for pkg, f := range sourceFiles(pkgs) {
			for spec, c := range tags(f) {
				pos := pkg.Fset.Position(c.Pos()).String()

				t, err := targetOf(pkg, spec, c, pos, warn)
				if !yield(t, err) {
					return
				}
			}
		}
	}
}

func targetOf(
	pkg *packages.Package,
	spec *ast.TypeSpec,
	c *ast.Comment,
	pos string,
	warn func(error),
) (target, error) {
	d, _ := dircmnt.Parse(c.Text)

	if spec == nil {
		return target{}, fmt.Errorf(
			"%w: %s: the directive is on no single type spec", ErrDirective, pos,
		)
	}

	t, err := parseDirective(d.Args)
	if err != nil {
		return target{}, fmt.Errorf("%w: %s: %w", ErrDirective, pos, err)
	}

	for _, k := range t.unknown {
		warn(fmt.Errorf("%w: %s: %s", ErrUnknownKey, pos, k))
	}

	obj, _ := pkg.TypesInfo.Defs[spec.Name].(*types.TypeName)
	if obj == nil {
		return target{}, fmt.Errorf("%w: %s: %s has no type information", ErrTarget, pos, spec.Name)
	}

	return target{id: t.id, obj: obj, key: t.key, label: t.label, pos: pos}, nil
}

// tags yields the //kama:fieldtree directive comments of f and the type
// spec each documents, nil for a comment on anything else.
func tags(f *ast.File) iter.Seq2[*ast.TypeSpec, *ast.Comment] {
	return func(yield func(*ast.TypeSpec, *ast.Comment) bool) {
		owners := map[*ast.CommentGroup]*ast.TypeSpec{}

		for _, decl := range f.Decls {
			d, ok := decl.(*ast.GenDecl)
			if !ok || d.Tok != token.TYPE {
				continue
			}

			if d.Doc != nil && len(d.Specs) == 1 {
				owners[d.Doc] = d.Specs[0].(*ast.TypeSpec)
			}

			for _, s := range d.Specs {
				if ts := s.(*ast.TypeSpec); ts.Doc != nil {
					owners[ts.Doc] = ts
				}
			}
		}

		for _, cg := range f.Comments {
			for _, c := range cg.List {
				d, ok := dircmnt.Parse(c.Text)
				if !ok || d.ToolName != base.ToolName || d.Directive != GeneratorId().Name {
					continue
				}

				if !yield(owners[cg], c) {
					return
				}
			}
		}
	}
}

// directive is the parsed args of a //kama:fieldtree directive.
type directive struct {
	id    string
	key   string
	label string
	// unknown are the keywords that name nothing.
	unknown []string
}

// parseDirective parses args: the id, a bare word or a string, then the
// keywords key and label.
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
		switch a.Name {
		case "":
			ids++
			if ids > 1 {
				return directive{}, fmt.Errorf("a second id %s", a.Value)
			}

			id, err := idOf(a.Value)
			if err != nil {
				return directive{}, err
			}

			d.id = id
		case "key", "label":
			s, ok := a.Value.(expr.String)
			if !ok || s.Value == "" {
				return directive{}, fmt.Errorf(
					"%s %s is no string that is not empty",
					a.Name,
					a.Value,
				)
			}

			if a.Name == "key" {
				d.key = s.Value
			} else {
				d.label = s.Value
			}
		default:
			d.unknown = append(d.unknown, a.Name)
		}
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
