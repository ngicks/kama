package overlayfs

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs/goformat"
)

func TestStagesThenCopiesDown(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(root, "orig.go"), []byte("package x\n"), 0o644))
	assert.NilError(t, os.WriteFile(
		filepath.Join(root, "go.mod"), []byte("module example.invalid/x\n\ngo 1.27\n"), 0o644,
	))

	openProc := &OpenProc{}
	inst, err := openProc.Instantiate(ctx, false)
	assert.NilError(t, err)

	open := inst.(*openInstance)
	assert.NilError(t, open.Param(OpenParam{Root: root}))
	assert.NilError(t, open.Run(ctx, nopReporter{}))

	f := open.AsInput()

	assert.NilError(t, f.Write(ctx, "orig.gen.go", []byte("package x // gen\n")))
	assert.NilError(t, f.Write(ctx, "sub/new.go", []byte("package sub\n")))

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
	assert.NilError(t, down.(*copyDownInstance).Param(CopyDownParam{}))
	down.(*copyDownInstance).In(f)
	assert.NilError(t, down.Run(ctx, nopReporter{}))

	onDisk, err := os.ReadFile(filepath.Join(root, "sub", "new.go"))
	assert.NilError(t, err)
	assert.Equal(t, string(onDisk), "package sub\n")

	assert.NilError(t, openProc.CleanUp(ctx))
	assert.ErrorIs(t, f.Write(ctx, "late.go", nil), overlayfs.ErrClosed)
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

		if err := i.Run(ctx, nopReporter{}); err != nil {
			return nil, err
		}

		return i.AsInput(), nil
	}

	f1, err := open(root)
	assert.NilError(t, err)
	f2, err := open(root)
	assert.NilError(t, err)

	// A file staged through one step is seen by the other.
	assert.NilError(t, f1.Write(ctx, "gen.go", []byte("package x\n")))

	got, err := fs.ReadFile(f2.FS(), "gen.go")
	assert.NilError(t, err)
	assert.Equal(t, string(got), "package x\n")
	assert.Assert(t, f1 == f2)

	_, err = open(t.TempDir())
	assert.ErrorContains(t, err, "is open")

	assert.NilError(t, p.CleanUp(ctx))
	assert.ErrorIs(t, f1.Write(ctx, "late.go", nil), overlayfs.ErrClosed)
}

func TestOpenFormatter(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	p := &OpenProc{}

	open := func(formatter []string) (*overlayfs.Fs, error) {
		t.Helper()

		inst, err := p.Instantiate(ctx, false)
		assert.NilError(t, err)

		i := inst.(*openInstance)
		assert.NilError(t, i.Param(OpenParam{Root: root, Formatter: formatter}))

		if err := i.Run(ctx, nopReporter{}); err != nil {
			return nil, err
		}

		return i.AsInput(), nil
	}

	// The command runs in root and its stdout replaces the content.
	formatter := []string{"sh", "-c", `echo "// $(basename "$PWD")"; cat`}
	f, err := open(formatter)
	assert.NilError(t, err)

	assert.NilError(t, f.Write(ctx, "x.go", []byte("package x\n")))
	got, err := fs.ReadFile(f.FS(), "x.go")
	assert.NilError(t, err)
	assert.Equal(t, string(got), "// "+filepath.Base(root)+"\npackage x\n")

	_, err = open(formatter)
	assert.NilError(t, err)

	_, err = open(nil)
	assert.ErrorContains(t, err, "formatter")

	assert.NilError(t, p.CleanUp(ctx))
}

func TestOpenFormatterParam(t *testing.T) {
	ctx := context.Background()
	missing := OpenParam{Root: t.TempDir(), Formatter: []string{"kama-no-such-formatter"}}

	dry, err := (&OpenProc{}).Instantiate(ctx, true)
	assert.NilError(t, err)
	assert.NilError(t, dry.(*openInstance).Param(missing))
	assert.ErrorIs(
		t,
		dry.(*openInstance).Param(OpenParam{Formatter: []string{""}}),
		goformat.ErrEmptyArgv,
	)

	real, err := (&OpenProc{}).Instantiate(ctx, false)
	assert.NilError(t, err)
	assert.ErrorIs(t, real.(*openInstance).Param(missing), exec.ErrNotFound)
}

// nopReporter drops what an instance reports.
type nopReporter struct{}

func (nopReporter) Warn(error)      {}
func (nopReporter) Progress(string) {}

func TestCopyDownSkipsCheckWithoutGoFiles(t *testing.T) {
	ctx := context.Background()
	// The root is not a Go module, so a package load in it fails.
	root := t.TempDir()

	f, err := overlayfs.New(root)
	assert.NilError(t, err)

	defer f.Close()

	assert.NilError(t, f.Write(ctx, "notes.txt", []byte("notes\n")))

	down, err := CopyDownProc{}.Instantiate(ctx, false)
	assert.NilError(t, err)
	assert.NilError(t, down.(*copyDownInstance).Param(CopyDownParam{}))
	down.(*copyDownInstance).In(f)
	assert.NilError(t, down.Run(ctx, nopReporter{}))

	onDisk, err := os.ReadFile(filepath.Join(root, "notes.txt"))
	assert.NilError(t, err)
	assert.Equal(t, string(onDisk), "notes\n")
}

func TestCopyDownParamRejectsEmptyPattern(t *testing.T) {
	down, err := CopyDownProc{}.Instantiate(context.Background(), true)
	assert.NilError(t, err)
	assert.ErrorContains(
		t, down.(*copyDownInstance).Param(CopyDownParam{Patterns: []string{""}}), "empty pattern",
	)
}
