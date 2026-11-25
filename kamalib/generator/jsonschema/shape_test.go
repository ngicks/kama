package jsonschema

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"maps"
	"os"
	"path"
	"slices"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/internal/suitetest"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// TestSuiteRootShapes checks the Go shape of the root of every group of
// the top-level files of the suite that generates, so that a typed shape
// cannot turn into raw JSON text unnoticed: an object of properties is a
// struct or a product, a oneOf or an anyOf is a union, and a schema of one
// kind is no raw JSON text, whatever other keywords it holds. The verbose
// output counts the roots per shape.
func TestSuiteRootShapes(t *testing.T) {
	counts := map[string]int{}

	for _, g := range suitetest.Groups(t, os.DirFS(moduleRoot)) {
		if path.Dir(g.File) != "." || generateSuite(g, false) != nil {
			continue
		}

		doc := suiteDoc(t, g.Schema)
		shape := rootShape(doc)
		counts[shape]++

		if want := wantShapes(doc); want != nil {
			assert.Assert(
				t,
				slices.Contains(want, shape),
				"%s: %s: root is %s, want one of %v",
				g.File,
				g.Description,
				shape,
				want,
			)
		}
	}

	for _, k := range slices.Sorted(maps.Keys(counts)) {
		t.Logf("%s: %d", k, counts[k])
	}
}

// wantShapes returns the shapes the root of doc may take by its keywords,
// or nil when they leave it open.
func wantShapes(doc *jsonschema.Schema) []string {
	switch {
	case doc.Bool.IsSome() || doc.Ref.IsSome() || doc.Enum != nil || doc.Const != nil:
		return nil
	case unionKeyword(doc) != "":
		return []string{"union", "nullable"}
	case len(doc.Type.Names) != 1 || doc.Type.Names[0] == "null":
		return nil
	case doc.Type.Names[0] == "object" && doc.Properties != nil:
		return []string{"struct", "product"}
	}

	return []string{"struct", "product", "container", "scalar", "enum"}
}

func suiteDoc(t *testing.T, raw jsontext.Value) *jsonschema.Schema {
	t.Helper()

	var doc jsonschema.Schema
	assert.NilError(t, jsonv2.Unmarshal(raw, &doc))

	return &doc
}

// rootShape names the Go shape of the root of doc.
func rootShape(doc *jsonschema.Schema) string {
	r, s, err := resolved(doc, "")

	switch {
	case err != nil:
		return "error"
	case raw(doc, ""):
		return "raw"
	case nullable(doc, ""):
		return "nullable"
	case union(s):
		return "union"
	case product(doc, r, s):
		return "product"
	case structKind(s):
		return "struct"
	case stringEnum(s):
		return "enum"
	case objectKind(s) && mapOnly(s), slices.Equal(typeKinds(s), []string{"array"}):
		return "container"
	}

	return "scalar"
}
