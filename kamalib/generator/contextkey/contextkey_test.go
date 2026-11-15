package contextkey

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ngicks/kama/kamalib/generator/base"

	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/fileloader"
	_ "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

var opts = Options{Package: "p", KeyTypeFunc: "k"}

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
			_, err := Render(c.entries, c.opts)
			assert.Assert(t, err != nil)
		})
	}
}

func TestRenderImports(t *testing.T) {
	files, err := Render([]Entry{{
		FileName:    "f",
		Name:        "N",
		Type:        "*slog.Logger",
		DefaultExpr: "slog.Default()",
		Imports:     []string{"log/slog", "context"},
	}}, opts)
	assert.NilError(t, err)

	src := string(files["f.contextkey.go"])
	want := `import (
	"context"
	"log/slog"
)
`
	assert.Assert(t, strings.Contains(src, want), src)
}

func TestRenderMarksDeclarations(t *testing.T) {
	files, err := Render([]Entry{{
		FileName:    "f",
		Name:        "N",
		Type:        "string",
		DefaultExpr: `""`,
	}}, opts)
	assert.NilError(t, err)

	file, err := parser.ParseFile(
		token.NewFileSet(),
		"",
		files["f.contextkey.go"],
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
		assert.Equal(t, doc.List[len(doc.List)-1].Text, genmark.Marker(GeneratorName), name)
		gen, ok := genmark.DefaultRegistry.Generated(doc)
		assert.Assert(t, ok, name)
		assert.Equal(t, gen, GeneratorName, name)
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
	i := base.NewInstance(newGenerator())

	assert.ErrorContains(
		t,
		i.Param(ProcParam{Dir: "../x", Package: "p", KeyTypeFunc: "k"}),
		"invalid dir",
	)
	assert.ErrorContains(t, i.Param(ProcParam{KeyTypeFunc: "k"}), "package")
	assert.ErrorContains(t, i.Param(ProcParam{Package: "p"}), "key type func")
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

	i := base.NewInstance(newGenerator())
	assert.NilError(t, i.Param(ProcParam{Dir: "gen", Package: "p", KeyTypeFunc: "k"}))
	i.In(&fileloader.Document{Path: "t.yaml", Data: data})
	i.Out(f)
	assert.NilError(t, i.Run(ctx, nopReporter{}))

	ov, err := f.Overlay()
	assert.NilError(t, err)
	assert.DeepEqual(t, slices.Sorted(maps.Keys(ov)), []string{
		filepath.Join(f.Root(), "gen", "a.contextkey.go"),
		filepath.Join(f.Root(), "gen", "b.contextkey.go"),
	})
}

func TestProcRunNoEntries(t *testing.T) {
	i := base.NewInstance(newGenerator())
	assert.NilError(t, i.Param(ProcParam{Package: "p", KeyTypeFunc: "k"}))
	i.In(&fileloader.Document{Path: "t.yaml", Data: map[string]any{}})
	assert.ErrorIs(t, i.Run(context.Background(), nopReporter{}), ErrNoEntries)
}

func TestFixtureSpecsBuild(t *testing.T) {
	for _, name := range []string{
		"internal/fixture/spec.yaml",
		"internal/fixture/inline/spec.yaml",
	} {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(name)
			assert.NilError(t, err)

			var spec workflow.Spec
			assert.NilError(t, yaml.Unmarshal(b, &spec))

			// Build only dry-runs, so the fixture directory is not touched.
			_, err = spec.Build(context.Background(), workflow.DefaultRegistry)
			assert.NilError(t, err)
		})
	}
}

// nopReporter drops what an instance reports.
type nopReporter struct{}

func (nopReporter) Warn(error)      {}
func (nopReporter) Progress(string) {}

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
		},
		{},
	}})
}

func TestDecodeTemplateRejectsInvalidUtf8(t *testing.T) {
	_, err := decodeTemplate(map[string]any{
		"entries": []any{map[string]any{"name": "\xff"}},
	})
	assert.ErrorContains(t, err, "invalid UTF-8")
}

func TestFileName(t *testing.T) {
	assert.Equal(t, FileName("request_id"), "request_id.contextkey.go")
	// A build constraint suffix stays last, so the file keeps its constraint.
	assert.Equal(t, FileName("sig_linux"), "sig.contextkey_linux.go")
	assert.Equal(t, FileName("sig_linux_amd64"), "sig.contextkey_linux_amd64.go")
}

func TestRenderHeader(t *testing.T) {
	o := opts
	o.Now = func() time.Time { return time.Date(2026, 11, 4, 10, 12, 33, 0, time.UTC) }
	files, err := Render([]Entry{{FileName: "f", Name: "N", Type: "string", DefaultExpr: `""`}}, o)
	assert.NilError(t, err)
	want := "// Code generated by " + generatorPath + ". DO NOT EDIT.\n" +
		"// params:\n//\tkey_type_func: \"k\"\n//\tpackage: \"p\"\n" +
		"// generated: 2026-11-04T10:12:33Z\n\npackage p\n"
	src := string(files["f.contextkey.go"])
	assert.Assert(t, strings.HasPrefix(src, want), src)
}

type recordReporter struct {
	warnings []error
	progress []string
}

func (r *recordReporter) Warn(w error)        { r.warnings = append(r.warnings, w) }
func (r *recordReporter) Progress(msg string) { r.progress = append(r.progress, msg) }

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
	run := func(now time.Time, entries ...any) *recordReporter {
		t.Helper()
		g := &generator{}
		i := base.NewInstance[ProcParam, *fileloader.Document](g)
		assert.NilError(t, i.Param(ProcParam{Dir: "gen", Package: "p", KeyTypeFunc: "k"}))
		g.opts.Now = func() time.Time { return now }
		i.In(&fileloader.Document{Path: "t.yaml", Data: map[string]any{"entries": entries}})
		i.Out(f)
		r := &recordReporter{}
		assert.NilError(t, i.Run(ctx, r))
		return r
	}

	first := time.Date(2026, 11, 4, 10, 12, 33, 0, time.UTC)
	r := run(first, a, b)
	assert.Equal(t, len(r.warnings), 0, "%v", r.warnings)
	assert.DeepEqual(t, r.progress, []string{"2 files written, 0 files unchanged"})

	r = run(first.Add(time.Hour), a)
	assert.DeepEqual(t, r.progress, []string{"0 files written, 1 file unchanged"})
	assert.Equal(t, len(r.warnings), 1, "%v", r.warnings)
	assert.ErrorIs(t, r.warnings[0], base.ErrStale)
	assert.ErrorContains(t, r.warnings[0], "gen/b.contextkey.go was generated by kama-contextkey")
}
