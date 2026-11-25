package jsonschema

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/generator/validator"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procpackages "github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

// TestSpecRegeneratesStaleFile runs the steps of a spec over a package
// whose generated file no longer compiles: it imports a package that is
// gone and calls a function no file declares. The first load keeps the
// package and warns, and the run replaces the file.
func TestSpecRegeneratesStaleFile(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/api.go":               "package api\n",
		"api/kama_jsonschema.json": procSchema,
	})
	assert.NilError(t, runSpec(t, f, nil))

	name := inline(t, "api/api.kama_jsonschema.go")

	want, err := fs.ReadFile(f.FS(), name)
	assert.NilError(t, err)

	stale := strings.Replace(
		string(want),
		"\npackage api\n",
		"\npackage api\n\nimport _ \"github.com/ngicks/kama/kamalib/jsonschemart\"\n",
		1,
	) + "\nvar _ = goneHelper()\n"
	assert.NilError(t, f.Write(context.Background(), name, []byte(stale)))

	load := gopackages.Options{Patterns: []string{"./" + inline(t, "api")}}

	_, err = gopackages.Load(context.Background(), f, load)
	assert.ErrorContains(t, err, "goneHelper")

	r := &gentest.RecordReporter{}
	assert.NilError(t, runSpec(t, f, r))
	assert.Assert(t, len(r.Warnings) > 0)

	for _, w := range r.Warnings {
		assert.ErrorIs(t, w, procpackages.ErrPackageErrors)
	}

	assert.ErrorContains(
		t,
		errors.Join(r.Warnings...),
		"could not import github.com/ngicks/kama/kamalib/jsonschemart",
	)

	got, err := fs.ReadFile(f.FS(), name)
	assert.NilError(t, err)
	assert.Equal(t, string(got), string(want))
}

// TestSpecGeneratesReferredTypes runs the steps of a spec over a package
// whose files refer to the types the run generates.
func TestSpecGeneratesReferredTypes(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/api.go":               "package api\n\nfunc Name(p Pet) string { return p.Name }\n",
		"api/kama_jsonschema.json": procSchema,
	})

	r := &gentest.RecordReporter{}
	assert.NilError(t, runSpec(t, f, r))
	assert.Equal(t, len(r.Warnings), 1, "%v", r.Warnings)
	assert.ErrorIs(t, r.Warnings[0], procpackages.ErrPackageErrors)
	assert.ErrorContains(t, r.Warnings[0], "undefined: Pet")
}

// TestSpecStrictLoadFails runs a load without allow_type_errors over a
// package whose files refer to types not generated yet.
func TestSpecStrictLoadFails(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/api.go":               "package api\n\nfunc Name(p Pet) string { return p.Name }\n",
		"api/kama_jsonschema.json": procSchema,
	})

	_, err := loadStep(
		t,
		f,
		procpackages.ProcParam{Patterns: []string{"./" + inline(t, "api")}},
		&gentest.RecordReporter{},
	)
	assert.ErrorContains(t, err, "undefined: Pet")
}

// loadInstance is an instance of packages.load.
type loadInstance interface {
	Param(p procpackages.ProcParam) error
	In(f *overlayfs.Fs)
	Out()
	AsInput() *procpackages.Packages
	Run(ctx context.Context, r workflow.Reporter) error
}

// runSpec runs over the package api procFs staged for t the steps a spec
// generating its types runs: a load allowing type errors, the proc, a load
// failing on them and validate.generate. It then checks that the package
// compiles. Every step reports to r unless r is nil.
func runSpec(t *testing.T, f *overlayfs.Fs, r workflow.Reporter) error {
	t.Helper()

	ctx := context.Background()

	if r == nil {
		r = gentest.NopReporter{}
	}

	patterns := []string{"./" + inline(t, "api")}

	pkgs, err := loadStep(t, f, procpackages.ProcParam{
		Patterns: patterns, AllowTypeErrors: true,
	}, r)
	if err != nil {
		return err
	}

	i := newInstance(t, gentest.Now())
	assert.NilError(t, i.Param(ProcParamPartial{}))
	i.In(pkgs)
	i.Out(f)

	if err := i.Run(ctx, r); err != nil {
		return err
	}

	pkgs, err = loadStep(t, f, procpackages.ProcParam{Patterns: patterns}, r)
	if err != nil {
		return err
	}

	inst, err := validator.NewProc().Instantiate(ctx, false)
	assert.NilError(t, err)

	v, ok := inst.(validateInstance)
	assert.Assert(t, ok, "%T", inst)
	assert.NilError(t, v.Param(validator.ProcParamPartial{}))
	v.In(pkgs)
	v.Out(f)

	if err := v.Run(ctx, r); err != nil {
		return err
	}

	return gentest.Build(f, gopackages.Options{Patterns: patterns})
}

// loadStep runs packages.load with param over f.
func loadStep(
	t *testing.T,
	f *overlayfs.Fs,
	param procpackages.ProcParam,
	r workflow.Reporter,
) (*procpackages.Packages, error) {
	t.Helper()

	inst, err := procpackages.NewProc().Instantiate(context.Background(), false)
	assert.NilError(t, err)

	l, ok := inst.(loadInstance)
	assert.Assert(t, ok, "%T", inst)
	assert.NilError(t, l.Param(param))
	l.In(f)

	if err := l.Run(context.Background(), r); err != nil {
		return nil, err
	}

	return l.AsInput(), nil
}
