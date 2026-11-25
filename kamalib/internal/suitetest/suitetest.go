// Package suitetest reads the vendored copy of the JSON Schema Test Suite
// for the tests of this module. It yields the groups of the suite that
// apply to draft 2020-12 as raw JSON, so it imports no schema package and
// the tests of package jsonschema may use it.
package suitetest

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Dir is the directory of the test files of the suite, slash-separated and
// relative to the root of this module.
const Dir = "pkg/jsonschema/testdata/suite/validation/tests"

// Release is the release of JSON Schema the compatibility of a group is
// matched against: draft 2020-12, which the suite spells by its year.
const Release = 2020

// Group is a test case of the suite: a schema, the schemas it refers to
// and the instances tested against it.
type Group struct {
	// File is the path of the test file, slash-separated and relative to
	// Dir.
	File string
	// Index is the index of the group in the tests array of File.
	Index       int
	Description string
	// Tags name the behaviors the group expects of an implementation, such
	// as format-assertion.
	Tags []string
	// Schema is the raw schema of the group.
	Schema jsontext.Value
	// ExternalSchemas maps the retrieval URIs of the documents Schema
	// refers to to their raw schemas. They are scoped to the group.
	ExternalSchemas map[string]jsontext.Value
	Tests           []Test
}

// Test is an instance and whether the schema of its group accepts it.
type Test struct {
	Description string         `json:"description"`
	Data        jsontext.Value `json:"data"`
	Valid       bool           `json:"valid"`
}

// Groups returns the groups of every test file under Dir in fsys, a file
// system rooted at this module, whose compatibility admits Release. Files
// come in lexical order of their paths and the groups of a file in their
// order in it. A file that does not decode or a group holding a member the
// suite does not define fails t.
func Groups(t testing.TB, fsys fs.FS) []Group {
	t.Helper()

	var groups []Group

	err := fs.WalkDir(fsys, Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".json" {
			return err
		}

		gs, err := readFile(fsys, p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}

		groups = append(groups, gs...)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return groups
}

// HasTag reports whether g carries tag.
func (g Group) HasTag(tag string) bool { return slices.Contains(g.Tags, tag) }

// SchemaRef returns the reference of the schema of g: the path of its file
// relative to the root of this module and the JSON pointer to the schema
// in it as the fragment.
func (g Group) SchemaRef() string { return g.ref("schema") }

// ExternalSchemaRef returns the reference of the external schema of g at
// uri, spelled as SchemaRef spells its schema. The fragment is
// percent-encoded, as the fragment of a URI reference is, since a URI may
// hold characters a fragment may not.
func (g Group) ExternalSchemaRef(uri string) string { return g.ref("externalSchemas", uri) }

func (g Group) ref(tokens ...string) string {
	var frag strings.Builder

	for _, tok := range append([]string{"tests", strconv.Itoa(g.Index)}, tokens...) {
		frag.WriteByte('/')
		frag.WriteString(url.PathEscape(pointerEscaper.Replace(tok)))
	}

	return path.Join(Dir, g.File) + "#" + frag.String()
}

var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

// file is a test file of the suite.
type file struct {
	Tests []group `json:"tests"`
}

// group is a test case as a file holds it.
type group struct {
	Description     string                    `json:"description"`
	Compatibility   string                    `json:"compatibility"`
	Tags            []string                  `json:"tags"`
	Schema          jsontext.Value            `json:"schema"`
	ExternalSchemas map[string]jsontext.Value `json:"externalSchemas"`
	Tests           []Test                    `json:"tests"`
	Comment         string                    `json:"comment"`
	Specification   jsontext.Value            `json:"specification"`
	// Unknown is rejected, unlike elsewhere in this module: a member the
	// suite adds may select the implementations a group applies to, as
	// compatibility and tags do, and a test ignoring it would check groups
	// that do not apply.
	Unknown map[string]jsontext.Value `json:",embed"`
}

func readFile(fsys fs.FS, p string) ([]Group, error) {
	b, err := fs.ReadFile(fsys, p)
	if err != nil {
		return nil, err
	}

	var f file
	if err := jsonv2.Unmarshal(b, &f); err != nil {
		return nil, err
	}

	var groups []Group

	for i, g := range f.Tests {
		if len(g.Unknown) > 0 {
			return nil, fmt.Errorf("group %d: unknown members %v", i,
				slices.Sorted(maps.Keys(g.Unknown)))
		}

		ok, err := compatible(g.Compatibility, Release)
		if err != nil {
			return nil, fmt.Errorf("group %d: %w", i, err)
		}

		if !ok {
			continue
		}

		groups = append(groups, Group{
			File:            strings.TrimPrefix(p, Dir+"/"),
			Index:           i,
			Description:     g.Description,
			Tags:            g.Tags,
			Schema:          g.Schema,
			ExternalSchemas: g.ExternalSchemas,
			Tests:           g.Tests,
		})
	}

	return groups, nil
}

// compatible reports whether the compatibility of a group admits release,
// as the README of the suite defines it. An empty compatibility admits
// every release. The constraints are ordered by their release, and the
// first one whose release reaches release decides; past the last one, the
// last one decides.
func compatible(compatibility string, release int) (bool, error) {
	if compatibility == "" {
		return true, nil
	}

	var ok bool

	for c := range strings.SplitSeq(compatibility, ",") {
		op, num := "", c

		for _, prefix := range []string{"<=", "="} {
			if rest, cut := strings.CutPrefix(c, prefix); cut {
				op, num = prefix, rest
				break
			}
		}

		v, err := strconv.Atoi(num)
		if err != nil {
			return false, fmt.Errorf("compatibility %q: %w", compatibility, err)
		}

		switch op {
		case "":
			ok = release >= v
		case "<=":
			ok = release <= v
		case "=":
			ok = release == v
		}

		if release <= v {
			break
		}
	}

	return ok, nil
}
