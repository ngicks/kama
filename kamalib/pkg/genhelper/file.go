package genhelper

import (
	"github.com/dave/dst"
)

// File assembles a whole Go source file from the declarations a chain
// rendered.
type File struct {
	// Header lines are written before the package clause, such as the
	// generated-code line.
	Header  []string
	PkgName string
	Dest    *Dest
}

// Render returns the file holding decls: the header, the package clause,
// the imports Dest.Imports records as missing, then decls. The result is
// formatted.
func (f File) Render(decls []byte) ([]byte, error) { panic("unimplemented") }

// AppendDecls parses decls and appends them to the existing file f, then
// adds the imports d.Imports records as missing.
func AppendDecls(f *dst.File, d *Dest, decls []byte) error { panic("unimplemented") }
