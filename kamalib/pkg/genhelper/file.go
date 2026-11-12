package genhelper

import (
	"github.com/dave/dst"

	"github.com/ngicks/kama/kamalib/pkg/importdecl"
)

// PrintFileHeader prints the lines of header as they are, then the package
// clause of pkgName. header holds whole comment lines, such as the
// generated-code line.
func PrintFileHeader(p Printer, header []string, pkgName string) {
	for _, line := range header {
		p.Printf("%s\n", line)
	}
	if len(header) > 0 {
		p.Printf("\n")
	}
	p.Printf("package %s\n\n", pkgName)
}

// PrintImports prints the import declaration of the imports m records as
// missing. It prints nothing when m records none.
//
// For a new file, m comes from parsing no import spec, so every import the
// rendered text asked for is missing.
func PrintImports(p Printer, m *importdecl.Map) {
	first := true
	for imp := range m.Missing() {
		if first {
			p.Printf("import (\n")
			first = false
		}
		if imp.Name != "" {
			p.Printf("\t%s %q\n", imp.Name, imp.Package.Path)
		} else {
			p.Printf("\t%q\n", imp.Package.Path)
		}
	}
	if !first {
		p.Printf(")\n\n")
	}
}

// WrapFile is a step turning the declarations before it into a whole file:
// header, the package clause, the imports the steps before it asked for,
// then the declarations. It runs last, since earlier steps record the
// imports. The file is not formatted.
func WrapFile[C Context[C]](header []string, pkgName string) Step[C] {
	return FuncStep[C](func(c C, b []byte) (C, []byte, error) {
		p := c.Printer(nil)
		PrintFileHeader(p, header, pkgName)
		PrintImports(p, c.Package().Imports)
		p.Printf("%s", b)
		return c, p.Bytes(), nil
	})
}

// AppendDecls parses decls and appends them to the existing file f, then
// adds the imports pkg.Imports records as missing.
func AppendDecls(f *dst.File, pkg *Package, decls []byte) error { panic("unimplemented") }
