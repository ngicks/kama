package overlayfs

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

func TestStagesThenCopiesDown(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(root, "orig.go"), []byte("package x\n"), 0o644))

	openProc := &OpenProc{}
	inst, err := openProc.Instantiate(ctx, false)
	assert.NilError(t, err)
	open := inst.(*openInstance)
	assert.NilError(t, open.Param(OpenParam{Root: root}))
	assert.NilError(t, open.Run(ctx))

	f := open.AsInput()

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

	down, err := CopyDownProc{}.Instantiate(ctx, false)
	assert.NilError(t, err)
	down.(*copyDownInstance).In(f)
	assert.NilError(t, down.Run(ctx))

	onDisk, err := os.ReadFile(filepath.Join(root, "sub", "new.go"))
	assert.NilError(t, err)
	assert.Equal(t, string(onDisk), "package sub\n")

	assert.NilError(t, openProc.Close(ctx))
	assert.ErrorIs(t, f.Write("late.go", nil), overlayfs.ErrClosed)
}

func TestOpenDryRunTouchesNothing(t *testing.T) {
	ctx := context.Background()
	param := OpenParam{Root: filepath.Join(t.TempDir(), "missing")}

	// A dry-run instance does not look at the root, so a missing one is not
	// reported.
	dry, err := (&OpenProc{}).Instantiate(ctx, true)
	assert.NilError(t, err)
	assert.NilError(t, dry.(*openInstance).Param(param))

	real, err := (&OpenProc{}).Instantiate(ctx, false)
	assert.NilError(t, err)
	assert.ErrorIs(t, real.(*openInstance).Param(param), fs.ErrNotExist)
}
