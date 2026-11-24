package base_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// TestExampleMatchesDoc keeps the walkthrough of the doc compiled and run:
// each code block of the doc from its first example up to its limits is
// code of the generators of internal/example or of the spec their test
// runs.
func TestExampleMatchesDoc(t *testing.T) {
	f, err := parser.ParseFile(
		token.NewFileSet(), "doc.go", nil, parser.PackageClauseOnly|parser.ParseComments,
	)
	assert.NilError(t, err)

	doc := f.Doc.Text()
	start := strings.Index(doc, "\n# Example: ")
	end := strings.Index(doc, "\n# Limits\n")
	assert.Assert(t, start >= 0 && end > start, "no example in\n%s", doc)

	names, err := filepath.Glob("internal/example/*/*.go")
	assert.NilError(t, err)

	tests, err := filepath.Glob("internal/example/*_test.go")
	assert.NilError(t, err)

	var src strings.Builder

	for _, name := range append(names, tests...) {
		b, err := os.ReadFile(name)
		assert.NilError(t, err)
		src.Write(b)
	}

	blocks := codeBlocks(doc[start:end])
	assert.Assert(t, len(blocks) >= 7, "%d blocks", len(blocks))

	// A block may show code nested in a function, so indentation does not
	// count.
	for _, block := range blocks {
		assert.Assert(t, strings.Contains(dedent(src.String()), dedent(block)), "want\n%s", block)
	}
}

// dedent returns s without the indentation of its lines.
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimLeft(line, "\t")
	}

	return strings.Join(lines, "\n")
}

// codeBlocks returns the code blocks of section, a part of a doc comment,
// without their indentation.
func codeBlocks(section string) []string {
	var (
		blocks []string
		block  []string
	)

	flush := func() {
		if len(block) > 0 {
			blocks = append(blocks, strings.TrimRight(strings.Join(block, "\n"), "\n"))
		}

		block = nil
	}

	for line := range strings.SplitSeq(section, "\n") {
		switch {
		case strings.HasPrefix(line, "\t"):
			block = append(block, strings.TrimPrefix(line, "\t"))
		case line == "" && len(block) > 0:
			block = append(block, "")
		default:
			flush()
		}
	}

	flush()

	return blocks
}
