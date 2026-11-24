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

func TestWriteFormatted(t *testing.T) {
	calls := 0
	f, err := New(t.TempDir(), WithFormatter(formatterFunc(
		func(_ context.Context, _ string, src []byte) ([]byte, error) {
			calls++
			return src, nil
		},
	)))
	assert.NilError(t, err)

	defer f.Close()

	assert.NilError(t, f.WriteFormatted("sub/x.go", []byte("package x\nfunc F( ) {}\n")))
	assert.Equal(t, calls, 0)

	got, err := fs.ReadFile(f.FS(), "sub/x.go")
	assert.NilError(t, err)
	assert.Equal(t, string(got), "package x\nfunc F( ) {}\n")
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

func TestFormat(t *testing.T) {
	ctx := context.Background()

	f, err := New(t.TempDir(), WithFormatter(formatterFunc(
		func(_ context.Context, _ string, src []byte) ([]byte, error) {
			return append([]byte("// formatted\n"), src...), nil
		},
	)))
	assert.NilError(t, err)

	got, err := f.Format(ctx, "sub/x.go", []byte("package x\n"))
	assert.NilError(t, err)
	assert.Equal(t, string(got), "// formatted\npackage x\n")

	got, err = f.Format(ctx, "x.txt", []byte("package x\n"))
	assert.NilError(t, err)
	assert.Equal(t, string(got), "package x\n")

	// Format stages nothing.
	_, err = fs.Stat(f.FS(), "sub/x.go")
	assert.ErrorIs(t, err, fs.ErrNotExist)

	// Write stages what Format returns.
	assert.NilError(t, f.Write(ctx, "sub/x.go", []byte("package x\n")))
	staged, err := fs.ReadFile(f.FS(), "sub/x.go")
	assert.NilError(t, err)
	assert.Equal(t, string(staged), "// formatted\npackage x\n")

	assert.NilError(t, f.Close())
	_, err = f.Format(ctx, "x.go", []byte("package x\n"))
	assert.ErrorIs(t, err, ErrClosed)
	assert.ErrorIs(t, f.Write(ctx, "x.go", []byte("package x\n")), ErrClosed)
}

func TestFormatLeavesInputIntact(t *testing.T) {
	ctx := context.Background()

	// The formatter rewrites its input in place, using spare capacity.
	f, err := New(t.TempDir(), WithFormatter(formatterFunc(
		func(_ context.Context, _ string, src []byte) ([]byte, error) {
			prefixed := append([]byte("// formatted\n"), src...)
			return append(src[:0], prefixed...), nil
		},
	)))
	assert.NilError(t, err)

	defer f.Close()

	content := make([]byte, 0, 100)
	content = append(content, "package x\n"...)

	got, err := f.Format(ctx, "x.go", content)
	assert.NilError(t, err)
	assert.Equal(t, string(got), "// formatted\npackage x\n")
	assert.Equal(t, string(content), "package x\n")

	assert.NilError(t, f.Write(ctx, "x.go", content))
	staged, err := fs.ReadFile(f.FS(), "x.go")
	assert.NilError(t, err)
	assert.Equal(t, string(staged), "// formatted\npackage x\n")
	assert.Equal(t, string(content), "package x\n")
}

func TestFormatError(t *testing.T) {
	errFormat := errors.New("format failed")
	f, err := New(t.TempDir(), WithFormatter(formatterFunc(
		func(context.Context, string, []byte) ([]byte, error) { return nil, errFormat },
	)))
	assert.NilError(t, err)

	defer f.Close()

	_, err = f.Format(context.Background(), "x.go", []byte("package x\n"))
	assert.ErrorIs(t, err, errFormat)
}
