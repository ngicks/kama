package base_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ngicks/option/opt"
	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	procgopackages "github.com/ngicks/kama/kamalib/proc/gopackages"
	procoverlayfs "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

var fakeId = base.Id{Namespace: "acme", Name: "fake"}

// fakeParam is the param of a fake generator writing one file.
type fakeParam struct {
	Greeting   string
	OptionFile opt.Option[fileloader.PathInput]
}

// fakeParamPartial is the partial variant of fakeParam, written by hand.
type fakeParamPartial struct {
	Greeting   *string                          `json:"greeting"`
	OptionFile opt.Option[fileloader.PathInput] `json:"option_file"`
	Unknown    map[string]any                   `json:",embed"`
}

func (p fakeParamPartial) Merge(r fakeParamPartial) fakeParamPartial {
	if r.Greeting != nil {
		p.Greeting = r.Greeting
	}

	if r.OptionFile.IsSome() {
		p.OptionFile = r.OptionFile
	}

	return p
}

func (p fakeParamPartial) ToValue() fakeParam {
	return fakeParam{Greeting: *p.Greeting, OptionFile: p.OptionFile}
}

func (p *fakeParamPartial) UnknownKeys() [][]string { return unknownkey.Paths(p.Unknown) }

// fakeLoader records the option file it is asked to load.
type fakeLoader struct {
	optionFile **fileloader.PathInput
	err        error
}

func (l fakeLoader) LoadOptionNamed(
	_ fs.FS,
	_ string,
	_ []*packages.Package,
	optionFile *fileloader.PathInput,
) (string, error) {
	*l.optionFile = optionFile
	return "loaded", l.err
}

var errFakeLoad = errors.New("fake: load")

// fakeProc returns the proc of the fake generator staging into the root of
// f. loaded receives the option file the loader is asked for.
func fakeProc(
	t *testing.T,
	f *overlayfs.Fs,
	loaded **fileloader.PathInput,
	loadErr error,
) base.SourceProc[fakeParamPartial, fakeParam, string, string] {
	t.Helper()

	hello := "hello"

	return base.SourceProc[fakeParamPartial, fakeParam, string, string]{
		Id:      fakeId,
		Default: func() fakeParamPartial { return fakeParamPartial{Greeting: &hello} },
		Options: func(p fakeParam) (string, opt.Option[fileloader.PathInput], error) {
			if p.Greeting == "" {
				return "", opt.None[fileloader.PathInput](), errors.New("fake: empty greeting")
			}

			return p.Greeting, p.OptionFile, nil
		},
		Loader:    fakeLoader{optionFile: loaded, err: loadErr},
		LoadError: func(err error) error { return fmt.Errorf("wrapped: %w", err) },
		Generate: func(_ []*packages.Package, greeting, loaded string) ([]base.File, []error, error) {
			body := fmt.Sprintf("var Greeting = %q\n\nvar Loaded = %q", greeting, loaded)

			return []base.File{{
				Path:    filepath.Join(f.Root(), "gen.go"),
				Content: source(t, gentest.Now(), body),
			}}, []error{errors.New("fake: warning")}, nil
		},
	}
}

// fakeInstance is an instance of the fake proc.
type fakeInstance interface {
	Param(p fakeParamPartial) error
	In(pkgs *procgopackages.Packages)
	Out(f *overlayfs.Fs)
	Run(ctx context.Context, r workflow.Reporter) error
}

func instantiate(t *testing.T, p workflow.Proc) fakeInstance {
	t.Helper()

	inst, err := p.Instantiate(context.Background(), false)
	assert.NilError(t, err)

	i, ok := inst.(fakeInstance)
	assert.Assert(t, ok, "%T", inst)

	return i
}

func TestSourceProc(t *testing.T) {
	ctx := context.Background()
	f, err := overlayfs.New(t.TempDir())
	assert.NilError(t, err)

	defer f.Close()

	var loaded *fileloader.PathInput

	p := fakeProc(t, f, &loaded, nil)
	assert.Equal(t, p.ParamType(), reflect.TypeFor[fakeParamPartial]())
	assert.Equal(t, fakeId.ProcKind(), "fake.generate")

	i := instantiate(t, p)
	assert.NilError(t, i.Param(fakeParamPartial{
		OptionFile: opt.Some(fileloader.PathInput{Base: "opts"}),
	}))
	i.In(&procgopackages.Packages{})
	i.Out(f)

	r := &gentest.RecordReporter{}
	assert.NilError(t, i.Run(ctx, r))
	assert.Equal(t, len(r.Warnings), 1)
	assert.Error(t, r.Warnings[0], "fake: warning")
	assert.DeepEqual(t, r.Messages, []string{"1 file written, 0 files unchanged"})

	// The option file reaches the loader normalized.
	assert.DeepEqual(t, loaded, &fileloader.PathInput{
		Base: "opts",
		Formats: []fileloader.Format{
			fileloader.FormatToml, fileloader.FormatYaml, fileloader.FormatJson,
		},
	})

	gen, err := fs.ReadFile(f.FS(), "gen.go")
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(gen), `var Greeting = "hello"`), string(gen))
	assert.Assert(t, strings.Contains(string(gen), `var Loaded = "loaded"`), string(gen))
}

func TestSourceProcParam(t *testing.T) {
	var loaded *fileloader.PathInput

	i := instantiate(t, fakeProc(t, nil, &loaded, nil))

	empty := ""
	assert.Error(t, i.Param(fakeParamPartial{Greeting: &empty}), "fake: empty greeting")
	assert.Error(
		t,
		i.Param(fakeParamPartial{OptionFile: opt.Some(fileloader.PathInput{})}),
		"option_file: base is required",
	)
	assert.ErrorIs(t, i.Param(fakeParamPartial{OptionFile: opt.Some(fileloader.PathInput{
		Base: "opts", Formats: []fileloader.Format{"ini"},
	})}), fileloader.ErrUnknownFormat)
}

func TestSourceProcLoadError(t *testing.T) {
	f, err := overlayfs.New(t.TempDir())
	assert.NilError(t, err)

	defer f.Close()

	var loaded *fileloader.PathInput

	i := instantiate(t, fakeProc(t, f, &loaded, errFakeLoad))
	assert.NilError(t, i.Param(fakeParamPartial{}))
	i.In(&procgopackages.Packages{})
	i.Out(f)

	err = i.Run(context.Background(), &gentest.RecordReporter{})
	assert.ErrorIs(t, err, errFakeLoad)
	assert.ErrorContains(t, err, "wrapped: ")
	assert.Assert(t, loaded == nil)

	_, err = fs.Stat(f.FS(), "gen.go")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// TestSourceProcWorkflow builds a graph around the proc: workflow finds the
// methods of the generic instance and the unknown keys of its param.
func TestSourceProcWorkflow(t *testing.T) {
	var loaded *fileloader.PathInput

	reg := workflow.NewRegistry()
	for kind, newProc := range map[string]func() workflow.Proc{
		"overlayfs.open": procoverlayfs.NewOpenProc,
		"packages.load":  procgopackages.NewProc,
		fakeId.ProcKind(): func() workflow.Proc {
			return fakeProc(t, nil, &loaded, nil)
		},
	} {
		assert.NilError(t, reg.Register(kind, newProc))
	}

	spec := workflow.Spec{
		Version: 1,
		Steps: map[string]workflow.Step{
			"root": {Proc: "overlayfs.open", Param: map[string]any{"root": t.TempDir()}},
			"load": {Proc: "packages.load", In: workflow.Names{"root"}},
			"gen": {
				Proc:  fakeId.ProcKind(),
				In:    workflow.Names{"load"},
				Out:   workflow.Names{"root"},
				Param: map[string]any{"greeting": "hi", "typo": 1},
			},
		},
	}

	g, err := spec.Build(context.Background(), reg)
	assert.NilError(t, err)

	warnings := g.Warnings()
	assert.Equal(t, len(warnings), 1, "%v", warnings)
	assert.ErrorIs(t, warnings[0], workflow.ErrUnknownKey)
	assert.ErrorContains(t, warnings[0], "typo")
}
