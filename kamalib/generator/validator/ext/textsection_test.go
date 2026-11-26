package ext_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

func loadText(t *testing.T, src string) ([]genext.TypeSupport, []error, error) {
	t.Helper()

	var data any
	assert.NilError(t, fileloader.Decode(strings.NewReader(src), fileloader.FormatToml, &data))

	return genext.LoadText([]genext.TextDocument{{Name: "ext.toml", Data: data}})
}

// TestTextDocExample loads the example file of the doc of package genext,
// whose sections of the validator this package reads.
func TestTextDocExample(t *testing.T) {
	f, err := parser.ParseFile(
		token.NewFileSet(),
		"../../genext/doc.go",
		nil,
		parser.PackageClauseOnly|parser.ParseComments,
	)
	assert.NilError(t, err)

	doc := f.Doc.Text()
	start := strings.Index(doc, "\tversion = 1\n")
	assert.Assert(t, start >= 0, "no example in\n%s", doc)

	ss, warnings, err := loadText(t, strings.ReplaceAll(doc[start+1:], "\n\t", "\n"))
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)

	var testers int

	for _, s := range ss {
		if _, ok := s.(ext.PresenceTester); ok {
			testers++
		}
	}

	// Four types and three validate sections.
	assert.Equal(t, len(ss), 7)
	assert.Equal(t, testers, 3)
}

func TestTextSectionErrors(t *testing.T) {
	const head = "version = 1\n[types.\"x.Box\".generator.validate]\n"

	for _, tc := range []struct{ src, want string }{
		{
			`compare = { ops = ["~"], operand = "string", cond = "x" }`,
			`compare.ops.0: "~"; want one of`,
		},
		{
			`compare = { ops = ["=="], operand = "int", cond = "x" }`,
			`compare.operand: "int"; want string or number`,
		},
		{"length = { expr = \"{{.V\" }", `types."x.Box".generator.validate.length.expr: template: `},
	} {
		_, _, err := loadText(t, head+tc.src)
		assert.ErrorIs(t, err, genext.ErrTextFile)
		assert.ErrorContains(t, err, tc.want)
	}

	_, warnings, err := loadText(t, head+"present = { cond = \"x\", wrods = \"y\" }\nlenght = 1\n")
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 2, "%v", warnings)
	assert.ErrorContains(t, warnings[0], `generator.validate.lenght: unknown key`)
	assert.ErrorContains(t, warnings[1], `generator.validate.present.wrods: unknown key`)
}
