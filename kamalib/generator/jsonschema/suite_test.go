package jsonschema

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

type suiteGroup struct {
	Description string         `json:"description"`
	Schema      jsontext.Value `json:"schema"`
	Tests       []suiteCase    `json:"tests"`
}

// TestSuiteNeverPanics runs Generate over every schema of the draft 2020-12
// test suite. Each run either gives a file that parses as Go and that
// gofmt leaves as it is, or fails with ErrUnsupported or ErrConflict. The
// verbose output counts both outcomes per file of the suite.
func TestSuiteNeverPanics(t *testing.T) {
	root := filepath.FromSlash("../../pkg/jsonschema/testdata/suite/tests/draft2020-12")

	var groups int

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var gs []suiteGroup
		if err := jsonv2.Unmarshal(b, &gs); err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		generated, unsupported := 0, 0

		for _, g := range gs {
			groups++

			err := generateSuite(g.Schema, false)
			if err == nil {
				generated++
				continue
			}

			if errors.Is(err, ErrUnsupported) || errors.Is(err, ErrConflict) {
				unsupported++
				continue
			}

			t.Errorf("%s: %s: %v", rel, g.Description, err)
		}

		t.Logf("%s: %d generated, %d unsupported", filepath.ToSlash(rel), generated, unsupported)

		return nil
	})
	assert.NilError(t, err)
	assert.Assert(t, groups > 400, "found %d groups", groups)
}

// suiteRemotes map the URIs the suite refers to, those of its remotes
// directory and of the meta-schemas of draft 2020-12, to their files
// relative to the root of this module.
func suiteRemotes() []Remote {
	meta := "https://json-schema.org/draft/2020-12/"
	dir := "pkg/jsonschema/testdata/metaschema/draft2020-12/"

	remotes := []Remote{
		{Uri: "http://localhost:1234/", Dir: "pkg/jsonschema/testdata/suite/remotes"},
		{Uri: meta + "schema", File: dir + "schema.json"},
	}

	for _, v := range []string{
		"core", "applicator", "unevaluated", "validation", "meta-data", "format-annotation",
		"content",
	} {
		remotes = append(
			remotes,
			Remote{Uri: meta + "meta/" + v, File: dir + "meta/" + v + ".json"},
		)
	}

	return remotes
}

// generateSuite runs Generate over raw and checks its output. A panic
// comes back as an error wrapping neither sentinel.
func generateSuite(raw jsontext.Value, assertFormats bool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	var doc jsonschema.Schema
	if err := jsonv2.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	files, _, err := Generate(&doc, Options{
		Package:       "suite",
		TypeName:      "Root",
		Schema:        "schema.json",
		Load:          remoteLoader(os.DirFS(filepath.FromSlash("../..")), suiteRemotes()),
		AssertFormats: assertFormats,
		Now:           gentest.Now,
	})
	if err != nil {
		return err
	}

	content := files[0].Content

	if _, err := parser.ParseFile(token.NewFileSet(), files[0].Path, content, 0); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	formatted, err := format.Source(content)
	if err != nil {
		return fmt.Errorf("format: %w", err)
	}

	if string(formatted) != string(content) {
		return errors.New("gofmt changes the output")
	}

	return nil
}
