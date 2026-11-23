package gentest_test

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
)

func TestFuncs(t *testing.T) {
	got := gentest.Funcs(t, `package x

// F is a function.
func F() {}

func (T) A() {}

func (*T) B() {}

func (G[K]) C() {}

func (*G2[K, V]) D() {}
`)
	assert.DeepEqual(t, got, map[string]string{
		"F":    "func F() {}",
		"T.A":  "func (T) A() {}",
		"T.B":  "func (*T) B() {}",
		"G.C":  "func (G[K]) C() {}",
		"G2.D": "func (*G2[K, V]) D() {}",
	})
}

func TestByBase(t *testing.T) {
	assert.DeepEqual(t, gentest.ByBase([]base.File{
		{Path: "/a/b/c.go", Content: []byte("c")},
		{Path: "d.go", Content: []byte("d")},
	}), map[string]string{"c.go": "c", "d.go": "d"})
}
