package dstreplace

import (
	"fmt"
	"go/ast"
	"go/token"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// Cache holds the decorated form of source files. The zero value is ready
// to use. A Cache is not safe for concurrent use.
type Cache struct {
	files map[*ast.File]decorated
}

type decorated struct {
	d   *decorator.Decorator
	err error
}

// TypeSpec returns the decorated node of ts, a type spec of file, whose
// positions fset holds. file is decorated on the first call naming it; the
// result, or the error, is kept for later calls.
//
// The returned node belongs to the cache: it must not be changed. Struct
// copies what it uses.
//
// TypeSpec returns the error of decorating file, and an error wrapping
// ErrNotFound when file does not declare ts.
func (c *Cache) TypeSpec(
	fset *token.FileSet,
	file *ast.File,
	ts *ast.TypeSpec,
) (*dst.TypeSpec, error) {
	if c.files == nil {
		c.files = map[*ast.File]decorated{}
	}
	e, ok := c.files[file]
	if !ok {
		e.d = decorator.NewDecorator(fset)
		if _, err := e.d.DecorateFile(file); err != nil {
			e = decorated{
				err: fmt.Errorf(
					"dstreplace: decorate %s: %w",
					fset.Position(file.Pos()).Filename,
					err,
				),
			}
		}
		c.files[file] = e
	}
	if e.err != nil {
		return nil, e.err
	}
	out, ok := e.d.Dst.Nodes[ts].(*dst.TypeSpec)
	if !ok {
		return nil, fmt.Errorf("%w: type spec %s", ErrNotFound, ts.Name.Name)
	}
	return out, nil
}
