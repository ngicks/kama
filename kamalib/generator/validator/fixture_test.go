package validator

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/und"
	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	pkggopackages "github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs/suffix"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	_ "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

// fixtures are the fixture directories with the params their spec.yaml
// gives the generate step. The runtime tests under internal/tests call the
// generated code of internal/fixture.
var fixtures = []struct {
	dir   string
	param ProcParamPartial
}{
	{dir: "internal/fixture"},
	{
		dir:   "internal/fixturefile",
		param: ProcParamPartial{OptionFile: und.Defined(fileloader.PathInput{Base: "validate"})},
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

			opts := pkggopackages.Options{Patterns: []string{"./..."}}
			pkgs, err := pkggopackages.Load(ctx, f, opts)
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

			produced := opiter.WrapSeq(slices.Values(files)).
				Map(func(file File) string { return filepath.FromSlash(file.Path) }).
				Collect(slices.Sorted)

			assert.DeepEqual(t, produced, generatedFiles(t, f.Root()))

			for _, name := range produced {
				got, err := fs.ReadFile(f.FS(), filepath.ToSlash(name))
				assert.NilError(t, err)
				want, err := os.ReadFile(filepath.Join(f.Root(), name))
				assert.NilError(t, err)
				assert.Equal(t, string(got), string(want), "run go generate ./%s", fx.dir)
			}

			// Load fails on any package error, so this checks the staged
			// output compiles.
			_, err = pkggopackages.Load(ctx, f, opts)
			assert.NilError(t, err)
		})
	}
}

// TestFixtureFileParity checks that option files generate what the
// directives of internal/fixture/parity do. internal/fixturefile/parity
// copies that package without directives. Its central option file
// validate.yaml gives the options of one type and its package option file
// kama_validate.yaml those of the other. Every generated file matches its
// counterpart once the import paths are replaced. The headers differ in
// the time of the run and in the option files.
func TestFixtureFileParity(t *testing.T) {
	const (
		directiveDir = "internal/fixture/parity"
		fileDir      = "internal/fixturefile/parity"
		pkgPath      = "github.com/ngicks/kama/kamalib/generator/validator/"
	)

	names := generatedFiles(t, directiveDir)
	assert.Assert(t, len(names) > 0)
	assert.DeepEqual(t, generatedFiles(t, fileDir), names)

	runParams := regexp.MustCompile(`(?m)^//\t(option_file|package_option_file): .*\n`)
	normalize := func(src string) string {
		return withoutGenerated(runParams.ReplaceAllString(src, ""))
	}

	for _, name := range names {
		want, err := os.ReadFile(filepath.Join(directiveDir, name))
		assert.NilError(t, err)
		got, err := os.ReadFile(filepath.Join(fileDir, name))
		assert.NilError(t, err)

		replaced := strings.ReplaceAll(string(want), pkgPath+directiveDir, pkgPath+fileDir)
		assert.Equal(t, normalize(string(got)), normalize(replaced), name)
	}
}

// generatedFiles returns the files of the generator under root, relative
// to root and sorted.
func generatedFiles(t *testing.T, root string) []string {
	t.Helper()

	var out []string

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !suffix.IsSuffixed(filepath.Base(p), naming.Suffix()) {
			return err
		}

		rel, err := filepath.Rel(root, p)
		out = append(out, rel)

		return err
	})
	assert.NilError(t, err)

	return out
}

// recordReporter records what an instance reports.
type recordReporter struct {
	warnings []error
	progress []string
}

func (r *recordReporter) Warn(w error)        { r.warnings = append(r.warnings, w) }
func (r *recordReporter) Progress(msg string) { r.progress = append(r.progress, msg) }
