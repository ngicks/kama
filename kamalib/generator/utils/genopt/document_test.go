package genopt

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

func TestDocuments(t *testing.T) {
	fsys := fstest.MapFS{
		"a/kama_x.json":     {Data: []byte(`{"default": true}`)},
		"a/kama_x.yaml":     {Data: []byte("default: yaml\n")},
		"a/b/one.json":      {Data: []byte("one")},
		"a/b/two.json":      {Data: []byte("two")},
		"shared/doc.schema": {Data: []byte("shared")},
	}

	l := NewDocumentLoader("kama", "x", fileloader.FormatJson)

	cases := []struct {
		name string
		pkg  func(t *testing.T) *packages.Package
		want []Document
	}{
		{
			name: "default of the formats given",
			pkg: func(t *testing.T) *packages.Package {
				return parsedPackage(t, "m/a", "a", map[string]string{"x.go": "package a\n"})
			},
			want: []Document{{Name: "a/kama_x.json", Content: []byte(`{"default": true}`)}},
		},
		{
			name: "no document",
			pkg: func(t *testing.T) *packages.Package {
				return parsedPackage(t, "m/c", "c", map[string]string{"x.go": "package c\n"})
			},
			want: []Document{},
		},
		{
			name: "every named file, in file name and position order, of any extension",
			pkg: func(t *testing.T) *packages.Package {
				return parsedPackage(t, "m/a/b", "a/b", map[string]string{
					"y.go": "//kama:option-file x=../../shared/doc.schema\n\npackage b\n",
					"x.go": "//kama:option-file x=two.json y=none.json\n" +
						"//kama:option-file x=one.json\n\npackage b\n",
				})
			},
			want: []Document{
				{Name: "a/b/two.json", Content: []byte("two")},
				{Name: "a/b/one.json", Content: []byte("one")},
				{Name: "shared/doc.schema", Content: []byte("shared")},
			},
		},
		{
			name: "one file named twice, spelled two ways",
			pkg: func(t *testing.T) *packages.Package {
				return parsedPackage(t, "m/a/b", "a/b", map[string]string{
					"x.go": "//kama:option-file x=one.json x=./one.json\n" +
						"//kama:option-file x=../b/one.json\n\npackage b\n",
				})
			},
			want: []Document{{Name: "a/b/one.json", Content: []byte("one")}},
		},
		{
			name: "package without Go files",
			pkg: func(t *testing.T) *packages.Package {
				return parsedPackage(t, "m/a", "a", nil)
			},
		},
		{
			name: "external test package skips the default",
			pkg: func(t *testing.T) *packages.Package {
				ext := parsedPackage(
					t,
					"m/a_test",
					"a",
					map[string]string{"x_test.go": "package a_test\n"},
				)
				ext.ID = "m/a_test [m/a.test]"

				return ext
			},
			want: []Document{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			docs, err := l.Documents(fsys, testRoot, c.pkg(t))
			assert.NilError(t, err)
			assert.DeepEqual(t, docs, c.want)
		})
	}
}

// TestDocumentsFragments splits the fragment off a named path when the
// loader takes fragments: one file under two fragments is two documents,
// and the same fragment named again is one.
func TestDocumentsFragments(t *testing.T) {
	fsys := fstest.MapFS{"a/one.json": {Data: []byte("one")}}
	pkg := parsedPackage(t, "m/a", "a", map[string]string{
		"x.go": "//kama:option-file x=one.json#/a x=one.json#/b x=./one.json#/a\n\npackage a\n",
	})

	l := NewDocumentLoader("kama", "x")
	l.Fragments = true

	docs, err := l.Documents(fsys, testRoot, pkg)
	assert.NilError(t, err)
	assert.DeepEqual(t, docs, []Document{
		{Name: "a/one.json", Fragment: "/a", Content: []byte("one")},
		{Name: "a/one.json", Fragment: "/b", Content: []byte("one")},
	})

	l.Fragments = false

	_, err = l.Documents(fsys, testRoot, pkg)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestDocumentsErrors(t *testing.T) {
	l := NewDocumentLoader("kama", "x")

	cases := []struct {
		name     string
		files    map[string]string
		is       error
		contains []string
	}{
		{
			name:     "missing named file",
			files:    map[string]string{"x.go": "//kama:option-file x=none.json\n\npackage a\n"},
			is:       fs.ErrNotExist,
			contains: []string{"x.go:1:1: "},
		},
		{
			name:     "path leaving the root",
			files:    map[string]string{"x.go": "//kama:option-file x=../../a.json\n\npackage a\n"},
			contains: []string{"x.go:1:1: ", `path "../../a.json" leaves the root`},
		},
		{
			name: "directive on a type",
			files: map[string]string{
				"x.go": "package a\n\n//kama:option-file x=a.json\ntype T int\n",
			},
			contains: []string{"x.go:3:1: ", "applies to free-floating comments only"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := l.Documents(fstest.MapFS{}, testRoot, parsedPackage(t, "m/a", "a", c.files))
			assert.ErrorIs(t, err, ErrPackageFile)

			if c.is != nil {
				assert.ErrorIs(t, err, c.is)
			}

			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}
