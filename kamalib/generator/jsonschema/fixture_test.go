package jsonschema

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// fixtureNow is the time the headers of the fixture files record.
var fixtureNow = time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)

// fixtures are the directories under internal/fixture. Each holds
// schema.json and the file Generate must produce from it, named after the
// directory, which is also the package name.
var fixtures = []string{
	"object", "nullable", "recursive", "oneof", "anyof", "allof",
	"prefilter", "nullablecycle", "multikind",
}

func TestFixtures(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join("internal", "fixture", name)

			src, err := os.ReadFile(filepath.Join(dir, "schema.json"))
			assert.NilError(t, err)

			var doc jsonschema.Schema
			assert.NilError(t, jsonv2.Unmarshal(src, &doc))

			files, warnings, err := Generate(&doc, Options{
				Package: name,
				Schema:  "schema.json",
				Now:     func() time.Time { return fixtureNow },
			})
			assert.NilError(t, err)
			assert.Equal(t, len(warnings), 0)
			assert.Equal(t, len(files), 1)
			assert.Equal(t, files[0].Path, name+".kama_jsonschema.go")

			want, err := os.ReadFile(filepath.Join(dir, files[0].Path))
			assert.NilError(t, err)
			assert.DeepEqual(t, string(files[0].Content), string(want))
		})
	}
}
