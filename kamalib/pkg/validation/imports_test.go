package validation

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestImportsStdlibOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	assert.NilError(t, err)

	fset := token.NewFileSet()
	parsed := 0

	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		assert.NilError(t, err)

		parsed++

		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			assert.NilError(t, err)

			first, _, _ := strings.Cut(path, "/")
			assert.Assert(t, !strings.Contains(first, "."), "%s imports non-stdlib %q", name, path)
		}
	}

	assert.Assert(t, parsed > 0)
}
