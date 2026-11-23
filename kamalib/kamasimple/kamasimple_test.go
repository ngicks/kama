package kamasimple

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/workflow"
)

// noteParam configures noteProc.
type noteParam struct {
	Text    string         `json:"text"`
	Unknown map[string]any `json:",embed"`
}

func (p *noteParam) UnknownKeys() [][]string { return workflow.UnknownKeys(p.Unknown) }

// noteProc records the text of each of its steps and warns with it.
type noteProc struct {
	ran *[]string
}

func (noteProc) ParamType() reflect.Type   { return reflect.TypeFor[noteParam]() }
func (noteProc) InTypes() []reflect.Type   { return nil }
func (noteProc) OutTypes() []reflect.Type  { return nil }
func (noteProc) AsInputType() reflect.Type { return nil }

func (p noteProc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &noteInstance{ran: p.ran}, nil
}

func (noteProc) CleanUp(context.Context) error { return nil }

type noteInstance struct {
	ran  *[]string
	text string
}

func (i *noteInstance) Param(p noteParam) error {
	i.text = p.Text
	return nil
}

func (i *noteInstance) In() {}

func (i *noteInstance) Out() {}

func (i *noteInstance) Run(_ context.Context, r workflow.Reporter) error {
	*i.ran = append(*i.ran, i.text)
	r.Warn(errors.New(i.text))

	return nil
}

func TestRun(t *testing.T) {
	specs := map[string]string{
		"spec.yaml": "version: 1\nsteps:\n  a: {proc: note, param: {text: one}}\n" +
			"  b: {proc: note, param: {text: two}, after: a}\n",
		"spec.json": `{"version": 1, "steps": {` +
			`"a": {"proc": "note", "param": {"text": "one"}},` +
			`"b": {"proc": "note", "param": {"text": "two"}, "after": "a"}}}`,
		"spec.toml": "version = 1\n\n[steps.a]\nproc = \"note\"\nparam = {text = \"one\"}\n\n" +
			"[steps.b]\nproc = \"note\"\nparam = {text = \"two\"}\nafter = \"a\"\n",
	}

	for name, src := range specs {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			assert.NilError(t, os.WriteFile(path, []byte(src), 0o644))

			var ran, warned []string

			reg := workflow.NewRegistry()
			assert.NilError(
				t,
				reg.Register("note", func() workflow.Proc { return noteProc{ran: &ran} }),
			)

			err := Run(context.Background(), Options{
				Spec:     path,
				Registry: reg,
				Progress: workflow.ProgressControl{
					OnWarning: func(step string, w error) { warned = append(warned, step+": "+w.Error()) },
				},
			})
			assert.NilError(t, err)
			assert.DeepEqual(t, ran, []string{"one", "two"})
			assert.DeepEqual(t, warned, []string{"a: one", "b: two"})
		})
	}
}

// TestRunWarnsUnknownKeys checks that the unknown keys of the spec reach
// OnWarning with an empty step before the steps report.
func TestRunWarnsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	assert.NilError(t, os.WriteFile(
		path,
		[]byte("version: 1\nstpes: {}\nsteps:\n"+
			"  a: {proc: note, prams: {text: one}}\n"+
			"  b: {proc: note, param: {text: two, txet: x}, after: a}\n"),
		0o644,
	))

	var ran []string

	reg := workflow.NewRegistry()
	assert.NilError(t, reg.Register("note", func() workflow.Proc { return noteProc{ran: &ran} }))

	var warned []string

	err := Run(context.Background(), Options{
		Spec:     path,
		Registry: reg,
		Progress: workflow.ProgressControl{
			OnWarning: func(step string, w error) {
				if step == "" {
					assert.ErrorIs(t, w, workflow.ErrUnknownKey)
				}

				warned = append(warned, step+": "+w.Error())
			},
		},
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, warned, []string{
		": kamasimple: " + path + ": workflow: unknown key: stpes",
		": kamasimple: " + path + ": workflow: unknown key: steps.a.prams",
		": kamasimple: " + path + ": workflow: unknown key: steps.b.param.txet",
		"a: ",
		"b: two",
	})
}

func TestRunErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) string {
		t.Helper()

		path := filepath.Join(dir, name)
		assert.NilError(t, os.WriteFile(path, []byte(src), 0o644))

		return path
	}

	err := Run(context.Background(), Options{Spec: filepath.Join(dir, "missing.yaml")})
	assert.ErrorIs(t, err, os.ErrNotExist)

	err = Run(context.Background(), Options{Spec: write("spec.txt", "version: 1\n")})
	assert.ErrorIs(t, err, fileloader.ErrUnknownFormat)

	// A spec naming an unknown proc fails before any step runs, and the
	// error names the spec file.
	path := write("spec.yaml", "version: 1\nsteps:\n  a: {proc: no.such}\n")
	err = Run(context.Background(), Options{Spec: path, Registry: workflow.NewRegistry()})
	assert.ErrorContains(t, err, path)
	assert.ErrorContains(t, err, "no.such")
}

// TestDefaultRegistry names every proc kamalib ships in one spec. Building
// it needs each of them registered by importing this package.
func TestDefaultRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	assert.NilError(t, os.WriteFile(path, []byte(`version: 1
steps:
  root: {proc: overlayfs.open}
  load: {proc: packages.load, in: root}
  partial: {proc: partial.generate, in: load, out: root}
  validate: {proc: validate.generate, in: load, out: root}
  template: {proc: file.load, in: root, param: {base: keys}}
  keys:
    proc: contextkey.generate
    in: template
    out: root
    param: {package: keys, key_type_func: keyTypeFunc}
  final: {proc: overlayfs.copy_down, in: root, after: [partial, validate, keys]}
`), 0o644))

	spec, err := LoadSpec(path)
	assert.NilError(t, err)

	// Build only dry-runs, so nothing is read or written.
	g, err := spec.Build(context.Background(), workflow.DefaultRegistry)
	assert.NilError(t, err)
	assert.Assert(t, len(g.Warnings()) == 0, "%v", g.Warnings())
}

// TestDefaultRegistryUnknownParamKeys checks that the params of the procs
// kamalib ships tell their unknown keys, those of nested tables included.
func TestDefaultRegistryUnknownParamKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	assert.NilError(t, os.WriteFile(path, []byte(`version: 1
steps:
  root: {proc: overlayfs.open, param: {rot: x}}
  load: {proc: packages.load, in: root, param: {patern: x}}
  partial:
    proc: partial.generate
    in: load
    out: root
    param: {mrege: append, option_file: {base: x, bse: y}}
  validate: {proc: validate.generate, in: load, out: root, param: {option_flie: x}}
  template: {proc: file.load, in: root, param: {base: keys, fromats: [yaml]}}
  keys:
    proc: contextkey.generate
    in: template
    out: root
    param: {package: keys, key_type_func: keyTypeFunc, dri: x}
  final:
    proc: overlayfs.copy_down
    in: root
    after: [partial, validate, keys]
    param: {test: true}
`), 0o644))

	spec, err := LoadSpec(path)
	assert.NilError(t, err)

	g, err := spec.Build(context.Background(), workflow.DefaultRegistry)
	assert.NilError(t, err)

	got := opiter.WrapSeq(slices.Values(g.Warnings())).
		Map(error.Error).
		Collect(slices.Sorted)

	assert.DeepEqual(t, got, []string{
		"workflow: unknown key: steps.final.param.test",
		"workflow: unknown key: steps.keys.param.dri",
		"workflow: unknown key: steps.load.param.patern",
		"workflow: unknown key: steps.partial.param.mrege",
		"workflow: unknown key: steps.partial.param.option_file.bse",
		"workflow: unknown key: steps.root.param.rot",
		"workflow: unknown key: steps.template.param.fromats",
		"workflow: unknown key: steps.validate.param.option_flie",
	})
}
