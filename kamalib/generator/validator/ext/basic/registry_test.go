package basic

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

// TestDefaultRegistry lists the validators the package registers.
func TestDefaultRegistry(t *testing.T) {
	assert.DeepEqual(t, ext.DefaultRegistry().Names(), []string{
		"always",
		"closed",
		"cmp",
		"const",
		"contains",
		"decode",
		"format",
		"items",
		"json",
		"keys",
		"kind",
		"length",
		"members",
		"multipleof",
		"never",
		"nil",
		"null",
		"oneof",
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
