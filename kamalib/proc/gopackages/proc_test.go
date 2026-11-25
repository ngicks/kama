package gopackages

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

func TestProcParam(t *testing.T) {
	i := &instance{}

	assert.NilError(t, i.Param(ProcParam{}))
	assert.DeepEqual(t, i.param.Patterns, []string{"./..."})

	assert.ErrorContains(t, i.Param(ProcParam{Patterns: []string{""}}), "empty pattern")
}

// brokenModule holds a package referring to a declaration no file has and
// one importing a package that does not exist.
var brokenModule = map[string]string{
	"a/a.go": "package a\n\nfunc A() int { return Gen() }\n",
	"b/b.go": "package b\n\nimport \"example.invalid/x/gone\"\n\nvar _ = gone.F\n",
	"c/c.go": "package c\n",
}

func TestRunAllowTypeErrors(t *testing.T) {
	i := &instance{}
	assert.NilError(t, i.Param(ProcParam{AllowTypeErrors: true}))
	i.In(stageModule(t, brokenModule))

	r := &recorder{}
	assert.NilError(t, i.Run(t.Context(), r))

	paths := opiter.WrapSeq(slices.Values(i.AsInput().Pkgs)).
		Map(func(p *packages.Package) string { return p.PkgPath }).
		Collect(slices.Sorted)

	assert.DeepEqual(t, paths, []string{
		"example.invalid/x/a", "example.invalid/x/b", "example.invalid/x/c",
	})

	warned := opiter.WrapSeq(slices.Values(r.warnings)).
		Map(warnedPath).
		CollectSlice()

	assert.DeepEqual(t, warned, []string{
		"example.invalid/x/a", "example.invalid/x/b", "example.invalid/x/gone",
	})
	assert.ErrorContains(t, r.warnings[0], "undefined: Gen")
}

func TestRunFailsOnTypeErrors(t *testing.T) {
	i := &instance{}
	assert.NilError(t, i.Param(ProcParam{}))
	i.In(stageModule(t, brokenModule))

	r := &recorder{}
	err := i.Run(t.Context(), r)

	var pkgErrs gopackages.PackageErrors
	assert.Assert(t, errors.As(err, &pkgErrs), "err: %v", err)
	assert.Assert(t, i.AsInput().Pkgs == nil)
	assert.Equal(t, len(r.warnings), 0, "%v", r.warnings)
}

// warnedPath returns the path of the package a warning of Run names.
func warnedPath(w error) string {
	var pkgErrs gopackages.PackageErrors
	if !errors.Is(w, ErrPackageErrors) || !errors.As(w, &pkgErrs) || len(pkgErrs) != 1 {
		return "unexpected warning: " + w.Error()
	}

	return slices.Collect(maps.Keys(pkgErrs))[0]
}

type recorder struct{ warnings []error }

func (r *recorder) Warn(w error)    { r.warnings = append(r.warnings, w) }
func (r *recorder) Progress(string) {}

// stageModule writes files, keyed by slash-separated path, and a go.mod of
// the module example.invalid/x into a temporary directory and opens it.
func stageModule(t *testing.T, files map[string]string) *overlayfs.Fs {
	t.Helper()

	root := t.TempDir()
	files = maps.Clone(files)
	files["go.mod"] = "module example.invalid/x\n\ngo 1.27\n"

	for name, src := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		assert.NilError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		assert.NilError(t, os.WriteFile(p, []byte(src), 0o644))
	}

	f, err := overlayfs.New(root)
	assert.NilError(t, err)
	t.Cleanup(func() { f.Close() })

	return f
}
