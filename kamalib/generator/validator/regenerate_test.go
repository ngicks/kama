package validator

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
)

const regenerateSrc = `package x

import "github.com/ngicks/option/opt"

//kama:validate
type A struct {
	//kama:validate required
	Name  string ` + "`json:\"name\"`" + `
	Inner Inner
	Items []Inner
	Box   Box[Inner]
	O     opt.Option[*Inner]
}

func (A) validateName(bool) error  { return nil }
func (A) validateInner(bool) error { return nil }
func (A) validateItems(bool) error { return nil }
func (A) validateBox(bool) error   { return nil }
func (A) validateO(bool) error     { return nil }

//kama:validate
//kama:validate hook=False
type Inner struct {
	//kama:validate required
	P *int
}

//kama:validate
//kama:validate hook=False
type Box[T any] struct{ V []T }

type Holder struct{ M map[string]Inner }

type Hand struct{}

func (Hand) Validate(bool) error { return nil }

type HandHolder struct{ H []Hand }
`

// TestRegenerate generates twice, the second time with the output of the
// first loaded as input. The second run reports nothing stale and gives the
// same files.
func TestRegenerate(t *testing.T) {
	f, opts := stage(t, map[string]string{
		"a.go": regenerateSrc,
		"b_linux.go": "package x\n\n//kama:validate\n//kama:validate hook=False\ntype B struct {\n" +
			"\t//kama:validate required\n\tN int\n}\n",
	})

	first, err := generateInto(t, f, opts, Options{})
	assert.NilError(t, err)

	second, err := generateInto(t, f, opts, Options{})
	assert.NilError(t, err)

	assert.DeepEqual(t, len(second), len(first))

	for name, src := range first {
		assert.Equal(t, second[name], src, name)
	}

	_, err = gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)
}

// TestStale reports a generated file of a type no longer targeted.
func TestStale(t *testing.T) {
	f, opts := stage(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=False\ntype A struct{ N int }\n",
		"b.go": "package x\n\n//kama:validate\n//kama:validate hook=False\ntype B struct{ N int }\n",
	})

	_, err := generateInto(t, f, opts, Options{})
	assert.NilError(t, err)

	ctx := context.Background()
	assert.NilError(
		t,
		f.Write(ctx, gentest.InlineDir(t)+"/b.go", []byte("package x\n\ntype B struct{ N int }\n")),
	)

	pkgs, err := gopackages.Load(ctx, f, opts)
	assert.NilError(t, err)

	files, warnings, err := runGenerate(t, f, pkgs, testRun{})
	assert.NilError(t, err)
	assert.Equal(t, len(files), 1)
	assert.Equal(t, len(warnings), 1)
	assert.ErrorIs(t, warnings[0], base.ErrStale)
	assert.ErrorContains(t, warnings[0], "b.kama_validate.go")
}
