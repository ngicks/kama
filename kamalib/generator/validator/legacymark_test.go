package validator

import (
	"testing"

	"gotest.tools/v3/assert"
)

// TestLegacyMark generates over a file carrying the mark of the generator
// before the mark took the form kama:validate: the conflict says to delete
// the file.
func TestLegacyMark(t *testing.T) {
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\ntype A struct{ N int }\n",
		"a.kama_validate.go": "package x\n\n" +
			"// Validate validates a.\n//\n//kama:generated kama-validate\n" +
			"func (a A) Validate(int) error { return nil }\n",
	}, testRun{})
	assert.ErrorIs(t, err, ErrConflict)
	assert.ErrorContains(
		t,
		err,
		"it carries //kama:generated kama-validate,"+
			" generated before the mark was renamed to //kama:generated kama:validate;"+
			" delete the file holding it and run again",
	)
}
