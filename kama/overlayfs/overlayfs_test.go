package overlayfs

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kama/workflow"
)

// generator emits fixed files.
type generator struct{ files []workflow.File }

func (generator) Requires() []workflow.KeyRef { return nil }
func (generator) Provides() []workflow.KeyRef { return nil }
func (g generator) Run(_ context.Context, _ *workflow.Inputs, out *workflow.Outputs) error {
	for _, f := range g.files {
		if err := out.Emit(f.Path, f.Content); err != nil {
			return err
		}
	}

	return nil
}

// capture keeps the *Fs for the test to inspect.
type capture struct{ fs *Fs }

func (*capture) Requires() []workflow.KeyRef { return []workflow.KeyRef{workflow.Key[*Fs]{}} }
func (*capture) Provides() []workflow.KeyRef { return nil }
func (c *capture) Run(_ context.Context, in *workflow.Inputs, _ *workflow.Outputs) error {
	f, err := in.Get[*Fs]()
	c.fs = f
	return err
}

func TestSinkStagesThenFlushes(t *testing.T) {
	root := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(root, "orig.go"), []byte("package x\n"), 0o644))

	gen := generator{files: []workflow.File{
		{Path: "orig.gen.go", Content: []byte("package x // gen\n")},
		{Path: "sub/new.go", Content: []byte("package sub\n")},
	}}
	cap := &capture{}

	g := workflow.NewGraph()
	assert.NilError(t, g.Add("fs", Provide(root), workflow.Edges{}))
	assert.NilError(t, g.Add("gen", gen, workflow.Edges{Output: "out"}))
	assert.NilError(t, g.Add("out", Sink(), workflow.Edges{Deps: []string{"fs"}}))
	assert.NilError(t, g.Add("cap", cap, workflow.Edges{Deps: []string{"out"}}))
	assert.NilError(t, g.Run(context.Background()))

	f := cap.fs
	defer f.Close()

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

	assert.NilError(t, f.CopyDown(context.Background()))

	onDisk, err := os.ReadFile(filepath.Join(root, "sub", "new.go"))
	assert.NilError(t, err)
	assert.Equal(t, string(onDisk), "package sub\n")
}
