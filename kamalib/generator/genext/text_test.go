package genext

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

func decodeText(t *testing.T, format fileloader.Format, src string) TextDocument {
	t.Helper()

	var data any
	assert.NilError(t, fileloader.Decode(strings.NewReader(src), format, &data))

	return TextDocument{Name: "ext" + format.Extensions()[0], Data: data}
}

const yamlText = `version: 1
imports:
  deque: github.com/gammazero/deque
types:
  example.com/x.Box:
    holder:
      role: v
      type: T
      guard: "{{.V}}.Has()"
      value: "{{.V}}.Get()"
    extra: 1
`

func TestLoadTextYaml(t *testing.T) {
	ss, warnings, err := LoadText([]TextDocument{decodeText(t, fileloader.FormatYaml, yamlText)})
	assert.NilError(t, err)
	assert.Equal(t, len(ss), 1)
	assert.Equal(t, ss[0].Name(), "ext.yaml#example.com/x.Box")
	assert.Equal(t, len(warnings), 1)
	assert.Error(t, warnings[0], `genext: ext.yaml: types."example.com/x.Box".extra: unknown key`)
}

func TestLoadTextErrors(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"version", `{"version": 2}`, "ext.json: version: unsupported version 2; want 1"},
		{
			"std import",
			`{"version": 1, "imports": {"s": "strings"}}`,
			`imports.s: strings is a standard library package; write {{std "strings"}} in place of it`,
		},
		{
			"bad key",
			`{"version": 1, "types": {"Box": {}}}`,
			`types.Box: "Box"; want <import path>.<type name>`,
		},
		{
			"yields",
			`{"version": 1, "types": {"x.Box": {"container": {"role": "v", "yields": "all"}}}}`,
			`container.yields: "all"; want one of values, pairs`,
		},
		{
			"role",
			`{"version": 1, "types": {"x.Box": {"holder": {"role": "a-b"}}}}`,
			`holder.role: "a-b" is no Go identifier`,
		},
		{
			"template",
			`{"version": 1, "types": {"x.Box": {"holder": {"role": "v", "value": "{{.V"}}}}`,
			`types."x.Box".holder.value: template: `,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := LoadText([]TextDocument{decodeText(t, fileloader.FormatJson, tc.src)})
			assert.ErrorIs(t, err, ErrTextFile)
			assert.ErrorContains(t, err, tc.want)
		})
	}

	// Two files describing one type.
	doc := decodeText(t, fileloader.FormatYaml, yamlText)
	other := doc
	other.Name = "other.yaml"

	_, _, err := LoadText([]TextDocument{doc, other})
	assert.ErrorContains(
		t,
		err,
		`other.yaml: types."example.com/x.Box": type example.com/x.Box is described in ext.yaml too`,
	)
}

func TestIsStd(t *testing.T) {
	for _, p := range []string{"strings", "net/netip", "encoding/json/v2"} {
		assert.Assert(t, isStd(p), p)
	}

	for _, p := range []string{"", "example.com/x", "github.com/ngicks/option/opt", "nostd/pkg"} {
		assert.Assert(t, !isStd(p), p)
	}
}

func TestPrependTypeSupports(t *testing.T) {
	reg := NewRegistry(support{name: "a"}, support{name: "b"})
	reg.PrependTypeSupports(support{name: "b", tag: "2"}, support{name: "c"})
	assert.DeepEqual(t, names(reg), []string{"b", "c", "a"})
}
