package partial

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/und"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	pkggopackages "github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs/suffix"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	_ "github.com/ngicks/kama/kamalib/proc/overlayfs"
)

// fixtures are the fixture directories with the params their spec.yaml
// gives the generate step.
var fixtures = []fixture{
	{dir: "internal/fixture"},
	{
		dir: "internal/fixturemerge",
		param: ProcParamPartial{
			MergeStrategy:       und.Defined("append"),
			MergeNestedStrategy: und.Defined("overwrite"),
		},
	},
	{
		dir:   "internal/fixtureexternal",
		param: ProcParamPartial{OptionFile: und.Defined(fileloader.PathInput{Base: "partial"})},
	},
	{dir: "internal/fixturepackage"},
	{dir: "internal/fixturetest", tests: true},
}

type fixture struct {
	dir   string
	param ProcParamPartial
	// tests mirrors the tests param of the load step of spec.yaml.
	tests bool
}

func TestFixtureSpecBuild(t *testing.T) {
	gentest.BuildSpecs(
		t,
		opiter.WrapSeq(slices.Values(fixtures)).
			Map(func(fx fixture) string { return fx.dir }).
			CollectSlice()...,
	)
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

			loadOpts := pkggopackages.Options{Patterns: []string{"./..."}, Tests: fx.tests}
			pkgs, err := pkggopackages.Load(ctx, f, loadOpts)
			assert.NilError(t, err)

			i := newInstance(t, proc)
			assert.NilError(t, i.Param(fx.param))
			i.In(&gopackages.Packages{Pkgs: pkgs})
			i.Out(f)
			files, err := i.Files(ctx, gentest.NopReporter{})
			assert.NilError(t, err)

			r := &gentest.RecordReporter{}
			assert.NilError(t, i.Run(ctx, r))
			assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)

			produced := opiter.WrapSeq(slices.Values(files)).
				Map(
					func(file File) string { return filepath.Join(f.Root(), filepath.FromSlash(file.Path)) },
				).
				Collect(slices.Sorted)

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
			_, err = pkggopackages.Load(ctx, f, loadOpts)
			assert.NilError(t, err)
		})
	}
}

// generatedFiles returns the generated files under root, sorted.
func generatedFiles(t *testing.T, root string) []string {
	t.Helper()

	var out []string

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(p) == ".go" &&
			suffix.IsSuffixed(filepath.Base(p), FileSuffix) {
			out = append(out, p)
		}

		return err
	})
	assert.NilError(t, err)

	return out
}

// TestFixtureFileParity checks that option files generate what the
// directives of internal/fixture do. The packages of each fixture copy
// those of internal/fixture without directives, so every generated file
// matches its counterpart once the import paths are replaced. The headers
// differ in the time of the run and in the option files.
//
// internal/fixtureexternal gives the options in its central partial.yaml.
// internal/fixturepackage gives them in package option files: one found
// by its default name, and one named by //kama:option-file with a path
// into another directory.
func TestFixtureFileParity(t *testing.T) {
	for _, fileDir := range []string{"internal/fixtureexternal", "internal/fixturepackage"} {
		t.Run(fileDir, func(t *testing.T) { testFixtureFileParity(t, fileDir) })
	}
}

func testFixtureFileParity(t *testing.T, fileDir string) {
	const (
		directiveDir = "internal/fixture"
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
	// Helper names hash the types they convert, import paths included,
	// and a helpers file declares them sorted by name. Helpers are compared
	// as a set with the hashes left out, the rest of each file as is.
	hash := regexp.MustCompile(
		`(kamaPartial[A-Za-z]+_)[0-9a-f]{10}_[0-9a-f]{10}(_[0-9a-f]{1,2})?\b`,
	)
	hideHash := func(src string) string { return hash.ReplaceAllString(src, "${1}<hash>") }
	normalize := func(src string) string {
		rest, helpers := splitHelpers(t, src)

		hidden := opiter.WrapSeq(slices.Values(helpers)).
			Map(hideHash).
			Collect(slices.Sorted)

		return hideHash(rest) + "\n" + strings.Join(hidden, "\n")
	}

	for _, name := range names {
		want, err := os.ReadFile(filepath.Join(directiveDir, name))
		assert.NilError(t, err)
		got, err := os.ReadFile(filepath.Join(fileDir, name))
		assert.NilError(t, err)
		assert.Equal(
			t,
			normalize(withoutRunParams(string(got))),
			normalize(withoutRunParams(replacer.Replace(string(want)))),
			name,
		)
	}
}

// splitHelpers returns src without the declarations of helpers and those
// declarations, each with its doc comment.
func splitHelpers(t *testing.T, src string) (string, []string) {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	assert.NilError(t, err)

	var (
		rest    strings.Builder
		helpers []string
		last    int
	)

	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || !strings.HasPrefix(fd.Name.Name, "kamaPartial") {
			continue
		}

		start, end := fset.Position(fd.Doc.Pos()).Offset, fset.Position(fd.End()).Offset
		rest.WriteString(src[last:start])
		helpers = append(helpers, src[start:end])
		last = end
	}

	rest.WriteString(src[last:])

	blank := regexp.MustCompile(`\n{3,}`)

	return blank.ReplaceAllString(rest.String(), "\n\n"), helpers
}
