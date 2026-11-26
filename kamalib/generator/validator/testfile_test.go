package validator

import (
	"context"
	"maps"
	"slices"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
)

// TestTestFiles generates Validate for targets of a _test.go file of the
// package x and of the external test package x_test, builds the output with
// the tests and generates again over it.
func TestTestFiles(t *testing.T) {
	x := modulePath(t)
	f, opts := stage(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\n//kama:validate hook=False\ntype Inner struct {\n" +
			"\t//kama:validate required\n\tP *int\n}\n",
		"a_test.go": "package x\n\n//kama:validate\n//kama:validate hook=False\ntype B struct {\n" +
			"\t//kama:validate required\n\tP *int\n" +
			"\t//kama:validate length(le=3)\n\tItems []Inner\n}\n",
		"ext_test.go": "package x_test\n\nimport x \"" + x + "\"\n\n" +
			"//kama:validate\n//kama:validate hook=False\ntype C struct {\n" +
			"\t//kama:validate required\n\tP *int\n\tB x.B\n}\n",
	})
	opts.Tests = true

	first, err := generateInto(t, f, opts, Options{})
	assert.NilError(t, err)
	assert.DeepEqual(t, slices.Sorted(maps.Keys(first)), []string{
		"a.kama_validate.go",
		"a.kama_validate_test.go",
		"ext.kama_validate_test.go",
		"zz_gen.helper.a.kama_validate_test.go",
		"zz_gen.helper.kama_validate.go",
		"zz_gen.helper.kama_validate_test.go",
	})
	// a.go and a_test.go share kamaValidateRequired, which x_test declares
	// for itself. Only a_test.go checks a length.
	assertContains(
		t,
		first["zz_gen.helper.kama_validate.go"],
		"\npackage x\n",
		"func kamaValidateRequired(",
	)
	assertNotContains(t, first["zz_gen.helper.kama_validate.go"], "Length")
	assertContains(t, first["zz_gen.helper.a.kama_validate_test.go"], "\npackage x\n", "Length")
	assertContains(
		t,
		first["zz_gen.helper.kama_validate_test.go"],
		"\npackage x_test\n",
		"func kamaValidateRequired(",
	)

	assert.NilError(t, gentest.Build(f, opts))

	second, err := generateInto(t, f, opts, Options{})
	assert.NilError(t, err)
	assert.DeepEqual(t, second, first)

	_, err = gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)
}
