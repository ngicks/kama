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

func TestStagesThenCopiesDown(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(root, "orig.go"), []byte("package x\n"), 0o644))

	f := &Fs{}

	open, err := OpenProc{}.BuildContext(
		ctx,
		workflow.NewParam(map[string]any{"root": root}),
		false,
	)
	assert.NilError(t, err)
	open.(*openInstance).Out(f)
	assert.NilError(t, open.Run(ctx))
	defer f.Close()

	assert.NilError(t, f.Write("orig.gen.go", []byte("package x // gen\n")))
	assert.NilError(t, f.Write("sub/new.go", []byte("package sub\n")))

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

	down, err := CopyDownProc{}.BuildContext(ctx, workflow.NewParam(nil), false)
	assert.NilError(t, err)
	down.(*copyDownInstance).In(f)
	assert.NilError(t, down.Run(ctx))

	onDisk, err := os.ReadFile(filepath.Join(root, "sub", "new.go"))
	assert.NilError(t, err)
	assert.Equal(t, string(onDisk), "package sub\n")
}

func TestOpenDryRunTouchesNothing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	param := workflow.NewParam(map[string]any{"root": missing})

	// A dry run only decodes param, so a missing root is not reported.
	_, err := OpenProc{}.BuildContext(context.Background(), param, true)
	assert.NilError(t, err)

	_, err = OpenProc{}.BuildContext(context.Background(), param, false)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestOpenTwice(t *testing.T) {
	f, err := New(t.TempDir())
	assert.NilError(t, err)
	defer f.Close()

	assert.ErrorIs(t, f.Open(t.TempDir()), ErrAlreadyOpen)
}
