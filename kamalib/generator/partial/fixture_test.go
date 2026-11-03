package partial

import (
	"context"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	pkggopackages "github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	_ "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

// fixtures are the fixture directories with the params their spec.yaml
// gives the generate step.
var fixtures = []struct {
	dir   string
	param ProcParam
}{
	{dir: "internal/fixture"},
	{dir: "internal/fixturemerge", param: ProcParam{Merge: "append", MergeNested: "overwrite"}},
}

func TestFixtureSpecBuild(t *testing.T) {
	for _, fx := range fixtures {
		t.Run(fx.dir, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(fx.dir, "spec.yaml"))
			assert.NilError(t, err)

			var spec workflow.Spec
			assert.NilError(t, yaml.Unmarshal(b, &spec))

			// Build only dry-runs, so the fixture directory is not touched.
			_, err = spec.Build(context.Background(), workflow.DefaultRegistry)
			assert.NilError(t, err)
		})
	}
}

// TestFixtureUpToDate regenerates each fixture, whose generated files are
// loaded as input like any other file, and compares the staged output with
// the files on disk.
func TestFixtureUpToDate(t *testing.T) {
	for _, fx := range fixtures {
		t.Run(fx.dir, func(t *testing.T) {
			ctx := context.Background()

			f, err := overlayfs.New(fx.dir)
			assert.NilError(t, err)
			defer f.Close()

			pkgs, err := pkggopackages.Load(
				ctx, f, pkggopackages.Options{Patterns: []string{"./..."}},
			)
			assert.NilError(t, err)

			i := &instance{}
			assert.NilError(t, i.Param(fx.param))
			i.In(&gopackages.Packages{Pkgs: pkgs})
			i.Out(f)
			assert.NilError(t, i.Run(ctx))

			staged, err := f.Overlay()
			assert.NilError(t, err)

			var onDisk []string
			err = filepath.WalkDir(f.Root(), func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() &&
					(strings.HasSuffix(p, FileSuffix+".go") || filepath.Base(p) == HelpersFile ||
						filepath.Base(p) == FlagSetFile) {
					onDisk = append(onDisk, p)
				}
				return err
			})
			assert.NilError(t, err)
			assert.DeepEqual(t, slices.Sorted(maps.Keys(staged)), onDisk)

			for name, content := range staged {
				want, err := os.ReadFile(name)
				assert.NilError(t, err)
				assert.Equal(t, string(content), string(want), "run go generate ./%s", fx.dir)
			}

			// Load fails on any package error, so this checks the staged
			// output compiles.
			_, err = pkggopackages.Load(
				ctx, f, pkggopackages.Options{Patterns: []string{"./..."}},
			)
			assert.NilError(t, err)
		})
	}
}
