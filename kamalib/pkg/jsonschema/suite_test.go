package jsonschema

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

type suiteGroup struct {
	Description string         `json:"description"`
	Schema      jsontext.Value `json:"schema"`
}

// TestSuiteRoundTrip decodes every schema of the draft 2020-12 test suite and encodes it again.
// Both sides are compared in a canonical form: object members are sorted by name, except the
// members of properties, $defs, patternProperties and dependentSchemas, which stay in source
// order. Strings compare by their unescaped value and numbers by their literal text.
func TestSuiteRoundTrip(t *testing.T) {
	root := filepath.Join("testdata", "suite", "tests", "draft2020-12")

	var count int

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var groups []suiteGroup
		if err := jsonv2.Unmarshal(b, &groups); err != nil {
			return err
		}

		for i, g := range groups {
			t.Run(filepath.ToSlash(path)+"/"+g.Description, func(t *testing.T) {
				var s Schema
				assert.NilError(t, jsonv2.Unmarshal(g.Schema, &s), "group %d", i)

				out, err := jsonv2.Marshal(s)
				assert.NilError(t, err)
				assert.Equal(t, canonical(t, out), canonical(t, g.Schema))
			})

			count++
		}

		return nil
	})
	assert.NilError(t, err)
	assert.Assert(t, count > 400, "found %d groups", count)
}

func canonical(t *testing.T, v jsontext.Value) string {
	t.Helper()

	n, err := readNode(jsontext.NewDecoder(bytes.NewReader(v)))
	assert.NilError(t, err)

	var b strings.Builder

	n.write(&b, true)

	return b.String()
}

type node struct {
	kind    jsontext.Kind
	text    string
	members []member
	elems   []node
}

type member struct {
	name  string
	value node
}

func readNode(dec *jsontext.Decoder) (node, error) {
	tok, err := dec.ReadToken()
	if err != nil {
		return node{}, err
	}

	n := node{kind: tok.Kind()}

	switch n.kind {
	case jsontext.KindBeginObject:
		for dec.PeekKind() != jsontext.KindEndObject {
			tok, err := dec.ReadToken()
			if err != nil {
				return node{}, err
			}

			name := tok.String()

			v, err := readNode(dec)
			if err != nil {
				return node{}, err
			}

			n.members = append(n.members, member{name: name, value: v})
		}

		_, err = dec.ReadToken()
	case jsontext.KindBeginArray:
		for dec.PeekKind() != jsontext.KindEndArray {
			v, err := readNode(dec)
			if err != nil {
				return node{}, err
			}

			n.elems = append(n.elems, v)
		}

		_, err = dec.ReadToken()
	case jsontext.KindString:
		n.text = string(quote(tok.String()))
	default:
		n.text = tok.String()
	}

	return n, err
}

// write renders n canonically. schema reports whether n sits at a schema position, where the
// keyword decides how a member value is rendered.
func (n node) write(b *strings.Builder, schema bool) {
	switch n.kind {
	case jsontext.KindBeginObject:
		members := slices.SortedFunc(slices.Values(n.members), func(x, y member) int {
			return cmp.Compare(x.name, y.name)
		})

		b.WriteByte('{')

		for _, m := range members {
			b.Write(quote(m.name))
			b.WriteByte(':')

			switch {
			case schema && slices.Contains(orderedKeywords, m.name):
				m.value.writeOrdered(b)
			case schema && slices.Contains(schemaKeywords, m.name):
				m.value.write(b, true)
			case schema && slices.Contains(schemaListKeywords, m.name):
				m.value.writeElems(b, true)
			default:
				m.value.write(b, false)
			}

			b.WriteByte(',')
		}

		b.WriteByte('}')
	case jsontext.KindBeginArray:
		n.writeElems(b, false)
	default:
		b.WriteString(n.text)
	}
}

func (n node) writeOrdered(b *strings.Builder) {
	b.WriteByte('{')

	for _, m := range n.members {
		b.Write(quote(m.name))
		b.WriteByte(':')
		m.value.write(b, true)
		b.WriteByte(',')
	}

	b.WriteByte('}')
}

func (n node) writeElems(b *strings.Builder, schema bool) {
	b.WriteByte('[')

	for _, e := range n.elems {
		e.write(b, schema)
		b.WriteByte(',')
	}

	b.WriteByte(']')
}

// quote normalizes the escapes of a JSON string. A decoded string is valid UTF-8, so AppendQuote
// does not fail.
func quote(s string) []byte {
	q, _ := jsontext.AppendQuote(nil, s)
	return q
}

var (
	orderedKeywords    = []string{"properties", "$defs", "patternProperties", "dependentSchemas"}
	schemaListKeywords = []string{"allOf", "anyOf", "oneOf", "prefixItems"}
	schemaKeywords     = []string{
		"not", "if", "then", "else", "items", "contains", "additionalProperties",
		"propertyNames", "unevaluatedItems", "unevaluatedProperties", "contentSchema",
	}
)
