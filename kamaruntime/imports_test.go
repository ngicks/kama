package kamaruntime_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/ngicks/kama/kamaruntime"

// TestImportsAllowlist keeps generated code, which imports this module, off any module but the
// standard library and this one. Tests are held to the same rule because a test dependency still
// lands in go.mod, and a consumer's module graph lists every module there.
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

		if !strings.HasSuffix(path, ".go") {
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

			if !allowed(imp) {
				t.Errorf("%s imports %q", path, imp)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if parsed == 0 {
		t.Fatal("no Go file found")
	}
}

// TestGoModRequiresNothing keeps the module free of dependencies. The check parses go.mod by hand
// since golang.org/x/mod would itself be a dependency.
func TestGoModRequiresNothing(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}

	for i, line := range strings.Split(string(data), "\n") {
		directive, _, _ := strings.Cut(strings.TrimSpace(line), "//")
		if fields := strings.Fields(directive); len(fields) > 0 && fields[0] == "require" {
			t.Errorf("go.mod:%d: %s", i+1, strings.TrimSpace(line))
		}
	}
}

func allowed(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")

	return !strings.Contains(first, ".") || imp == modulePath ||
		strings.HasPrefix(imp, modulePath+"/")
}
