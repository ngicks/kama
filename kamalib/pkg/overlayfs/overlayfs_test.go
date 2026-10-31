package overlayfs

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestOpenTwice(t *testing.T) {
	f, err := New(t.TempDir())
	assert.NilError(t, err)
	defer f.Close()

	assert.ErrorIs(t, f.Open(t.TempDir()), ErrAlreadyOpen)
}
