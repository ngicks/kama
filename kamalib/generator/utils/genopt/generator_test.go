package genopt

import (
	"errors"
	"go/token"
	"testing"
	"testing/fstest"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

func TestNewLoader(t *testing.T) {
	l := NewLoader[leaf, leaf, leaf]("kama", "x", 2, namesInput{"opts.yaml"})

	assert.DeepEqual(t, l, Loader[leaf, leaf, leaf]{
		ToolName:  "kama",
		Generator: "x",
		Version:   2,
		File:      namesInput{"opts.yaml"},
		PackageFile: fileloader.PathInput{
			Base: "kama_x",
			Formats: []fileloader.Format{
				fileloader.FormatToml, fileloader.FormatYaml, fileloader.FormatJson,
			},
		},
	})
}

func TestNewLoaderPackageFilePriority(t *testing.T) {
	l := NewLoader[leaf, leaf, leaf]("kama", "x", 1, nil)

	loaded, err := l.LoadFiles(fstest.MapFS{
		"a/kama_x.toml": {Data: []byte("version = 1\nvalue = \"toml\"\n")},
		"a/kama_x.yaml": packageDoc("yaml"),
		"a/kama_x.json": {Data: []byte(`{"version":1,"value":"json"}`)},
	}, testRoot, []*packages.Package{
		typedPackage(t, "m/a", "a", map[string]string{"x.go": "package a\n"}),
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, packageValues(loaded), map[string][2]string{
		"m/a": {"a/kama_x.toml", "toml"},
	})
	assert.Equal(t, len(loaded.Warnings), 0)
}

func TestLoadOptionNamed(t *testing.T) {
	fsys := fstest.MapFS{
		"opts.yaml": {Data: []byte("version: 1\npackages:\n  m/a:\n    value: central\n")},
	}
	// The File of the loader is replaced, so this one is never read.
	l := fileLoader("unread.yaml")

	loaded, err := l.LoadOptionNamed(fsys, testRoot, filesPackage(t), &fileloader.PathInput{
		Base: "opts", Formats: []fileloader.Format{fileloader.FormatYaml},
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, loaded.Files, []string{"opts.yaml"})
	assert.Equal(t, loaded.Option.Packages["m/a"].Package.Value, "central")

	loaded, err = l.LoadOptionNamed(fsys, testRoot, filesPackage(t), nil)
	assert.NilError(t, err)
	assert.Equal(t, len(loaded.Files), 0)

	_, err = l.LoadOptionNamed(fsys, testRoot, filesPackage(t), &fileloader.PathInput{
		Base: "missing", Formats: []fileloader.Format{fileloader.FormatYaml},
	})
	assert.ErrorIs(t, err, ErrOptionFile)
	assert.ErrorIs(t, err, fileloader.ErrNotFound)
	assert.ErrorContains(t, err, `"missing"`)
}

func TestLoadedHeaderParams(t *testing.T) {
	cases := []struct {
		name   string
		loaded *testLoaded
		want   map[string]any
	}{
		{
			name:   "nil",
			loaded: nil,
			want:   map[string]any{"option_file": nil, "package_option_file": nil},
		},
		{
			name:   "none",
			loaded: &testLoaded{},
			want:   map[string]any{"option_file": nil, "package_option_file": nil},
		},
		{
			name: "one",
			loaded: &testLoaded{
				Files:        []string{"a.yaml"},
				PackageFiles: map[string]string{"m/a": "a/kama_x.yaml", "m/b": "b/kama_x.yaml"},
			},
			want: map[string]any{"option_file": "a.yaml", "package_option_file": "a/kama_x.yaml"},
		},
		{
			name:   "several",
			loaded: &testLoaded{Files: []string{"a.yaml", "b.yaml"}},
			want: map[string]any{
				"option_file":         []string{"a.yaml", "b.yaml"},
				"package_option_file": nil,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.DeepEqual(t, tc.loaded.HeaderParams("m/a"), tc.want)
		})
	}
}

func TestWrapLoadError(t *testing.T) {
	errFile := errors.New("gen: invalid option file")
	errDirective := errors.New("gen: invalid directive")
	other := errors.New("other")

	cases := []struct {
		name  string
		err   error
		is    []error
		isNot []error
	}{
		{
			name:  "option file",
			err:   ErrOptionFile,
			is:    []error{errFile},
			isNot: []error{errDirective},
		},
		{
			name:  "package file",
			err:   ErrPackageFile,
			is:    []error{errFile},
			isNot: []error{errDirective},
		},
		{name: "directive", err: ErrDirective, is: []error{errDirective}, isNot: []error{errFile}},
		{
			name: "both",
			err:  errors.Join(ErrOptionFile, ErrDirective),
			is:   []error{errFile, errDirective},
		},
		{name: "other", err: other, is: []error{other}, isNot: []error{errFile, errDirective}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := WrapLoadError(tc.err, errFile, errDirective)
			assert.ErrorIs(t, err, tc.err)

			for _, is := range tc.is {
				assert.ErrorIs(t, err, is)
			}

			for _, isNot := range tc.isNot {
				assert.Assert(t, !errors.Is(err, isNot))
			}
		})
	}
}

func TestTargetDescribe(t *testing.T) {
	cases := []struct {
		target Target
		want   string
	}{
		{Target{Kind: TargetFreeFloating, PkgPath: "m/a"}, "a free-floating comment"},
		{Target{Kind: TargetTypeGroup, PkgPath: "m/a"}, "a type group"},
		{Target{Kind: TargetType, PkgPath: "m/a", Type: "T"}, "type m/a.T"},
		{Target{Kind: TargetField, PkgPath: "m/a", Type: "T", Field: "F"}, "field m/a.T.F"},
		{Target{Kind: TargetPackage, PkgPath: "m/a"}, "package m/a"},
		{Target{}, "an unknown target"},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.target.Describe(), tc.want)
	}

	assert.Equal(t, Target{PkgPath: "m/a", Type: "T", Field: "F"}.FieldName(), "m/a.T.F")
}

func TestSourceOriginSpelling(t *testing.T) {
	directive := Source{
		Kind: SourceDirective,
		Name: "a.go",
		Pos:  token.Position{Filename: "a.go", Line: 3, Column: 1},
	}
	file := Source{Kind: SourceFile, Name: "opts.yaml", KeyPath: []string{"packages", "m/a"}}
	pkgFile := Source{Kind: SourcePackageFile, Name: "a/kama_x.yaml"}

	cases := []struct {
		name     string
		src      Source
		option   string
		keys     []string
		origin   string
		spelling string
	}{
		{
			name:     "bare directive",
			src:      directive,
			origin:   "a.go:3:1",
			spelling: "//kama:x",
		},
		{
			name:     "named directive",
			src:      directive,
			option:   "sub",
			keys:     []string{"ignored"},
			origin:   "a.go:3:1",
			spelling: "//kama:x+sub",
		},
		{
			name:     "type entry",
			src:      file,
			origin:   `opts.yaml: packages."m/a"`,
			spelling: "option file type entry",
		},
		{
			name:     "name as key",
			src:      file,
			option:   "hook",
			origin:   `opts.yaml: packages."m/a".hook`,
			spelling: "option file hook option",
		},
		{
			name:     "keys",
			src:      pkgFile,
			option:   "v",
			keys:     []string{"validators", "0"},
			origin:   "a/kama_x.yaml: validators.0",
			spelling: "package option file v option",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.src.Origin(tc.option, tc.keys), tc.origin)
			assert.Equal(t, tc.src.Spelling("kama", "x", "+", tc.option), tc.spelling)
		})
	}

	// Origin leaves the spare capacity of the key path of the source alone.
	file.KeyPath = append(make([]string, 0, 4), "packages", "m/a")
	assert.Equal(t, file.Origin("a", nil), `opts.yaml: packages."m/a".a`)
	assert.Equal(t, file.KeyPath[:3][2], "")
}
