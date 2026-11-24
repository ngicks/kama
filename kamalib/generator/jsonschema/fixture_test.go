package jsonschema

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/generator/validator"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procpackages "github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

// fixtures are the directories under internal/fixture. Each holds
// schema.json, the spec.yaml generating from it and the files the
// generators produce: the file of this generator, named after the
// directory, which is also the package name, and the files of the
// validator generator.
var fixtures = []string{
	"object", "nullable", "recursive", "oneof", "anyof", "allof",
	"prefilter", "nullablecycle", "multikind", "nullableenum", "nullableroot", "productcycle",
	"oneofoverlap", "nestedunion", "validation", "rootscalar", "rawroot", "subschema",
	"typedkeywords",
}

func TestFixtureSpecBuild(t *testing.T) {
	gentest.BuildSpecs(
		t,
		opiter.WrapSeq(slices.Values(fixtures)).
			Map(fixtureDir).
			CollectSlice()...,
	)
}

// validateInstance is the instance of validate.generate.
type validateInstance interface {
	Param(p validator.ProcParamPartial) error
	In(pkgs *procpackages.Packages)
	Out(f *overlayfs.Fs)
	Run(ctx context.Context, r workflow.Reporter) error
}

// TestFixtureUpToDate runs the steps of each spec.yaml, the proc and then
// validate.generate over the packages loaded with its output, and compares
// the files they produce, as the merged view holds them after Run, with the
// files on disk. Run stages only a file that changed beyond the generated:
// line, so a staged file differs from disk and fails the comparison.
func TestFixtureUpToDate(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			dir := fixtureDir(name)

			f, err := overlayfs.New(dir)
			assert.NilError(t, err)

			defer f.Close()

			i := &instance{}
			assert.NilError(t, i.Param(procParam("schema.json", "", "", "")))
			i.Out(f)

			r := &gentest.RecordReporter{}
			assert.NilError(t, i.Run(ctx, r))
			assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)

			load := gopackages.Options{Patterns: []string{"./..."}}

			pkgs, err := gopackages.Load(ctx, f, load)
			assert.NilError(t, err)

			inst, err := validator.NewProc().Instantiate(ctx, false)
			assert.NilError(t, err)

			v, ok := inst.(validateInstance)
			assert.Assert(t, ok, "%T", inst)
			assert.NilError(t, v.Param(validator.ProcParamPartial{}))
			v.In(&procpackages.Packages{Pkgs: pkgs})
			v.Out(f)
			assert.NilError(t, v.Run(ctx, r))
			assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)

			produced := generatedFiles(t, f.FS())
			assert.Assert(t, slices.Contains(produced, name+".kama_jsonschema.go"), "%v", produced)

			onDisk := generatedFiles(t, os.DirFS(dir))
			assert.DeepEqual(t, produced, onDisk)

			for _, file := range produced {
				got, err := fs.ReadFile(f.FS(), file)
				assert.NilError(t, err)
				want, err := os.ReadFile(filepath.Join(dir, file))
				assert.NilError(t, err)
				assert.Equal(t, string(got), string(want), "run go generate ./%s", dir)
			}

			// Load fails on any package error, so this checks the output
			// compiles.
			assert.NilError(t, gentest.Build(f, load))
		})
	}
}

// generatedFiles returns the names of the files of fsys, a fixture
// directory, that a generator of kama writes, sorted.
func generatedFiles(t *testing.T, fsys fs.FS) []string {
	t.Helper()

	entries, err := fs.ReadDir(fsys, ".")
	assert.NilError(t, err)

	return opiter.WrapSeq(slices.Values(entries)).
		Map(fs.DirEntry.Name).
		Filter(func(n string) bool { return strings.Contains(n, ".kama_") }).
		Collect(slices.Sorted)
}

func fixtureDir(name string) string { return filepath.Join("internal", "fixture", name) }
