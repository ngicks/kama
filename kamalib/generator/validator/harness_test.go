package validator

import (
	"context"
	"go/ast"
	"go/format"
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

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// gofmtOnly is the formatter of the overlays of the tests. The default
// formatter of overlayfs works like goimports: it drops an unused import
// and adds a missing one, which would hide both mistakes of the generator
// from the type check of the staged output.
type gofmtOnly struct{}

func (gofmtOnly) Format(_ context.Context, _ string, src []byte) ([]byte, error) {
	return format.Source(src)
}

// newOverlay returns an overlay over root staging Go files as they are
// written, formatted by gofmtOnly.
func newOverlay(t *testing.T, root string) *overlayfs.Fs {
	t.Helper()

	f, err := overlayfs.New(root, overlayfs.WithFormatter(gofmtOnly{}))
	assert.NilError(t, err)
	t.Cleanup(func() { f.Close() })

	return f
}

// stage writes files, keyed by slash-separated path relative to a
// directory of its own under testdata, into an overlay only, and returns
// the overlay and the options loading their packages.
func stage(t *testing.T, files map[string]string) (*overlayfs.Fs, gopackages.Options) {
	t.Helper()

	f := newOverlay(t, ".")

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
// the test, unused imports and variables included. It returns the
// generated files keyed by base name.
func generateAndBuild(t *testing.T, files map[string]string) map[string]string {
	t.Helper()

	f, opts := stage(t, files)
	out, err := generateInto(t, f, opts, Options{})
	assert.NilError(t, err)

	assert.NilError(t, build(f, opts))

	return out
}

// generateLoading is generateAndBuild loading only the packages in dirs,
// relative to the directory stage writes files to. The other packages load
// as dependencies, outside the run. It returns the error of Generate
// instead of failing on it.
func generateLoading(
	t *testing.T,
	files map[string]string,
	dirs ...string,
) (map[string]string, error) {
	t.Helper()

	f, opts := stage(t, files)
	opts.Patterns = opiter.WrapSeq(slices.Values(dirs)).
		Map(func(d string) string { return "./" + path.Join(inlineDir(t), d) }).
		CollectSlice()

	out, err := generateInto(t, f, opts, Options{})
	if err != nil {
		return nil, err
	}

	assert.NilError(t, build(f, opts))

	return out, nil
}

// build type-checks the packages opts loads from f. go/types reports an
// unused import and an unused variable as errors, which fail the load as
// long as the overlay stages the files as written.
func build(f *overlayfs.Fs, opts gopackages.Options) error {
	_, err := gopackages.Load(context.Background(), f, opts)
	return err
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

// TestBuildStrict proves that build fails on the mistakes the type check of
// go/packages misses under a goimports-like formatter.
func TestBuildStrict(t *testing.T) {
	cases := map[string]struct{ src, msg string }{
		"unused import":   {"package x\n\nimport \"strings\"\n", `"strings" imported and not used`},
		"unused variable": {"package x\n\nfunc A() { x := 1 }\n", "declared and not used: x"},
		"missing import":  {"package x\n\nvar _ = strings.ToUpper\n", "undefined: strings"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f, opts := stage(t, map[string]string{"a.go": tc.src})
			assert.ErrorContains(t, build(f, opts), tc.msg)
		})
	}
}
