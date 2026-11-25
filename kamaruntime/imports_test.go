package kamaruntime_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

const modulePath = "github.com/ngicks/kama/kamaruntime"

// TestImportsAllowlist keeps generated code, which imports this module, off any module but the
// standard library and this one. Tests may import more, since a consumer never builds them.
func TestImportsAllowlist(t *testing.T) {
	fset := token.NewFileSet()
	parsed := 0

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if path != "." && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		parsed++

		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}

			assert.Assert(t, allowed(imp), "%s imports %q", path, imp)
		}

		return nil
	})
	assert.NilError(t, err)
	assert.Assert(t, parsed > 0)
}

func allowed(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")

	return !strings.Contains(first, ".") || imp == modulePath ||
		strings.HasPrefix(imp, modulePath+"/")
}
