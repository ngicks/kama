package validator

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// stage writes files, keyed by slash-separated path relative to a
// directory of its own under testdata, into an overlay only, and returns
// the overlay and the options loading their packages.
func stage(t *testing.T, files map[string]string) (*overlayfs.Fs, gopackages.Options) {
	t.Helper()

	f, err := overlayfs.New(".")
	assert.NilError(t, err)
	t.Cleanup(func() { f.Close() })

	dir := inlineDir(t)
	dirs := map[string]bool{}

	for name, src := range files {
		assert.NilError(t, f.Write(context.Background(), path.Join(dir, name), []byte(src)))
		dirs["./"+path.Dir(path.Join(dir, name))] = true
	}

	return f, gopackages.Options{Patterns: slices.Sorted(maps.Keys(dirs))}
}

// load type-checks files as packages of this module.
func load(t *testing.T, files map[string]string) []*packages.Package {
	t.Helper()

	f, opts := stage(t, files)
	pkgs, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	return pkgs
}

// inlineDir is the directory under testdata stage writes the files of t to.
func inlineDir(t *testing.T) string {
	return "testdata/inline/" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
}

// modulePath returns the import path of the inline test directory of t.
func modulePath(t *testing.T) string {
	return "github.com/ngicks/kama/kamalib/generator/validator/" + inlineDir(t)
}

// markers returns a marker registry of this generator only.
func markers(t *testing.T) *genmark.Registry {
	t.Helper()

	r := genmark.NewRegistry()
	assert.NilError(t, r.Register(GeneratorName))

	return r
}

// testNow is the time the header of the generated files records.
func testNow() time.Time { return time.Date(2026, 11, 4, 10, 12, 33, 0, time.UTC) }

// testOptions returns opts with the markers of this generator only and
// testNow.
func testOptions(t *testing.T, opts Options) Options {
	t.Helper()

	if opts.Markers == nil {
		opts.Markers = markers(t)
	}

	if opts.Now == nil {
		opts.Now = testNow
	}

	return opts
}

// generate runs Generate on files with opts and fails on a warning.
func generate(t *testing.T, files map[string]string, opts Options) ([]File, error) {
	t.Helper()

	out, warnings, err := Generate(load(t, files), testOptions(t, opts))
	assert.Assert(t, len(warnings) == 0, "%v", warnings)

	return out, err
}

func byBase(files []File) map[string]string {
	m := map[string]string{}
	for _, f := range files {
		m[filepath.Base(f.Path)] = string(f.Content)
	}

	return m
}

// generateInto generates from the packages opts loads from f, stages the
// output next to them and returns it keyed by base name.
func generateInto(
	t *testing.T,
	f *overlayfs.Fs,
	opts gopackages.Options,
	genOpts Options,
) (map[string]string, error) {
	t.Helper()

	ctx := context.Background()
	pkgs, err := gopackages.Load(ctx, f, opts)
	assert.NilError(t, err)

	out, warnings, err := Generate(pkgs, testOptions(t, genOpts))
	if err != nil {
		return nil, err
	}

	assert.Equal(t, len(warnings), 0, "%v", warnings)

	for _, o := range out {
		rel, err := filepath.Rel(f.Root(), o.Path)
		assert.NilError(t, err)
		assert.NilError(t, f.Write(ctx, filepath.ToSlash(rel), o.Content))
	}

	return byBase(out), nil
}

// generateAndBuild generates from files, stages the output next to them and
// loads the packages again, so that any compile error of the output fails
// the test. It returns the generated files keyed by base name.
func generateAndBuild(t *testing.T, files map[string]string) map[string]string {
	t.Helper()

	f, opts := stage(t, files)
	out, err := generateInto(t, f, opts, Options{})
	assert.NilError(t, err)

	_, err = gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	for name, src := range out {
		assertImportsUsed(t, name, src)
	}

	return out
}

// assertImportsUsed fails when src imports a package it does not refer to,
// which the type check of go/packages does not report.
func assertImportsUsed(t *testing.T, name, src string) {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
	assert.NilError(t, err)

	used := map[string]bool{}

	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}

		return true
	})

	for _, imp := range f.Imports {
		ident := path.Base(strings.Trim(imp.Path.Value, `"`))
		if imp.Name != nil {
			ident = imp.Name.Name
		}

		assert.Assert(t, used[ident], "%s imports %s without using it:\n%s", name, ident, src)
	}
}

// funcs returns the source of each function declared in src keyed by name,
// methods keyed by receiver type and name as in T.M.
func funcs(t *testing.T, src string) map[string]string {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	assert.NilError(t, err)

	out := map[string]string{}

	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}

		name := fd.Name.Name
		if fd.Recv != nil {
			recv := fd.Recv.List[0].Type
			for {
				switch x := recv.(type) {
				case *ast.StarExpr:
					recv = x.X
					continue
				case *ast.IndexExpr:
					recv = x.X
					continue
				case *ast.IndexListExpr:
					recv = x.X
					continue
				}

				break
			}

			name = recv.(*ast.Ident).Name + "." + name
		}

		out[name] = src[fset.Position(fd.Pos()).Offset:fset.Position(fd.End()).Offset]
	}

	return out
}

// validateOf returns the source of the method Validate of typeName in the
// files generated from files, which must build.
func validateOf(t *testing.T, files map[string]string, file, typeName string) string {
	t.Helper()

	out := generateAndBuild(t, files)
	src, ok := out[file]
	assert.Assert(t, ok, "no %s in %v", file, slices.Sorted(maps.Keys(out)))

	fn, ok := funcs(t, src)[typeName+".Validate"]
	assert.Assert(t, ok, "no %s.Validate in\n%s", typeName, src)

	return fn
}

var generatedLine = regexp.MustCompile(`(?m)^` + genhelper.GeneratedTimePrefix + `.*\n`)

// withoutGenerated returns src without its generated: line.
func withoutGenerated(src string) string { return generatedLine.ReplaceAllString(src, "") }

// assertContains fails unless src holds every one of subs.
func assertContains(t *testing.T, src string, subs ...string) {
	t.Helper()

	for _, s := range subs {
		assert.Assert(t, strings.Contains(src, s), "want %q in\n%s", s, src)
	}
}

// assertNotContains fails when src holds one of subs.
func assertNotContains(t *testing.T, src string, subs ...string) {
	t.Helper()

	for _, s := range subs {
		assert.Assert(t, !strings.Contains(src, s), "want no %q in\n%s", s, src)
	}
}
