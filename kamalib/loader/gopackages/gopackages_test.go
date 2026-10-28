package gopackages

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/overlayfs"
)

func TestProcSeesStagedFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	assert.NilError(
		t,
		os.WriteFile(
			filepath.Join(root, "go.mod"),
			[]byte("module example.com/x\n\ngo 1.27\n"),
			0o644,
		),
	)
	assert.NilError(
		t,
		os.WriteFile(
			filepath.Join(root, "a.go"),
			[]byte("package x\n\nfunc A() int { return B() }\n"),
			0o644,
		),
	)

	f, err := overlayfs.New(root)
	assert.NilError(t, err)
	defer f.Close()

	// B exists only in the staged layer; type checking a.go needs it.
	assert.NilError(t, f.Write("b.gen.go", []byte("package x\n\nfunc B() int { return 1 }\n")))

	inst, err := Proc{}.Instantiate(ctx, false)
	assert.NilError(t, err)

	i := inst.(*instance)
	assert.NilError(t, i.Param(ProcParam{}))
	i.In(f)
	assert.NilError(t, i.Run(ctx))

	out := i.AsInput()
	assert.Equal(t, len(out.Pkgs), 1)
	pkg := out.Pkgs[0]
	assert.Equal(t, len(pkg.Errors), 0, "errors: %v", pkg.Errors)

	var files []string
	for _, f := range pkg.GoFiles {
		files = append(files, filepath.Base(f))
	}
	slices.Sort(files)
	assert.DeepEqual(t, files, []string{"a.go", "b.gen.go"})
}

func TestProcParam(t *testing.T) {
	i := &instance{}

	assert.NilError(t, i.Param(ProcParam{}))
	assert.DeepEqual(t, i.param.Patterns, []string{"./..."})

	assert.ErrorContains(t, i.Param(ProcParam{Patterns: []string{""}}), "empty pattern")
}
