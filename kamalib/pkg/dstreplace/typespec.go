package dstreplace

import (
	"fmt"
	"go/ast"
	"go/token"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// TypeSpec decorates file, whose positions fset holds, and returns the
// decorated node of ts, a type spec of file. Every call decorates the file
// again, so the returned node belongs to the caller.
//
// TypeSpec returns the error of decorating file, and an error wrapping
// ErrNotFound when file does not declare ts.
func TypeSpec(fset *token.FileSet, file *ast.File, ts *ast.TypeSpec) (*dst.TypeSpec, error) {
	d := decorator.NewDecorator(fset)
	if _, err := d.DecorateFile(file); err != nil {
		return nil, fmt.Errorf(
			"dstreplace: decorate %s: %w", fset.Position(file.Pos()).Filename, err,
		)
	}

	out, ok := d.Dst.Nodes[ts].(*dst.TypeSpec)
	if !ok {
		return nil, fmt.Errorf("%w: type spec %s", ErrNotFound, ts.Name.Name)
	}

	return out, nil
}
