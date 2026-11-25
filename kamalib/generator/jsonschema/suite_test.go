package jsonschema

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/internal/suitetest"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// TestSuiteNeverPanics runs Generate over every schema of the test suite
// that applies to draft 2020-12. Each run either gives a file that parses
// as Go and that gofmt formats, or fails with ErrUnsupported or
// ErrConflict. The verbose output counts both outcomes per file of the
// suite.
func TestSuiteNeverPanics(t *testing.T) {
	groups := suitetest.Groups(t, os.DirFS(moduleRoot))
	generated, unsupported := map[string]int{}, map[string]int{}

	for _, g := range groups {
		err := generateSuite(g, false)

		switch {
		case err == nil:
			generated[g.File]++
		case errors.Is(err, ErrUnsupported) || errors.Is(err, ErrConflict):
			unsupported[g.File]++
		default:
			t.Errorf("%s: %s: %v", g.File, g.Description, err)
		}
	}

	files := slices.Compact(
		opiter.WrapSeq(slices.Values(groups)).
			Map(func(g suitetest.Group) string { return g.File }).
			CollectSlice(),
	)

	for _, file := range files {
		t.Logf("%s: %d generated, %d unsupported", file, generated[file], unsupported[file])
	}

	assert.Assert(t, len(groups) > 400, "found %d groups", len(groups))
}

// moduleRoot is the root of this module, relative to the directory of the
// package.
var moduleRoot = filepath.FromSlash("../..")

// suiteRemotes map the URIs a group of the suite refers to, those of its
// external schemas and of the meta-schemas of draft 2020-12, to their files
// relative to the root of this module.
func suiteRemotes(g suitetest.Group) []Remote {
	meta := "https://json-schema.org/draft/2020-12/"
	dir := "pkg/jsonschema/testdata/metaschema/draft2020-12/"

	remotes := []Remote{{Uri: meta + "schema", File: dir + "schema.json"}}

	for _, v := range []string{
		"core", "applicator", "unevaluated", "validation", "meta-data", "format-annotation",
		"content",
	} {
		remotes = append(
			remotes,
			Remote{Uri: meta + "meta/" + v, File: dir + "meta/" + v + ".json"},
		)
	}

	return slices.AppendSeq(
		remotes,
		opiter.WrapSeq2(opiter.MapsSorted(g.ExternalSchemas)).
			OmitL().
			Map(func(uri string) Remote { return Remote{Uri: uri, File: g.ExternalSchemaRef(uri)} }).
			Iter(),
	)
}

// generateSuite runs Generate over the schema of g and checks its output.
// A panic comes back as an error wrapping neither sentinel.
func generateSuite(g suitetest.Group, assertFormats bool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	var doc jsonschema.Schema
	if err := jsonv2.Unmarshal(g.Schema, &doc); err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	files, err := gentest.Prepare(context.Background(), Generator(), base.DefaultRegistry(), Input{
		Doc: &doc,
		Options: Options{
			Package:       "suite",
			TypeName:      "Root",
			Load:          remoteLoader(os.DirFS(moduleRoot), suiteRemotes(g)),
			AssertFormats: assertFormats,
		},
	}, overlayfs.WithFormatter(gentest.AsIs{}))
	if err != nil {
		return err
	}

	content := files[0].Content

	if _, err := parser.ParseFile(token.NewFileSet(), files[0].Path, content, 0); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	if _, err := format.Source(content); err != nil {
		return fmt.Errorf("format: %w", err)
	}

	return nil
}
