package validator

import (
	"context"
	"go/format"
	"maps"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
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

// stage is gentest.Stage staging Go files as they are written, formatted by
// gofmtOnly.
func stage(t *testing.T, files map[string]string) (*overlayfs.Fs, gopackages.Options) {
	t.Helper()
	return gentest.Stage(t, files, overlayfs.WithFormatter(gofmtOnly{}))
}

// modulePath returns the import path of the inline test directory of t.
func modulePath(t *testing.T) string {
	return "github.com/ngicks/kama/kamalib/generator/validator/" + gentest.InlineDir(t)
}

// markers returns a marker registry of this generator only.
func markers(t *testing.T) *genmark.Registry {
	t.Helper()

	r := genmark.NewRegistry(base.ToolName)
	assert.NilError(t, r.Register(GeneratorId().String()))

	return r
}

// testRun configures a run of Generator in a test. Markers is a registry
// of this generator only when nil, and Loaded holds the directives of the
// packages only when nil. The run takes gentest.Now as its time.
type testRun struct {
	Options
	Markers *genmark.Registry
	Loaded  *LoadedOptions
}

// runGenerate runs Generator with r on pkgs, loaded from f, as a run of the
// proc does without staging. It returns the files, with absolute paths, the
// warnings of the loaded options followed by those of the run, and the
// error.
func runGenerate(
	t *testing.T,
	f *overlayfs.Fs,
	pkgs []*packages.Package,
	r testRun,
) ([]base.File, []error, error) {
	t.Helper()

	if r.Markers == nil {
		r.Markers = markers(t)
	}

	in := Input{Packages: base.SourcePackages(pkgs), Options: r.Options, Root: f.Root()}

	in.Loaded = r.Loaded
	if in.Loaded == nil {
		loaded, err := NewLoader(nil).LoadPackages(in.Packages)
		if err != nil {
			return nil, nil, loadError(err)
		}

		in.Loaded = loaded
	}

	files, warnings, err := gentest.Run(
		t.Context(), Generator(), r.Markers, gentest.Now(), in, in.Scope(), f,
	)

	return files, slices.Concat(in.Loaded.Warnings, warnings), err
}

// generate runs Generator on files with r and fails on a warning.
func generate(t *testing.T, files map[string]string, r testRun) ([]base.File, error) {
	t.Helper()

	f, opts := stage(t, files)
	pkgs, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	out, warnings, err := runGenerate(t, f, pkgs, r)
	assert.Assert(t, len(warnings) == 0, "%v", warnings)

	return out, err
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

	pkgs, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	out, warnings, err := runGenerate(t, f, pkgs, testRun{Options: genOpts})
	if err != nil {
		return nil, err
	}

	assert.Equal(t, len(warnings), 0, "%v", warnings)

	gentest.Write(t, f, out)

	return gentest.ByBase(out), nil
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

	assert.NilError(t, gentest.Build(f, opts))

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
	opts.Patterns = opiter.SlicesValues(dirs).
		Map(func(d string) string { return "./" + path.Join(gentest.InlineDir(t), d) }).
		CollectSlice()

	out, err := generateInto(t, f, opts, Options{})
	if err != nil {
		return nil, err
	}

	assert.NilError(t, gentest.Build(f, opts))

	return out, nil
}

// validateOf returns the source of the method Validate of typeName in the
// files generated from files, which must build.
func validateOf(t *testing.T, files map[string]string, file, typeName string) string {
	t.Helper()

	out := generateAndBuild(t, files)
	src, ok := out[file]
	assert.Assert(t, ok, "no %s in %v", file, slices.Sorted(maps.Keys(out)))

	fn, ok := gentest.Funcs(t, src)[typeName+".Validate"]
	assert.Assert(t, ok, "no %s.Validate in\n%s", typeName, src)

	return fn
}

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

// TestBuildStrict proves that gentest.Build fails on the mistakes the type
// check of go/packages misses under a goimports-like formatter.
func TestBuildStrict(t *testing.T) {
	cases := map[string]struct{ src, msg string }{
		"unused import":   {"package x\n\nimport \"strings\"\n", `"strings" imported and not used`},
		"unused variable": {"package x\n\nfunc A() { x := 1 }\n", "declared and not used: x"},
		"missing import":  {"package x\n\nvar _ = strings.ToUpper\n", "undefined: strings"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f, opts := stage(t, map[string]string{"a.go": tc.src})
			assert.ErrorContains(t, gentest.Build(f, opts), tc.msg)
		})
	}
}
