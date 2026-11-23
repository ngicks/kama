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

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	procpackages "github.com/ngicks/kama/kamalib/proc/gopackages"
	procoverlayfs "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"

	"github.com/ngicks/kama/kamalib/generator/validator/rule"
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

	i := &instance{}
	assert.NilError(t, i.Param(ProcParamPartial{}))
	assert.Assert(t, i.optionFile == nil)

	err := i.Param(param(fileloader.PathInput{}))
	assert.Error(t, err, "option_file: base is required")

	err = i.Param(
		param(fileloader.PathInput{Base: "validate", Formats: []fileloader.Format{"ini"}}),
	)
	assert.ErrorIs(t, err, fileloader.ErrUnknownFormat)

	assert.NilError(t, i.Param(param(fileloader.PathInput{Base: "validate"})))
	assert.DeepEqual(t, i.optionFile, &fileloader.PathInput{
		Base: "validate",
		Formats: []fileloader.Format{
			fileloader.FormatYaml, fileloader.FormatJson, fileloader.FormatToml,
		},
	})
}

// procProject writes files into a new module example.invalid/x that
// resolves this module from its directory, and returns the root.
func procProject(t *testing.T, files map[string]string) string {
	t.Helper()

	kamalib, err := filepath.Abs("../..")
	assert.NilError(t, err)

	// A go line below that of this module would need go mod tidy.
	gomod, err := os.ReadFile(filepath.Join(kamalib, "go.mod"))
	assert.NilError(t, err)

	goLine := opiter.WrapSeq(strings.Lines(string(gomod))).
		Filter(func(l string) bool { return strings.HasPrefix(l, "go ") }).
		First()
	assert.Assert(t, goLine.IsSome())

	root := t.TempDir()
	files["go.mod"] = "module example.invalid/x\n\n" + goLine.Value() + "\n" +
		"require github.com/ngicks/kama/kamalib v0.0.0\n\n" +
		"replace github.com/ngicks/kama/kamalib => " + kamalib + "\n"

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
//kama:validate hook=off
type A struct {
	//kama:validate length=">=1"
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
	reg := rule.DefaultRegistry().Clone()
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
		"a/a.go": "package a\n\n//kama:validate\n//kama:validate hook=off\ntype A struct {\n" +
			"\t//kama:validate positive\n\tN int\n" +
			"\t//kama:validate length=\">=1\"\n\tS string\n}\n",
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
