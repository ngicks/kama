package contextkey

import (
	"context"
	"encoding/json/v2"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/option/und"

	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	pkgfileloader "github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/fileloader"
	_ "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

var opts = Options{Package: "p", KeyTypeFunc: "k"}

// render runs Generator on entries and returns the files, as the
// framework assembles them, keyed by path.
func render(t *testing.T, entries []Entry, opts Options) (map[string][]byte, error) {
	t.Helper()

	in := Input{Path: "t.yaml", Template: Template{Entries: entries}, Options: opts}

	files, err := gentest.Assemble(t, Generator, base.DefaultRegistry, in)
	if err != nil {
		return nil, err
	}

	out := map[string][]byte{}
	for _, f := range files {
		out[f.Path] = f.Content
	}

	return out, nil
}

// procInstance is an instance of Proc.
type procInstance interface {
	Param(p ProcParamPartial) error
	In(doc *fileloader.Document)
	Out(f *overlayfs.Fs)
	Run(ctx context.Context, r workflow.Reporter) error
}

// newInstance returns an instance of Proc whose runs take place at now.
func newInstance(t *testing.T, now time.Time) procInstance {
	t.Helper()

	p := Proc
	p.Now = func() time.Time { return now }

	inst, err := p.Instantiate(context.Background(), false)
	assert.NilError(t, err)

	i, ok := inst.(procInstance)
	assert.Assert(t, ok, "%T", inst)

	return i
}

func TestRenderErrors(t *testing.T) {
	valid := Entry{FileName: "f", Name: "N", Type: "string", DefaultExpr: `""`}

	cases := []struct {
		name    string
		entries []Entry
		opts    Options
	}{
		{"package", []Entry{valid}, Options{Package: "a-b", KeyTypeFunc: "k"}},
		{"key type func", []Entry{valid}, Options{Package: "p"}},
		{"file name", []Entry{{Name: "N", Type: "string", DefaultExpr: `""`}}, opts},
		{
			"file name with dir",
			[]Entry{{FileName: "a/b", Name: "N", Type: "string", DefaultExpr: `""`}},
			opts,
		},
		{"name", []Entry{{FileName: "f", Name: "1N", Type: "string", DefaultExpr: `""`}}, opts},
		{"type", []Entry{{FileName: "f", Name: "N", Type: "[", DefaultExpr: `""`}}, opts},
		{"default expr", []Entry{{FileName: "f", Name: "N", Type: "string"}}, opts},
		{"duplicate file name", []Entry{valid, valid}, opts},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := render(t, c.entries, c.opts)
			assert.Assert(t, err != nil)
		})
	}
}

func TestRenderImports(t *testing.T) {
	files, err := render(t, []Entry{{
		FileName:    "f",
		Name:        "N",
		Type:        "*slog.Logger",
		DefaultExpr: "slog.Default()",
		Imports:     []string{"log/slog", "context"},
	}}, opts)
	assert.NilError(t, err)

	src := string(files["f.kama_contextkey.go"])
	want := `import (
	"context"
	"log/slog"
)
`
	assert.Assert(t, strings.Contains(src, want), src)
}

func TestRenderMarksDeclarations(t *testing.T) {
	files, err := render(t, []Entry{{
		FileName:    "f",
		Name:        "N",
		Type:        "string",
		DefaultExpr: `""`,
	}}, opts)
	assert.NilError(t, err)

	file, err := parser.ParseFile(
		token.NewFileSet(),
		"",
		files["f.kama_contextkey.go"],
		parser.ParseComments|parser.SkipObjectResolution,
	)
	assert.NilError(t, err)

	var names []string

	for _, decl := range file.Decls {
		var (
			name string
			doc  *ast.CommentGroup
		)

		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}

			name, doc = d.Tok.String(), d.Doc
			if vs, ok := d.Specs[0].(*ast.ValueSpec); ok {
				name = vs.Names[0].Name
			}
		case *ast.FuncDecl:
			name, doc = d.Name.Name, d.Doc
		}

		names = append(names, name)

		assert.Assert(t, doc != nil, "%s has no doc comment", name)
		gen, ok := base.DefaultRegistry.Generated(doc)
		assert.Assert(t, ok, name)
		assert.Equal(t, gen, GeneratorId.String(), name)
	}

	assert.DeepEqual(t, names, []string{
		"KeyN",
		"WithN",
		"ValueN",
		"ValueNFallback",
		"ValueNFallbackFn",
		"ValueNDefault",
	})
}

func TestProcParamErrors(t *testing.T) {
	i := newInstance(t, gentest.Now())

	assert.ErrorContains(
		t,
		i.Param(param("../x", "p", "k")),
		"invalid dir",
	)
	assert.ErrorContains(t, i.Param(param("", "", "k")), "package")
	assert.ErrorContains(t, i.Param(param("", "p", "")), "key type func")
}

func TestProcRun(t *testing.T) {
	ctx := context.Background()

	f, err := overlayfs.New(t.TempDir())
	assert.NilError(t, err)

	defer f.Close()

	var data any
	assert.NilError(t, yaml.Unmarshal([]byte(`
entries:
  - {file_name: a, name: A, type: string, default_expr: '""'}
  - {file_name: b, name: B, type: int, default_expr: "1"}
`), &data))

	i := newInstance(t, gentest.Now())
	assert.NilError(t, i.Param(param("gen", "p", "k")))
	i.In(&fileloader.Document{Path: "t.yaml", Data: data})
	i.Out(f)
	assert.NilError(t, i.Run(ctx, gentest.NopReporter{}))

	ov, err := f.Overlay()
	assert.NilError(t, err)
	assert.DeepEqual(t, slices.Sorted(maps.Keys(ov)), []string{
		filepath.Join(f.Root(), "gen", "a.kama_contextkey.go"),
		filepath.Join(f.Root(), "gen", "b.kama_contextkey.go"),
	})
}

func TestProcRunNoEntries(t *testing.T) {
	f, err := overlayfs.New(t.TempDir())
	assert.NilError(t, err)

	defer f.Close()

	i := newInstance(t, gentest.Now())
	assert.NilError(t, i.Param(param("", "p", "k")))
	i.In(&fileloader.Document{Path: "t.yaml", Data: map[string]any{}})
	i.Out(f)
	assert.ErrorIs(t, i.Run(context.Background(), gentest.NopReporter{}), ErrNoEntries)
}

// TestProcRunWarnsUnknownKeys checks that unknown keys of the template file
// are warned about with their key paths and leave the output unchanged.
func TestProcRunWarnsUnknownKeys(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 4, 10, 12, 33, 0, time.UTC)

	run := func(doc string) (*overlayfs.Fs, *gentest.RecordReporter) {
		t.Helper()

		f, err := overlayfs.New(t.TempDir())
		assert.NilError(t, err)
		t.Cleanup(func() { _ = f.Close() })

		var data any
		assert.NilError(t, yaml.Unmarshal([]byte(doc), &data))

		i := newInstance(t, now)
		assert.NilError(t, i.Param(param("gen", "p", "k")))
		i.In(&fileloader.Document{Path: "t.yaml", Data: data})
		i.Out(f)

		r := &gentest.RecordReporter{}
		assert.NilError(t, i.Run(ctx, r))

		return f, r
	}

	clean, r := run(`
entries:
  - {file_name: a, name: A, type: string, default_expr: '""'}
`)
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)

	dirty, r := run(`
version: 1
entries:
  - {file_name: a, name: A, type: string, default_expr: '""', default: x}
`)
	assert.Equal(t, len(r.Warnings), 2, "%v", r.Warnings)

	for _, w := range r.Warnings {
		assert.ErrorIs(t, w, ErrUnknownKey)
	}

	assert.Error(t, r.Warnings[0], "contextkey: unknown key: t.yaml: version")
	assert.Error(t, r.Warnings[1], "contextkey: unknown key: t.yaml: entries.0.default")

	want, err := fs.ReadFile(clean.FS(), "gen/a.kama_contextkey.go")
	assert.NilError(t, err)
	got, err := fs.ReadFile(dirty.FS(), "gen/a.kama_contextkey.go")
	assert.NilError(t, err)
	assert.Equal(t, string(got), string(want))
}

func TestFixtureSpecsBuild(t *testing.T) {
	gentest.BuildSpecs(t, "internal/fixture", "internal/fixture/inline")
}

func TestDecodeTemplate(t *testing.T) {
	got, err := decodeTemplate(map[string]any{
		"entries": []any{
			map[string]any{
				"file_name":    "user",
				"name":         "User",
				"type":         "string",
				"default_expr": `""`,
				"imports":      []any{"time"},
				"extra":        true,
			},
			map[string]any{"File_Name": "ignored", "Name": "Ignored"},
		},
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, got, Template{Entries: []Entry{
		{
			FileName:    "user",
			Name:        "User",
			Type:        "string",
			DefaultExpr: `""`,
			Imports:     []string{"time"},
			Unknown:     map[string]any{"extra": true},
		},
		{Unknown: map[string]any{"File_Name": "ignored", "Name": "Ignored"}},
	}})
	assert.DeepEqual(t, got.UnknownKeys(), [][]string{
		{"entries", "0", "extra"},
		{"entries", "1", "File_Name"},
		{"entries", "1", "Name"},
	})
}

// TestFixtureTemplatesHaveNoUnknownKeys keeps the fixture template files
// free of the warnings Proc reports about unknown keys.
func TestFixtureTemplatesHaveNoUnknownKeys(t *testing.T) {
	fsys := os.DirFS("internal/fixture")
	names, err := fs.Glob(fsys, "templates/*")
	assert.NilError(t, err)

	names = append(names, "inline/contextkey.yaml")

	for _, name := range names {
		var data any
		assert.NilError(t, pkgfileloader.LoadFile(fsys, name, &data))

		tmpl, err := decodeTemplate(data)
		assert.NilError(t, err, name)
		assert.Assert(t, len(tmpl.Entries) > 0, name)
		assert.Equal(t, len(tmpl.UnknownKeys()), 0, "%s: %v", name, tmpl.UnknownKeys())
	}
}

func TestDecodeTemplateRejectsInvalidUtf8(t *testing.T) {
	_, err := decodeTemplate(map[string]any{
		"entries": []any{map[string]any{"name": "\xff"}},
	})
	assert.ErrorContains(t, err, "invalid UTF-8")
}

func TestFileName(t *testing.T) {
	assert.Equal(t, FileName("request_id"), "request_id.kama_contextkey.go")
	// A build constraint suffix stays last, so the file keeps its constraint.
	assert.Equal(t, FileName("sig_linux"), "sig.kama_contextkey_linux.go")
	assert.Equal(t, FileName("sig_linux_amd64"), "sig.kama_contextkey_linux_amd64.go")
}

func TestRenderHeader(t *testing.T) {
	files, err := render(
		t,
		[]Entry{{FileName: "f", Name: "N", Type: "string", DefaultExpr: `""`}},
		opts,
	)
	assert.NilError(t, err)

	want := "// Code generated by kama:contextkey. DO NOT EDIT.\n" +
		"// params:\n//\tkey_type_func: \"k\"\n//\tpackage: \"p\"\n" +
		"// generated: 2026-11-04T10:12:33Z\n\npackage p\n"
	src := string(files["f.kama_contextkey.go"])
	assert.Assert(t, strings.HasPrefix(src, want), src)
}

// TestProcStale runs the proc twice, the second time without one entry and
// at a later time: the file of the entry left out is reported, and the
// file whose header alone would change is kept.
func TestProcStale(t *testing.T) {
	ctx := context.Background()
	f, err := overlayfs.New(t.TempDir())
	assert.NilError(t, err)

	defer f.Close()
	// A file written by hand in the output directory is never stale.
	assert.NilError(
		t,
		f.Write(ctx, "gen/key.go", []byte("package p\n\nfunc k(string) any { return nil }\n")),
	)

	a := map[string]any{"file_name": "a", "name": "A", "type": "string", "default_expr": `""`}
	b := map[string]any{"file_name": "b", "name": "B", "type": "int", "default_expr": "1"}
	run := func(now time.Time, entries ...any) *gentest.RecordReporter {
		t.Helper()

		i := newInstance(t, now)
		assert.NilError(t, i.Param(param("gen", "p", "k")))
		i.In(&fileloader.Document{Path: "t.yaml", Data: map[string]any{"entries": entries}})
		i.Out(f)

		r := &gentest.RecordReporter{}
		assert.NilError(t, i.Run(ctx, r))

		return r
	}

	first := time.Date(2026, 11, 4, 10, 12, 33, 0, time.UTC)
	r := run(first, a, b)
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)
	assert.DeepEqual(t, r.Messages, []string{"2 files written, 0 files unchanged"})

	r = run(first.Add(time.Hour), a)
	assert.DeepEqual(t, r.Messages, []string{"0 files written, 1 file unchanged"})
	assert.Equal(t, len(r.Warnings), 1, "%v", r.Warnings)
	assert.ErrorIs(t, r.Warnings[0], base.ErrStale)
	assert.ErrorContains(
		t,
		r.Warnings[0],
		"gen/b.kama_contextkey.go holds declarations generated by kama:contextkey",
	)
}

// param returns the param a spec holding these keys decodes into. An empty
// argument leaves its key out.
func param(dir, pkg, keyTypeFunc string) ProcParamPartial {
	set := func(s string) und.Und[string] {
		if s == "" {
			return und.Undefined[string]()
		}

		return und.Defined(s)
	}

	return ProcParamPartial{Dir: set(dir), Package: set(pkg), KeyTypeFunc: set(keyTypeFunc)}
}

// TestProcParamDefaults decodes the param of a spec as the workflow does:
// a key left out takes its default, and a key that is set wins.
func TestProcParamDefaults(t *testing.T) {
	cases := []struct {
		name    string
		param   string
		wantDir string
		wantErr string
	}{
		{name: "dir left out", param: `{package: p, key_type_func: k}`, wantDir: "."},
		{name: "dir set", param: `{dir: gen, package: p, key_type_func: k}`, wantDir: "gen"},
		// null is a value: it gives the zero value, not the default.
		{
			name:    "dir null",
			param:   `{dir: null, package: p, key_type_func: k}`,
			wantErr: `invalid dir ""`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var spec workflow.Spec
			assert.NilError(t, yaml.Unmarshal([]byte(`
version: 1
steps:
  root: {proc: overlayfs.open}
  template: {proc: file.load, in: root, param: {base: keys}}
  keys: {proc: contextkey.generate, in: template, out: root, param: `+c.param+`}
`), &spec))

			_, err := spec.Build(context.Background(), workflow.DefaultRegistry)
			if c.wantErr != "" {
				assert.ErrorContains(t, err, c.wantErr)
				return
			}

			assert.NilError(t, err)

			// The workflow decodes a param into plain values and then with
			// encoding/json/v2.
			var raw any
			assert.NilError(t, yaml.Unmarshal([]byte(c.param), &raw))
			b, err := json.Marshal(raw)
			assert.NilError(t, err)

			var p ProcParamPartial
			assert.NilError(t, json.Unmarshal(b, &p))
			assert.Equal(t, DefaultProcParam().Merge(p).ToValue().Dir, c.wantDir)
		})
	}
}
