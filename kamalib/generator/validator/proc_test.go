package validator

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/opt"
	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procpackages "github.com/ngicks/kama/kamalib/proc/gopackages"
	procoverlayfs "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

func TestProcParam(t *testing.T) {
	assert.Assert(t, DefaultProcParam().ToValue().OptionFile.IsNone())

	set := opt.Some(fileloader.PathInput{
		Base: "validate", Formats: []fileloader.Format{fileloader.FormatYaml},
	})

	cases := []struct {
		name string
		json string
		want opt.Option[fileloader.PathInput]
	}{
		{"absent", `{}`, opt.None[fileloader.PathInput]()},
		{"null", `{"option_file": null}`, opt.None[fileloader.PathInput]()},
		{"set", `{"option_file": {"base": "validate", "formats": ["yaml"]}}`, set},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p ProcParamPartial
			assert.NilError(t, json.Unmarshal([]byte(tc.json), &p))

			got := DefaultProcParam().Merge(p).ToValue().OptionFile
			assert.Equal(t, got.IsSome(), tc.want.IsSome())
			assert.DeepEqual(t, got.Value(), tc.want.Value())
		})
	}
}

func TestProcParamOptionFile(t *testing.T) {
	param := func(in fileloader.PathInput) ProcParamPartial {
		var p ProcParamPartial
		p.FromValue(ProcParam{OptionFile: opt.Some(in)})

		return p
	}

	i := newInstance(t, NewProc())
	assert.NilError(t, i.Param(ProcParamPartial{}))

	err := i.Param(param(fileloader.PathInput{}))
	assert.Error(t, err, "option_file: base is required")

	err = i.Param(
		param(fileloader.PathInput{Base: "validate", Formats: []fileloader.Format{"ini"}}),
	)
	assert.ErrorIs(t, err, fileloader.ErrUnknownFormat)

	assert.NilError(t, i.Param(param(fileloader.PathInput{Base: "validate"})))
}

// procInstance is an instance of the proc of NewProc.
type procInstance interface {
	Param(p ProcParamPartial) error
	In(pkgs *procpackages.Packages)
	Out(f *overlayfs.Fs)
	Run(ctx context.Context, r workflow.Reporter) error
	Files(ctx context.Context, r workflow.Reporter) ([]base.File, error)
}

func newInstance(t *testing.T, p workflow.Proc) procInstance {
	t.Helper()

	inst, err := p.Instantiate(context.Background(), false)
	assert.NilError(t, err)

	i, ok := inst.(procInstance)
	assert.Assert(t, ok, "%T", inst)

	return i
}

// TestFileNames pins the exported file name and the package option file to
// the naming of the generator.
func TestFileNames(t *testing.T) {
	assert.Equal(t, HelpersFile, naming.SharedFile(helperKind))
	assert.Equal(t, NewLoader(nil).PackageFile.(fileloader.PathInput).Base, naming.Name)
}

// procProject writes files into a new module example.invalid/x that
// resolves this module and the runtime module next to it from their
// directories, and returns the root.
func procProject(t *testing.T, files map[string]string) string {
	t.Helper()

	kamalib, err := filepath.Abs("../..")
	assert.NilError(t, err)

	kamaruntime := filepath.Join(kamalib, "..", "kamaruntime")

	// A go line below that of this module would need go mod tidy.
	gomod, err := os.ReadFile(filepath.Join(kamalib, "go.mod"))
	assert.NilError(t, err)

	goLine := opiter.WrapSeq(strings.Lines(string(gomod))).
		Filter(func(l string) bool { return strings.HasPrefix(l, "go ") }).
		First()
	assert.Assert(t, goLine.IsSome())

	root := t.TempDir()
	files["go.mod"] = "module example.invalid/x\n\n" + goLine.Value() + "\n" +
		"require (\n" +
		"\tgithub.com/ngicks/kama/kamalib v0.0.0\n" +
		"\tgithub.com/ngicks/kama/kamaruntime v0.0.0\n" +
		")\n\n" +
		"replace (\n" +
		"\tgithub.com/ngicks/kama/kamalib => " + kamalib + "\n" +
		"\tgithub.com/ngicks/kama/kamaruntime => " + kamaruntime + "\n" +
		")\n"

	for name, src := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		assert.NilError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		assert.NilError(t, os.WriteFile(p, []byte(src), 0o644))
	}

	return root
}

// runProcSpec runs the graph opening root, loading its packages,
// generating with the validate.generate param and copying the result down.
func runProcSpec(t *testing.T, root, param string) error {
	t.Helper()

	return runProcSpecWith(t, workflow.DefaultRegistry, root, param)
}

// runProcSpecWith is runProcSpec building the graph from reg.
func runProcSpecWith(t *testing.T, reg *workflow.Registry, root, param string) error {
	t.Helper()

	var spec workflow.Spec
	assert.NilError(t, yaml.Unmarshal(fmt.Appendf(nil, `
version: 1
steps:
  root: {proc: overlayfs.open, param: {root: %q}}
  load: {proc: packages.load, in: root}
  generate: {proc: validate.generate, in: load, out: root, param: %s}
  final: {proc: overlayfs.copy_down, in: root, after: generate}
`, root, param), &spec))

	ctx := context.Background()

	g, err := spec.Build(ctx, reg)
	assert.NilError(t, err)

	return g.Run(ctx, workflow.ProgressControl{})
}

// procSrc declares a marked type with a validator.
const procSrc = `package a

//kama:validate
//kama:validate hook=False
type A struct {
	//kama:validate length(ge=1)
	Name string ` + "`json:\"name\"`" + `
}
`

// TestProcRun runs the proc in a workflow graph. copy_down type-checks the
// staged output before it writes the project directory.
func TestProcRun(t *testing.T) {
	root := procProject(t, map[string]string{
		"a/a.go": procSrc,
		"b/b.go": "package b\n\nimport \"example.invalid/x/a\"\n\ntype B struct{ As []a.A }\n",
		"validate.yaml": "version: 1\npackages:\n  example.invalid/x/b:\n    types:\n" +
			"      B:\n        hook: false\n",
	})

	assert.NilError(t, runProcSpec(t, root, `{option_file: {base: validate}}`))

	tree := readTree(t, root)
	assert.Assert(t, strings.Contains(tree["a/a.kama_validate.go"], "func (v A) Validate("))
	assert.Assert(t, strings.Contains(tree["a/"+HelpersFile], "func kamaValidateLength("))
	assert.Assert(t, strings.Contains(tree["b/b.kama_validate.go"], "func (v B) Validate("))
	assert.Assert(t, strings.Contains(tree["b/b.kama_validate.go"], `option_file: "validate.yaml"`))

	// A second run finds nothing to change.
	assert.NilError(t, runProcSpec(t, root, `{option_file: {base: validate}}`))
	assert.DeepEqual(t, readTree(t, root), tree)

	// A null option_file reads no option file. B is still a dependant, and
	// its header names no option file.
	assert.NilError(t, runProcSpec(t, root, `{option_file: null}`))
	assert.Assert(
		t,
		strings.Contains(readTree(t, root)["b/b.kama_validate.go"], "option_file: null"),
	)
}

// TestProcRunWithRegistry runs a graph whose validate.generate proc
// generates with a registry holding a validator of the test.
func TestProcRunWithRegistry(t *testing.T) {
	reg := ext.DefaultRegistry().Clone()
	reg.Register(positive{})

	procs := workflow.NewRegistry()
	for kind, newProc := range map[string]func() workflow.Proc{
		"overlayfs.open":      procoverlayfs.NewOpenProc,
		"overlayfs.copy_down": procoverlayfs.NewCopyDownProc,
		"packages.load":       procpackages.NewProc,
		"validate.generate":   NewProcWith(reg),
	} {
		assert.NilError(t, procs.Register(kind, newProc))
	}

	// A validator registered after NewProcWith does not reach the procs.
	reg.Register(lengthOne{})

	root := procProject(t, map[string]string{
		"a/a.go": "package a\n\n//kama:validate\n//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate positive\n\tN int\n" +
			"\t//kama:validate length(ge=1)\n\tS string\n}\n",
	})

	assert.NilError(t, runProcSpecWith(t, procs, root, `{}`))

	tree := readTree(t, root)
	assertContains(t, tree["a/a.kama_validate.go"], "kamaValidatePositive(int64(v.N))")
	assertNotContains(t, tree["a/a.kama_validate.go"], "kamaValidateLengthOne")
	assertContains(t, tree["a/"+HelpersFile], "func kamaValidatePositive(n int64) error {")

	// The default registry knows no positive.
	err := runProcSpec(t, root, `{}`)
	assert.ErrorIs(t, err, ErrDirective)
	assert.ErrorContains(t, err, "unknown validator")
}

// TestProcRunFails leaves the project directory untouched when the
// generate step fails.
func TestProcRunFails(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		param string
		is    error
		msg   string
	}{
		{
			name: "missing hook",
			files: map[string]string{
				"a/a.go": "package a\n\n//kama:validate\ntype A struct{ N int }\n",
			},
			param: `{}`,
			is:    ErrHook,
			msg:   "has no hook validateN",
		},
		{
			name:  "missing option file",
			files: map[string]string{"a/a.go": procSrc},
			param: `{option_file: {base: missing}}`,
			is:    fileloader.ErrNotFound,
			msg:   `"missing"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := procProject(t, tc.files)
			before := readTree(t, root)

			err := runProcSpec(t, root, tc.param)
			assert.ErrorIs(t, err, workflow.ErrRun)
			assert.ErrorIs(t, err, tc.is)
			assert.ErrorContains(t, err, `step "generate"`)
			assert.ErrorContains(t, err, tc.msg)

			assert.DeepEqual(t, readTree(t, root), before)
		})
	}
}

// readTree returns the contents of the files under root keyed by
// slash-separated path relative to root.
func readTree(t *testing.T, root string) map[string]string {
	t.Helper()

	out := map[string]string{}

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = string(b)

		return err
	})
	assert.NilError(t, err)

	return out
}

// TestProcRunRejectsPackagesWithErrors runs the proc over packages a load
// allowing type errors kept, whose types the generator cannot trust.
func TestProcRunRejectsPackagesWithErrors(t *testing.T) {
	f, load := gentest.Stage(t, map[string]string{
		"a/a.go": procSrc,
		"a/b.go": "package a\n\nvar _ = gone()\n",
	})
	load.AllowTypeErrors = true

	pkgs, err := gopackages.Load(context.Background(), f, load)
	assert.NilError(t, err)

	i := newInstance(t, NewProc())
	assert.NilError(t, i.Param(ProcParamPartial{}))
	i.In(&procpackages.Packages{Pkgs: pkgs})
	i.Out(f)

	err = i.Run(context.Background(), gentest.NopReporter{})
	assert.ErrorContains(t, err, "kama:validate needs packages that type-check")
	assert.ErrorContains(t, err, "/a:\n\t")
	assert.ErrorContains(t, err, "undefined: gone")

	staged, err := f.Overlay()
	assert.NilError(t, err)
	assert.Equal(t, len(staged), 2, "%v", staged)
}
