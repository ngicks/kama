package overlayfs

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/workflow"
)

// generate writes fixed files into the filesystem it is given as output.
func generate(_ context.Context, out *Fs, _ struct{}) error {
	if err := out.Write("orig.gen.go", []byte("package x // gen\n")); err != nil {
		return err
	}

	return out.Write("sub/new.go", []byte("package sub\n"))
}

func TestStagesThenCopiesDown(t *testing.T) {
	root := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(root, "orig.go"), []byte("package x\n"), 0o644))

	var checked bool
	check := func(_ context.Context, f *Fs, _ struct{}) error {
		checked = true

		// The merged view shows originals and staged files together.
		got, err := fs.ReadFile(f.FS(), "orig.gen.go")
		assert.NilError(t, err)
		assert.Equal(t, string(got), "package x // gen\n")

		got, err = fs.ReadFile(f.FS(), "orig.go")
		assert.NilError(t, err)
		assert.Equal(t, string(got), "package x\n")

		// The overlay map holds only staged files, keyed by absolute path.
		ov, err := f.Overlay()
		assert.NilError(t, err)
		assert.DeepEqual(t, ov, map[string][]byte{
			filepath.Join(root, "orig.gen.go"):   []byte("package x // gen\n"),
			filepath.Join(root, "sub", "new.go"): []byte("package sub\n"),
		})

		// Nothing reached disk yet.
		_, err = os.Stat(filepath.Join(root, "orig.gen.go"))
		assert.ErrorIs(t, err, fs.ErrNotExist)

		return nil
	}

	var opened *Fs
	keep := func(_ context.Context, f *Fs, _ struct{}) error {
		opened = f
		return nil
	}

	reg := workflow.NewRegistry()
	assert.NilError(t, reg.Loader("fs", Load))
	assert.NilError(t, reg.Loader("gen", generate))
	assert.NilError(t, reg.Sink("check", check))
	assert.NilError(t, reg.Sink("keep", keep))
	assert.NilError(t, reg.Sink("copy_down", CopyDownSink))

	spec := workflow.Spec{
		Version: workflow.SpecVersion,
		Steps: map[string]workflow.Step{
			"root":  {Loader: "fs", Param: map[string]any{"root": root}},
			"gen":   {Loader: "gen", Output: "root"},
			"check": {Sink: "check", Input: workflow.Names{"root"}, After: workflow.Names{"gen"}},
			"down": {
				Sink:  "copy_down",
				Input: workflow.Names{"root"},
				After: workflow.Names{"check"},
			},
			"keep": {Sink: "keep", Input: workflow.Names{"root"}},
		},
	}

	p, err := spec.Build(reg)
	assert.NilError(t, err)
	assert.NilError(t, p.Run(context.Background()))
	defer opened.Close()

	assert.Assert(t, checked)

	onDisk, err := os.ReadFile(filepath.Join(root, "sub", "new.go"))
	assert.NilError(t, err)
	assert.Equal(t, string(onDisk), "package sub\n")
}

func TestOpenTwice(t *testing.T) {
	f, err := New(t.TempDir())
	assert.NilError(t, err)
	defer f.Close()

	assert.ErrorIs(t, f.Open(t.TempDir()), ErrAlreadyOpen)
}
