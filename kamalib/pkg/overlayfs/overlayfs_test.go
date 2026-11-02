package overlayfs

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestOpenTwice(t *testing.T) {
	f, err := New(t.TempDir())
	assert.NilError(t, err)
	defer f.Close()

	assert.ErrorIs(t, f.Open(t.TempDir()), ErrAlreadyOpen)
}

func TestWriteFormatsGo(t *testing.T) {
	ctx := context.Background()

	f, err := New(t.TempDir())
	assert.NilError(t, err)
	defer f.Close()

	assert.NilError(t, f.Write(ctx, "x.go", []byte("package x\nfunc F( ) { fmt.Println() }\n")))
	got, err := fs.ReadFile(f.FS(), "x.go")
	assert.NilError(t, err)
	assert.Equal(t, string(got), "package x\n\nimport \"fmt\"\n\nfunc F() { fmt.Println() }\n")

	// Only Go files are formatted.
	assert.NilError(t, f.Write(ctx, "x.txt", []byte("func F( ) {}")))
	got, err = fs.ReadFile(f.FS(), "x.txt")
	assert.NilError(t, err)
	assert.Equal(t, string(got), "func F( ) {}")
}

func TestWriteRejectsInvalidGo(t *testing.T) {
	f, err := New(t.TempDir())
	assert.NilError(t, err)
	defer f.Close()

	assert.ErrorContains(
		t,
		f.Write(context.Background(), "x.go", []byte("package x\nfunc {")),
		"format",
	)

	_, err = fs.Stat(f.FS(), "x.go")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

type formatterFunc func(ctx context.Context, path string, src []byte) ([]byte, error)

func (fn formatterFunc) Format(ctx context.Context, path string, src []byte) ([]byte, error) {
	return fn(ctx, path, src)
}

func TestWithFormatter(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	var gotPath string
	f, err := New(root, WithFormatter(formatterFunc(
		func(_ context.Context, path string, src []byte) ([]byte, error) {
			gotPath = path
			return append([]byte("// formatted\n"), src...), nil
		},
	)))
	assert.NilError(t, err)
	defer f.Close()

	assert.NilError(t, f.Write(ctx, "sub/x.go", []byte("package x\n")))
	assert.Equal(t, gotPath, filepath.Join(f.Root(), "sub", "x.go"))

	got, err := fs.ReadFile(f.FS(), "sub/x.go")
	assert.NilError(t, err)
	assert.Equal(t, string(got), "// formatted\npackage x\n")
}

func TestWithFormatterError(t *testing.T) {
	errFormat := errors.New("format failed")
	f, err := New(t.TempDir(), WithFormatter(formatterFunc(
		func(context.Context, string, []byte) ([]byte, error) { return nil, errFormat },
	)))
	assert.NilError(t, err)
	defer f.Close()

	assert.ErrorIs(t, f.Write(context.Background(), "x.go", []byte("package x\n")), errFormat)
}
