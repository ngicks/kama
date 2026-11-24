package jsonschema

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// fixtures are the directories under internal/fixture. Each holds
// schema.json, the spec.yaml generating from it and the file the
// generator produces, named after the directory, which is also the
// package name.
var fixtures = []string{
	"object", "nullable", "recursive", "oneof", "anyof", "allof",
	"prefilter", "nullablecycle", "multikind", "nullableenum", "nullableroot", "productcycle",
	"oneofoverlap", "nestedunion",
}

func TestFixtureSpecBuild(t *testing.T) {
	gentest.BuildSpecs(
		t,
		opiter.WrapSeq(slices.Values(fixtures)).
			Map(fixtureDir).
			CollectSlice()...,
	)
}

// TestFixtureUpToDate runs the proc over each fixture as its spec.yaml
// does and compares the file it produces, as the merged view holds it
// after Run, with the file on disk. Run stages only a file that changed
// beyond the generated: line, so a staged file differs from disk and
// fails the comparison.
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

			file := name + ".kama_jsonschema.go"

			got, err := fs.ReadFile(f.FS(), file)
			assert.NilError(t, err)
			want, err := os.ReadFile(filepath.Join(dir, file))
			assert.NilError(t, err)
			assert.Equal(t, string(got), string(want), "run go generate ./%s", dir)

			// Load fails on any package error, so this checks the output
			// compiles.
			assert.NilError(t, gentest.Build(f, gopackages.Options{Patterns: []string{"./..."}}))
		})
	}
}

func fixtureDir(name string) string { return filepath.Join("internal", "fixture", name) }
