package partial

import (
	"testing"

	"gotest.tools/v3/assert"
)

// TestLegacyMark generates over a partial file carrying the mark of the
// generator before the mark took the form kama:partial: the conflict says
// to delete the file.
func TestLegacyMark(t *testing.T) {
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ N int }\n",
		"a.kama_partial.go": "package x\n\n" +
			"// APartial is the partial variant of A.\n//\n//kama:generated kama-partial\n" +
			"type APartial struct{ N *int }\n",
	}, testRun{})
	assert.ErrorIs(t, err, ErrConflict)
	assert.ErrorContains(
		t,
		err,
		"is already declared for the partial variant of A; it carries //kama:generated kama-partial,"+
			" generated before the mark was renamed to //kama:generated kama:partial;"+
			" delete the file holding it and run again",
	)
}
