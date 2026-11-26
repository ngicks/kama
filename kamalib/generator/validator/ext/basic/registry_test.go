package basic

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

// TestDefaultRegistry lists the validators the package registers.
func TestDefaultRegistry(t *testing.T) {
	assert.DeepEqual(t, ext.DefaultRegistry().Names(), []string{
		"and",
		"closed",
		"cmp",
		"const",
		"contains",
		"decode",
		"format",
		"if",
		"items",
		"json",
		"keys",
		"kind",
		"length",
		"members",
		"multipleof",
		"nil",
		"not",
		"null",
		"oneof",
		"or",
		"prefix",
		"props",
		"ref",
		"regexp",
		"required",
		"requires",
		"runes",
		"unevalitems",
		"unevalprops",
		"unique",
		"zero",
	})
}
