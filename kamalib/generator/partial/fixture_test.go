package partial

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
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
	{
		dir:   "internal/fixtureexternal",
		param: ProcParam{OptionFile: &fileloader.PathInput{Base: "partial"}},
	},
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
// loaded as input like any other file, and compares the files Generate
// produces, as the merged view holds them after Run, with the files on
// disk. Run stages only the files that changed beyond the generated: line,
// so a staged file differs from disk and fails the comparison.
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
			files, _, err := i.generate()
			assert.NilError(t, err)
			r := &recordReporter{}
			assert.NilError(t, i.Run(ctx, r))
			assert.Equal(t, len(r.warnings), 0, "%v", r.warnings)

			var produced []string
			for _, file := range files {
				produced = append(produced, file.Path)
			}
			slices.Sort(produced)
			assert.DeepEqual(t, produced, generatedFiles(t, f.Root()))

			for _, name := range produced {
				rel, err := filepath.Rel(f.Root(), name)
				assert.NilError(t, err)
				got, err := fs.ReadFile(f.FS(), filepath.ToSlash(rel))
				assert.NilError(t, err)
				want, err := os.ReadFile(name)
				assert.NilError(t, err)
				assert.Equal(t, string(got), string(want), "run go generate ./%s", fx.dir)
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

// generatedFiles returns the generated files under root, sorted.
func generatedFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() &&
			(strings.HasSuffix(p, FileSuffix+".go") || filepath.Base(p) == HelpersFile ||
				filepath.Base(p) == FlagSetFile) {
			out = append(out, p)
		}
		return err
	})
	assert.NilError(t, err)
	return out
}

// TestFixtureExternalParity checks that the options of
// internal/fixtureexternal/partial.yaml generate what the directives of
// internal/fixture do: its packages copy those of internal/fixture without
// directives, so every generated file matches its counterpart once the
// import paths are replaced. The headers differ in the time of the run and
// in the option file.
func TestFixtureExternalParity(t *testing.T) {
	const (
		directiveDir = "internal/fixture"
		fileDir      = "internal/fixtureexternal"
		pkgPath      = "github.com/ngicks/kama/kamalib/generator/partial/"
	)
	rel := func(root string) []string {
		var out []string
		for _, p := range generatedFiles(t, root) {
			r, err := filepath.Rel(root, p)
			assert.NilError(t, err)
			out = append(out, filepath.ToSlash(r))
		}
		return out
	}
	names := rel(directiveDir)
	assert.Assert(t, len(names) > 0)
	assert.DeepEqual(t, rel(fileDir), names)

	replacer := strings.NewReplacer(pkgPath+directiveDir+"/", pkgPath+fileDir+"/")
	for _, name := range names {
		want, err := os.ReadFile(filepath.Join(directiveDir, name))
		assert.NilError(t, err)
		got, err := os.ReadFile(filepath.Join(fileDir, name))
		assert.NilError(t, err)
		assert.Equal(
			t,
			withoutRunParams(string(got)),
			withoutRunParams(replacer.Replace(string(want))),
			name,
		)
	}
}

// nopReporter drops what an instance reports.
type nopReporter struct{}

func (nopReporter) Warn(error)      {}
func (nopReporter) Progress(string) {}
