package genhelper

import (
	"go/scanner"
	"go/token"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/importdecl"
)

// PrintFileHeader prints header with [PrintComment], then the package
// clause of pkgName. header holds the comment sections before the package
// clause, such as the text of [GeneratorParams.Comment] and a build
// constraint.
func PrintFileHeader(p Printer, header []string, pkgName string) {
	if printComment(p, header) {
		p.Printf("\n")
	}

	p.Printf("package %s\n\n", pkgName)
}

// PrintComment prints sections as comments. Each element is one section,
// which may span lines. An empty line separates sections, so each section
// is a comment group of its own. An empty section is skipped.
//
// A section that is Go comments as a whole, line comments or block
// comments, is printed as it is. Any other section is taken as plain text,
// and every line of it is printed after "// ".
//
// [PrintCommentGroup] prints its parts as one comment group instead.
func PrintComment(p Printer, sections []string) {
	printComment(p, sections)
}

// PrintCommentGroup prints parts as the lines of one comment group, in the
// order of parts. Each part follows the rules of one section of
// [PrintComment]: text that is Go comments as a whole stays as it is, and
// every line of other text gets "// ". No empty line separates the parts,
// so a directive line given as the last part stays in the group. An empty
// part gives one "//" line, which separates paragraphs.
//
// [Comment] is its counterpart for the texts of steps.
func PrintCommentGroup(p Printer, parts []string) {
	for _, part := range parts {
		if strings.TrimSuffix(part, "\n") == "" {
			p.Printf("//\n")
			continue
		}

		printSection(p, part)
	}
}

// IndentComment returns text as line comments whose text is indented by a
// tab, as in "//\tline", the form of a code block in a doc comment. An
// empty line of text gives a "//" line. The result is Go comments as a
// whole, so [PrintComment] and [PrintCommentGroup] keep it as it is.
//
// Plain text given to them gets "// " before each line instead, which
// leaves a space before the tab.
func IndentComment(text string) string {
	lines := opiter.WrapSeq(strings.SplitSeq(text, "\n")).
		Map(indentLine).
		CollectSlice()

	return strings.Join(lines, "\n")
}

// indentLine returns line as one line of [IndentComment].
func indentLine(line string) string {
	if line == "" {
		return "//"
	}

	return "//\t" + line
}

// WrapComment wraps text into lines that stay within width bytes once
// [PrintComment] prefixes them with "// ". A word longer than that takes a
// line of its own. An empty line separates the paragraphs of text, which
// text separates by "\n\n".
func WrapComment(text string, width int) string {
	// prefix is the length of "// " before each line.
	const prefix = 3

	var lines []string

	for i, para := range strings.Split(text, "\n\n") {
		if i > 0 {
			lines = append(lines, "")
		}

		line := ""
		for word := range strings.FieldsSeq(para) {
			if line != "" && prefix+len(line)+1+len(word) > width {
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

// printComment reports whether it printed a section.
func printComment(p Printer, sections []string) bool {
	printed := false

	for _, section := range sections {
		if section == "" {
			continue
		}

		if printed {
			p.Printf("\n")
		}

		printed = true

		printSection(p, section)
	}

	return printed
}

// printSection prints section as the comment lines [PrintComment] prints
// for one section.
func printSection(p Printer, section string) {
	commented := isComments(section)
	for line := range strings.SplitSeq(strings.TrimSuffix(section, "\n"), "\n") {
		switch {
		case commented:
			p.Printf("%s\n", line)
		case line == "":
			p.Printf("//\n")
		default:
			p.Printf("// %s\n", line)
		}
	}
}

// isComments reports whether src holds nothing but Go comments.
//
// The check scans src as a whole, since the lines inside a block comment
// carry no comment marker of their own.
func isComments(src string) bool {
	var (
		s      scanner.Scanner
		failed bool
	)

	file := token.NewFileSet().AddFile("", -1, len(src))
	s.Init(file, []byte(src), func(token.Position, string) { failed = true }, scanner.ScanComments)

	for {
		_, tok, lit := s.Scan()
		switch {
		case tok == token.EOF:
			return !failed
		case tok == token.COMMENT, tok == token.SEMICOLON && lit == "\n":
		default:
			return false
		}
	}
}

// PrintImports prints the import declaration of every import m holds,
// those the file has and those it misses. It prints nothing when m holds
// none.
//
// m knows no blank or dot import, so PrintImports prints none. A file
// needing one gets it through [AppendDecls] into a parsed file instead.
func PrintImports(p Printer, m *importdecl.Map) {
	first := true
	for imp := range m.Imports() {
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
// header, the package clause, the imports of [Context.Package], then the
// declarations. It runs last, since earlier steps record the imports they
// ask for. A context without a Package or without Imports gives no import
// declaration. The file is not formatted.
func WrapFile[C Context[C]](header []string, pkgName string) Step[C] {
	return FuncStep[C](func(c C, b []byte) (C, []byte, error) {
		p := c.Printer(nil)
		PrintFileHeader(p, header, pkgName)

		if pkg := c.Package(); pkg != nil && pkg.Imports != nil {
			PrintImports(p, pkg.Imports)
		}

		p.Printf("%s", b)

		return c, p.Bytes(), nil
	})
}
