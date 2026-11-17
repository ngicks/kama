package goformat

import (
	"context"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestImports(t *testing.T) {
	src := []byte(
		"package x\nimport \"os\"\nfunc F( ) string { return strings.TrimSpace(\" a \") }\n",
	)

	got, err := Imports{}.Format(context.Background(), filepath.Join(t.TempDir(), "x.go"), src)
	assert.NilError(t, err)
	assert.Equal(t, string(got), `package x

import "strings"

func F() string { return strings.TrimSpace(" a ") }
`)
}

func TestImportsInvalid(t *testing.T) {
	_, err := Imports{}.Format(context.Background(), "/x.go", []byte("package x\nfunc {"))
	assert.Assert(t, err != nil)
}

func TestCommand(t *testing.T) {
	dir := t.TempDir()
	c := Command{Argv: []string{"sh", "-c", `pwd; cat`}, Dir: dir}

	got, err := c.Format(context.Background(), "ignored.go", []byte("package x\n"))
	assert.NilError(t, err)
	assert.Equal(t, string(got), dir+"\npackage x\n")
}

func TestCommandErrors(t *testing.T) {
	ctx := context.Background()

	_, err := Command{}.Format(ctx, "x.go", nil)
	assert.ErrorIs(t, err, ErrEmptyArgv)

	_, err = Command{Argv: []string{"sh", "-c", "echo broken >&2; exit 3"}}.Format(ctx, "x.go", nil)
	assert.ErrorContains(t, err, "broken")

	ctx, cancel := context.WithCancel(ctx)
	cancel()

	_, err = Command{Argv: []string{"cat"}}.Format(ctx, "x.go", nil)
	assert.ErrorIs(t, err, context.Canceled)
}
