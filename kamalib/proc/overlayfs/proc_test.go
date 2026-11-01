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

	assert.NilError(t, openProc.CleanUp(ctx))
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

func TestOpenSharesFs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	p := &OpenProc{}

	open := func(root string) (*overlayfs.Fs, error) {
		t.Helper()

		inst, err := p.Instantiate(ctx, false)
		assert.NilError(t, err)
		i := inst.(*openInstance)
		assert.NilError(t, i.Param(OpenParam{Root: root}))
		if err := i.Run(ctx); err != nil {
			return nil, err
		}

		return i.AsInput(), nil
	}

	f1, err := open(root)
	assert.NilError(t, err)
	f2, err := open(root)
	assert.NilError(t, err)

	// A file staged through one step is seen by the other.
	assert.NilError(t, f1.Write("gen.go", []byte("package x\n")))
	got, err := fs.ReadFile(f2.FS(), "gen.go")
	assert.NilError(t, err)
	assert.Equal(t, string(got), "package x\n")
	assert.Assert(t, f1 == f2)

	_, err = open(t.TempDir())
	assert.ErrorContains(t, err, "is open")

	assert.NilError(t, p.CleanUp(ctx))
	assert.ErrorIs(t, f1.Write("late.go", nil), overlayfs.ErrClosed)
}
