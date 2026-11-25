package contextkey

import (
	"context"
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

	"gotest.tools/v3/assert"

	pkgfileloader "github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procpackages "github.com/ngicks/kama/kamalib/proc/gopackages"
	_ "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

var opts = Options{Package: "p", KeyTypeFunc: "k"}

// render runs Generator on entries and returns the files, as the
// framework assembles them, keyed by path.
func render(t *testing.T, entries []Entry, opts Options) (map[string][]byte, error) {
	t.Helper()

	in := Input{Path: "t.yaml", Template: Template{Entries: entries}, Options: opts}

	files, err := gentest.Assemble(t, Generator(), base.DefaultRegistry(), in)
	if err != nil {
		return nil, err
	}

	out := map[string][]byte{}
	for _, f := range files {
		out[f.Path] = f.Content
	}

	return out, nil
}

// procInstance is an instance of the proc of NewProc.
type procInstance interface {
	Param(p ProcParamPartial) error
	In(pkgs *procpackages.Packages)
	Out(f *overlayfs.Fs)
	Run(ctx context.Context, r workflow.Reporter) error
}

// newInstance returns an instance of the proc whose runs take place at now.
func newInstance(t *testing.T, now time.Time) procInstance {
	t.Helper()

	p := proc
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
		gen, ok := base.DefaultRegistry().Generated(doc)
		assert.Assert(t, ok, name)
		assert.Equal(t, gen, GeneratorId().String(), name)
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

	assert.ErrorContains(t, i.Param(param("1k")), `key type func "1k" is not an identifier`)
	assert.NilError(t, i.Param(param("")))
}

func TestProcRun(t *testing.T) {
	f := procFs(t, map[string]string{
		"gen/key.go": "package gen\n",
		"gen/kama_contextkey.yaml": `
entries:
  - {file_name: a, name: A, type: string, default_expr: '""'}
  - {file_name: b, name: B, type: int, default_expr: "1"}
`,
		// A package naming its template files does not read the default one.
		"named/key.go": "//kama:option-file contextkey=one.json contextkey=../shared/two.toml\n" +
			"package named\n",
		"named/kama_contextkey.yaml": "entries: [{file_name: x}]\n",
		"named/one.json": `{"key_type_func": "own", "entries": [
			{"file_name": "c", "name": "C", "type": "string", "default_expr": "\"\""}]}`,
		"shared/two.toml": "[[entries]]\nfile_name = \"d\"\nname = \"D\"\ntype = \"int\"\n" +
			"default_expr = \"1\"\n",
		// A package without a template file generates nothing.
		"none/none.go": "package none\n",
	})

	r, err := runProc(t, f, param("k"), gentest.Now())
	assert.NilError(t, err)
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)

	ov, err := f.Overlay()
	assert.NilError(t, err)
	assert.DeepEqual(t, slices.Sorted(maps.Keys(ov)), []string{
		filepath.Join(f.Root(), "gen", "a.kama_contextkey.go"),
		filepath.Join(f.Root(), "gen", "b.kama_contextkey.go"),
		filepath.Join(f.Root(), "named", "c.kama_contextkey.go"),
		filepath.Join(f.Root(), "named", "d.kama_contextkey.go"),
	})

	header := func(name string) string {
		t.Helper()

		b, err := fs.ReadFile(f.FS(), name)
		assert.NilError(t, err)

		hdr, _, _ := strings.Cut(string(b), "// generated:")

		return hdr
	}

	const generated = "// Code generated by kama:contextkey. DO NOT EDIT.\n// params:\n"

	assert.Equal(
		t,
		header("gen/a.kama_contextkey.go"),
		generated+"//\tkey_type_func: \"k\"\n//\ttemplate: \"gen/kama_contextkey.yaml\"\n",
	)
	assert.Equal(
		t,
		header("named/c.kama_contextkey.go"),
		generated+"//\tkey_type_func: \"own\"\n//\ttemplate: \"named/one.json\"\n",
	)
	assert.Assert(t, strings.Contains(header("named/d.kama_contextkey.go"), `"shared/two.toml"`))
}

func TestProcRunErrors(t *testing.T) {
	entry := func(name string) string {
		return "entries: [{file_name: " + name + ", name: N, type: int, default_expr: '1'}]\n"
	}

	cases := []struct {
		name  string
		files map[string]string
		param ProcParamPartial
		is    error
		msg   string
	}{
		{
			name:  "no entries",
			files: map[string]string{"gen/kama_contextkey.yaml": "{}\n"},
			param: param("k"),
			is:    ErrNoEntries,
			msg:   "gen/kama_contextkey.yaml",
		},
		{
			name:  "no key type func",
			files: map[string]string{"gen/kama_contextkey.yaml": entry("a")},
			msg:   `gen/kama_contextkey.yaml: contextkey: key type func "" is not an identifier`,
		},
		{
			name: "file name two template files share",
			files: map[string]string{
				"gen/key.go": "//kama:option-file contextkey=a.yaml contextkey=b.yaml\npackage gen\n",
				"gen/a.yaml": entry("a"),
				"gen/b.yaml": entry("a"),
			},
			param: param("k"),
			msg:   `"gen/a.kama_contextkey.go" is yielded twice`,
		},
		{
			name: "missing template file",
			files: map[string]string{
				"gen/key.go": "//kama:option-file contextkey=none.yaml\npackage gen\n",
			},
			param: param("k"),
			is:    fs.ErrNotExist,
			msg:   "gen/key.go:1:1",
		},
		{
			name:  "undecodable template file",
			files: map[string]string{"gen/kama_contextkey.yaml": "entries: [\n"},
			param: param("k"),
			msg:   "contextkey: gen/kama_contextkey.yaml: ",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := c.files["gen/key.go"]; !ok {
				c.files["gen/key.go"] = "package gen\n"
			}

			_, err := runProc(t, procFs(t, c.files), c.param, gentest.Now())
			if c.is != nil {
				assert.ErrorIs(t, err, c.is)
			}

			assert.ErrorContains(t, err, c.msg)
		})
	}
}

// TestProcRunWarnsUnknownKeys checks that unknown keys of the template file
// are warned about with their key paths and leave the output unchanged.
func TestProcRunWarnsUnknownKeys(t *testing.T) {
	run := func(doc string) (*overlayfs.Fs, *gentest.RecordReporter) {
		t.Helper()

		f := procFs(
			t,
			map[string]string{"gen/key.go": "package gen\n", "gen/kama_contextkey.yaml": doc},
		)

		r, err := runProc(t, f, param("k"), gentest.Now())
		assert.NilError(t, err)

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

	assert.Error(t, r.Warnings[0], "contextkey: unknown key: gen/kama_contextkey.yaml: version")
	assert.Error(
		t,
		r.Warnings[1],
		"contextkey: unknown key: gen/kama_contextkey.yaml: entries.0.default",
	)

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
// free of the warnings the proc reports about unknown keys.
func TestFixtureTemplatesHaveNoUnknownKeys(t *testing.T) {
	fsys := os.DirFS("internal/fixture")
	names, err := fs.Glob(fsys, "templates/*")
	assert.NilError(t, err)

	names = append(names, "inline/kama_contextkey.yaml")

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
		"// params:\n//\tkey_type_func: \"k\"\n//\ttemplate: \"t.yaml\"\n" +
		"// generated: 2026-11-04T10:12:33Z\n\npackage p\n"
	src := string(files["f.kama_contextkey.go"])
	assert.Assert(t, strings.HasPrefix(src, want), src)
}

// TestProcStale runs the proc three times: the second time without one
// entry and at a later time, the third time with the package naming no
// template file. The files of the entries left out are reported, and the
// file whose header alone would change is kept.
func TestProcStale(t *testing.T) {
	ctx := context.Background()
	// A file written by hand in the directory of the package is never
	// stale.
	key := "package gen\n\nfunc k(string) any { return nil }\n"
	f := procFs(t, map[string]string{"gen/key.go": "//kama:option-file contextkey=t.json\n" + key})

	a := `{"file_name": "a", "name": "A", "type": "string", "default_expr": "\"\""}`
	b := `{"file_name": "b", "name": "B", "type": "int", "default_expr": "1"}`
	run := func(now time.Time, entries ...string) *gentest.RecordReporter {
		t.Helper()

		assert.NilError(t, f.Write(ctx, "gen/t.json", []byte(
			`{"entries": [`+strings.Join(entries, ",")+`]}`,
		)))

		r, err := runProc(t, f, param("k"), now)
		assert.NilError(t, err)

		return r
	}
	staleFiles := func(r *gentest.RecordReporter) []string {
		t.Helper()

		var out []string

		for _, w := range r.Warnings {
			assert.ErrorIs(t, w, base.ErrStale)

			name, _, _ := strings.Cut(w.Error(), " holds declarations generated by kama:contextkey")
			out = append(out, name[strings.LastIndex(name, " ")+1:])
		}

		return out
	}

	first := time.Date(2026, 11, 4, 10, 12, 33, 0, time.UTC)
	r := run(first, a, b)
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)
	assert.DeepEqual(t, r.Messages, []string{"2 files written, 0 files unchanged"})

	r = run(first.Add(time.Hour), a)
	assert.DeepEqual(t, r.Messages, []string{"0 files written, 1 file unchanged"})
	assert.DeepEqual(t, staleFiles(r), []string{"gen/b.kama_contextkey.go"})

	assert.NilError(t, f.Write(ctx, "gen/key.go", []byte(key)))

	r = run(first.Add(2 * time.Hour))
	assert.DeepEqual(
		t,
		staleFiles(r),
		[]string{"gen/a.kama_contextkey.go", "gen/b.kama_contextkey.go"},
	)
}

// param returns the param a spec holding key_type_func decodes into. An
// empty argument leaves the key out.
func param(keyTypeFunc string) ProcParamPartial {
	if keyTypeFunc == "" {
		return ProcParamPartial{}
	}

	return ProcParamPartial{KeyTypeFunc: und.Defined(keyTypeFunc)}
}

// procFs returns an overlay over a new module holding files, keyed by
// slash-separated path relative to its root.
func procFs(t *testing.T, files map[string]string) *overlayfs.Fs {
	t.Helper()

	dir := t.TempDir()
	files = maps.Clone(files)
	files["go.mod"] = "module example.com/m\n\ngo 1.25\n"

	for name, src := range files {
		name = filepath.Join(dir, filepath.FromSlash(name))
		assert.NilError(t, os.MkdirAll(filepath.Dir(name), 0o755))
		assert.NilError(t, os.WriteFile(name, []byte(src), 0o644))
	}

	f, err := overlayfs.New(dir)
	assert.NilError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	return f
}

// runProc loads the packages of f and runs the proc with p over them into
// f at now.
func runProc(
	t *testing.T,
	f *overlayfs.Fs,
	p ProcParamPartial,
	now time.Time,
) (*gentest.RecordReporter, error) {
	t.Helper()

	ctx := context.Background()

	pkgs, err := gopackages.Load(ctx, f, gopackages.Options{Patterns: []string{"./..."}})
	assert.NilError(t, err)

	i := newInstance(t, now)
	assert.NilError(t, i.Param(p))
	i.In(&procpackages.Packages{Pkgs: pkgs})
	i.Out(f)

	r := &gentest.RecordReporter{}

	return r, i.Run(ctx, r)
}
