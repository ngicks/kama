package base_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procgopackages "github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

// rootEnv returns the env of a run writing into a new overlay over root,
// opened with opts.
func rootEnv(
	t *testing.T,
	root string,
	opts ...overlayfs.Option,
) (base.Env, *overlayfs.Fs, *gentest.RecordReporter) {
	t.Helper()

	f, err := overlayfs.New(root, opts...)
	assert.NilError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	reg := genmark.NewRegistry(base.ToolName)
	assert.NilError(t, reg.Register(driveId.String()))

	r := &gentest.RecordReporter{}

	return base.Env{Reporter: r, Now: gentest.Now(), Registry: reg, Out: f.FS()}, f, r
}

func whole(path, content string) base.Whole {
	return base.Whole{Path: path, Content: []byte(content)}
}

// TestDriveStagesNothingOnUnwritablePath yields a good file first and a
// file the output cannot take after it: the run stages neither.
func TestDriveStagesNothingOnUnwritablePath(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, root string)
		bad   []base.Output
		err   string
	}{
		{name: "root", bad: []base.Output{whole(".", "x")}, err: `invalid path "."`},
		{
			name:  "existing directory",
			setup: func(t *testing.T, root string) { mkdir(t, root, "sub") },
			bad:   []base.Output{whole("sub", "x")},
			err:   `"sub": is a directory`,
		},
		{
			name: "file and a path below it",
			bad:  []base.Output{whole("b.txt", "b"), whole("b.txt/c.txt", "c")},
			err:  `"b.txt/c.txt": "b.txt" is yielded as a file too`,
		},
		{
			name:  "below an existing file",
			setup: func(t *testing.T, root string) { writeFile(t, root, "f.txt", "f") },
			bad:   []base.Output{whole("f.txt/c.txt", "c")},
			err:   `"f.txt/c.txt": "f.txt" is not a directory`,
		},
		{
			name: "through a symbolic link leaving the root",
			setup: func(t *testing.T, root string) {
				assert.NilError(t, os.Symlink(t.TempDir(), filepath.Join(root, "link")))
			},
			bad: []base.Output{whole("link/x.txt", "x")},
			err: `"link/x.txt": "link" is a symbolic link`,
		},
		{
			name: "through a symbolic link inside the root",
			setup: func(t *testing.T, root string) {
				mkdir(t, root, "sub")
				assert.NilError(t, os.Symlink("sub", filepath.Join(root, "link")))
			},
			bad: []base.Output{whole("link/x.txt", "x")},
			err: `"link/x.txt": "link" is a symbolic link`,
		},
		{
			name: "onto a symbolic link",
			setup: func(t *testing.T, root string) {
				writeFile(t, root, "target.txt", "t")
				assert.NilError(t, os.Symlink("target.txt", filepath.Join(root, "link.txt")))
			},
			bad: []base.Output{whole("link.txt", "x")},
			err: `"link.txt": is a symbolic link`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if c.setup != nil {
				c.setup(t, root)
			}

			env, f, r := rootEnv(t, root)

			outs := append([]base.Output{whole("a.txt", "a")}, c.bad...)

			err := base.Drive(t.Context(), yielding(outs), env, struct{}{}, base.Scope{}, f)
			assert.ErrorContains(t, err, "acme:drive: ")
			assert.ErrorContains(t, err, c.err)
			assert.DeepEqual(t, overlaid(t, f), []string(nil))
			assert.Equal(t, len(r.Messages), 0)
		})
	}
}

func mkdir(t *testing.T, root, name string) {
	t.Helper()
	assert.NilError(t, os.MkdirAll(filepath.Join(root, name), 0o755))
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	assert.NilError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
}

type formatterFunc func(ctx context.Context, path string, src []byte) ([]byte, error)

func (fn formatterFunc) Format(ctx context.Context, path string, src []byte) ([]byte, error) {
	return fn(ctx, path, src)
}

// TestDriveFormatsOnce runs a formatter that fails when it sees a file a
// second time: the run formats each file once and stages what it gave.
func TestDriveFormatsOnce(t *testing.T) {
	seen := map[string]int{}
	fm := formatterFunc(func(_ context.Context, path string, src []byte) ([]byte, error) {
		seen[filepath.Base(path)]++
		if seen[filepath.Base(path)] > 1 {
			return nil, errors.New("formatted twice")
		}

		return append(src, "// formatted\n"...), nil
	})

	root := t.TempDir()
	writeFile(t, root, "b.go", "package x\n")
	env, f, _ := rootEnv(t, root, overlayfs.WithFormatter(fm))

	err := base.Drive(t.Context(), yielding([]base.Output{
		whole("a.go", "package x\n"),
		whole("b.go", "package x\n\nvar B = 1\n"),
	}), env, struct{}{}, base.Scope{}, f)
	assert.NilError(t, err)
	assert.DeepEqual(t, seen, map[string]int{"a.go": 1, "b.go": 1})

	src, err := fs.ReadFile(f.FS(), "b.go")
	assert.NilError(t, err)
	assert.Equal(t, string(src), "package x\n\nvar B = 1\n// formatted\n")
}

// TestDriveFormatsWholeGo stages a Whole named *.go formatted: the output
// formats every Go file.
func TestDriveFormatsWholeGo(t *testing.T) {
	env, f, _ := driveEnv(t)

	err := base.Drive(t.Context(), yielding([]base.Output{
		whole("w.go", "package x\n\nimport \"strings\"\n\nfunc  F( ) {}\n"),
		whole("w.txt", "func  F( ) {}"),
	}), env, struct{}{}, base.Scope{}, f)
	assert.NilError(t, err)

	src, err := fs.ReadFile(f.FS(), "w.go")
	assert.NilError(t, err)
	assert.Equal(t, string(src), "package x\n\nfunc F() {}\n")

	src, err = fs.ReadFile(f.FS(), "w.txt")
	assert.NilError(t, err)
	assert.Equal(t, string(src), "func  F( ) {}")
}

func TestPrepareRejectsOutputs(t *testing.T) {
	cases := []struct {
		name string
		out  base.Output
		err  string
	}{
		{
			name: "Composed not named *.go",
			out:  base.Composed{Path: "x.txt", Package: "x"},
			err:  "acme:drive: x.txt: a Composed is a Go file, named *.go",
		},
		{
			name: "empty Decl",
			out: base.Composed{
				Path:    "x.go",
				Package: "x",
				Parts:   []base.Part{base.Decl("var A = 1"), base.Decl(" \n\t")},
			},
			err: "acme:drive: x.go: part 1: empty Decl",
		},
		{name: "nil *Whole", out: (*base.Whole)(nil), err: "acme:drive: nil *Whole"},
		{name: "nil *Composed", out: (*base.Composed)(nil), err: "acme:drive: nil *Composed"},
		{name: "nil", out: nil, err: "acme:drive: unknown output <nil>"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, _, _ := driveEnv(t)

			_, err := gentest.Assemble(t, yielding([]base.Output{c.out}), env.Registry, struct{}{})
			assert.Error(t, err, c.err)
		})
	}
}

func TestDrivePointerOutputs(t *testing.T) {
	env, f, _ := driveEnv(t)

	err := base.Drive(t.Context(), yielding([]base.Output{
		&base.Whole{Path: "a.txt", Content: []byte("a")},
		&base.Composed{Path: "b.go", Package: "p", Parts: []base.Part{base.Decl("var B = 1")}},
	}), env, struct{}{}, base.Scope{}, f)
	assert.NilError(t, err)
	assert.DeepEqual(t, overlaid(t, f), []string{"a.txt", "b.go"})
}

func TestDriveCancelled(t *testing.T) {
	env, f, _ := driveEnv(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := base.Drive(ctx, yielding([]base.Output{
		base.Composed{Path: "a.go", Package: "x", Parts: []base.Part{base.Decl("func A() {}")}},
	}), env, struct{}{}, base.Scope{}, f)
	assert.ErrorIs(t, err, context.Canceled)
	assert.DeepEqual(t, overlaid(t, f), []string(nil))
}

func TestDriveRecoversPanic(t *testing.T) {
	env, f, _ := driveEnv(t)

	g := base.Generator[struct{}]{
		Id: driveId,
		Generate: func(context.Context, base.Env, struct{}) iter.Seq2[base.Output, error] {
			return func(yield func(base.Output, error) bool) {
				if !yield(whole("a.txt", "a"), nil) {
					return
				}

				panic("boom")
			}
		},
	}

	err := base.Drive(t.Context(), g, env, struct{}{}, base.Scope{}, f)
	assert.ErrorContains(t, err, "acme:drive: panic: boom")
	assert.DeepEqual(t, overlaid(t, f), []string(nil))
}

func TestDriveNilGenerate(t *testing.T) {
	env, f, _ := driveEnv(t)

	err := base.Drive(
		t.Context(), base.Generator[struct{}]{Id: driveId}, env, struct{}{}, base.Scope{}, f,
	)
	assert.Error(t, err, "acme:drive: Generate is nil")
}

// TestDriveReplacesOwnFilesOnly yields a Composed over existing files: one
// written by hand, one of another generator, and two of the generator.
func TestDriveReplacesOwnFilesOnly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "user.go", "package x\n\n// Mine is mine.\nfunc Mine() {}\n")
	writeFile(t, root, "other.go", "package x\n\n//kama:generated acme:other\nfunc O() {}\n")
	writeFile(t, root, "own.go", "package x\n\n//kama:generated acme:drive\nfunc Own() {}\n")
	writeFile(
		t, root, "text.go", "// Code generated by acme:drive. DO NOT EDIT.\n\npackage x\n",
	)

	env, f, _ := rootEnv(t, root)
	assert.NilError(t, env.Registry.Register("acme:other"))

	composed := func(name string) base.Composed {
		return base.Composed{Path: name, Package: "x", Parts: []base.Part{base.Text("// Text.")}}
	}

	err := base.Drive(t.Context(), yielding([]base.Output{
		composed("user.go"), composed("other.go"), composed("own.go"), composed("text.go"),
	}), env, struct{}{}, base.Scope{}, f)
	assert.ErrorContains(t, err, `acme:drive: "user.go" exists and is not generated by acme:drive`)
	assert.ErrorContains(t, err, `acme:drive: "other.go" exists and is not generated by acme:drive`)
	assert.Assert(t, !strings.Contains(err.Error(), `"own.go"`), err)
	assert.Assert(t, !strings.Contains(err.Error(), `"text.go"`), err)
	assert.DeepEqual(t, overlaid(t, f), []string(nil))

	// A file holding Text only carries no mark; the header tells it.
	assert.NilError(t, base.Drive(t.Context(), yielding([]base.Output{
		composed("own.go"), composed("text.go"), composed("new.go"),
	}), env, struct{}{}, base.Scope{}, f))
	assert.NilError(t, base.Drive(t.Context(), yielding([]base.Output{
		composed("new.go"),
	}), env, struct{}{}, base.Scope{}, f))
}

// TestStampRules pins what a Decl that is not one declaration with its
// doc comment attached gives: Stamp does not parse, so the mark goes on
// whatever comes first.
func TestStampRules(t *testing.T) {
	cases := []struct {
		decl   string
		marked []string
	}{
		{"// A is a.\nfunc A() {}", []string{"A"}},
		{"/* A is a. */\nfunc A() {}", []string{"A"}},
		{"\r\n// A is a.\r\nfunc A() {}\r\n", []string{"A"}},
		// The mark goes on the detached comment, and B is left unmarked.
		{"// Detached.\n\nfunc B() {}", nil},
		// The mark goes on D only.
		{"func D() {}\nfunc E() {}", []string{"D"}},
	}

	for _, c := range cases {
		t.Run(c.decl, func(t *testing.T) {
			env, f, _ := driveEnv(t)

			assert.NilError(t, base.Drive(t.Context(), yielding([]base.Output{base.Composed{
				Path:    "x.go",
				Package: "x",
				Parts:   []base.Part{base.Decl(c.decl)},
			}}), env, struct{}{}, base.Scope{}, f))

			src, err := fs.ReadFile(f.FS(), "x.go")
			assert.NilError(t, err)

			file, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.ParseComments)
			assert.NilError(t, err)

			var marked []string

			for _, d := range file.Decls {
				fd := d.(*ast.FuncDecl)
				if _, ok := env.Registry.Generated(fd.Doc); ok {
					marked = append(marked, fd.Name.Name)
				}
			}

			assert.DeepEqual(t, marked, c.marked)
		})
	}
}

func TestPrepareImports(t *testing.T) {
	imports := func(t *testing.T, imps ...importdecl.Import) *importdecl.Map {
		t.Helper()

		parser := importdecl.NewParser(types.NewPackage("example.com/p", "p"))
		assert.NilError(t, parser.Require(imps...))

		m, err := parser.ParseAst(nil)
		assert.NilError(t, err)

		return m
	}
	imp := func(name, path string) importdecl.Import {
		return importdecl.Import{
			Name:    name,
			Package: importdecl.Package{Path: path, Name: filepath.Base(path)},
		}
	}

	cases := []struct {
		name    string
		imports []importdecl.Import
		parts   []base.Part
		want    string
	}{
		{
			name:    "single",
			imports: []importdecl.Import{imp("", "fmt")},
			want:    "import \"fmt\"\n\n",
		},
		{
			name:    "alias",
			imports: []importdecl.Import{imp("f", "fmt"), imp("q2", "example.com/q")},
			want:    "import (\n\tf \"fmt\"\n\n\tq2 \"example.com/q\"\n)\n\n",
		},
		{
			name:    "non-standard only",
			imports: []importdecl.Import{imp("", "example.com/q"), imp("", "example.com/r")},
			want:    "import (\n\t\"example.com/q\"\n\t\"example.com/r\"\n)\n\n",
		},
		{
			name:    "blank import in a leading Text",
			imports: []importdecl.Import{imp("", "fmt")},
			parts:   []base.Part{base.Text("import _ \"embed\"\n")},
			want:    "import \"fmt\"\n\nimport _ \"embed\"\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, _, _ := driveEnv(t)

			files, err := gentest.Assemble(t, yielding([]base.Output{base.Composed{
				Path:    "p.go",
				Package: "p",
				Imports: imports(t, c.imports...),
				Parts:   c.parts,
			}}), env.Registry, struct{}{})
			assert.NilError(t, err)

			src := string(files[0].Content)
			_, after, ok := strings.Cut(src, "package p\n\n")
			assert.Assert(t, ok, src)
			assert.Equal(t, after, c.want)
		})
	}
}

// TestDriveBlankImportSurvivesFormat stages a blank import declared in a
// leading Text: the formatter keeps it.
func TestDriveBlankImportSurvivesFormat(t *testing.T) {
	env, f, _ := driveEnv(t)

	assert.NilError(t, base.Drive(t.Context(), yielding([]base.Output{base.Composed{
		Path:    "p.go",
		Package: "p",
		Parts: []base.Part{
			base.Text("import _ \"embed\"\n"),
			base.Decl("var V = 1"),
		},
	}}), env, struct{}{}, base.Scope{}, f))

	src, err := fs.ReadFile(f.FS(), "p.go")
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(src), "import _ \"embed\"\n"), string(src))
}

// TestProcFilesMatchesRun checks that Files returns the bytes Run stages.
func TestProcFilesMatchesRun(t *testing.T) {
	ctx := t.Context()
	f, err := overlayfs.New(t.TempDir())
	assert.NilError(t, err)

	defer f.Close()

	var loaded *fileloader.PathInput

	inst, err := fakeProc(&loaded, nil).Instantiate(ctx, false)
	assert.NilError(t, err)

	i, ok := inst.(interface {
		fakeInstance
		Files(ctx context.Context, r workflow.Reporter) ([]base.File, error)
	})
	assert.Assert(t, ok, "%T", inst)
	assert.NilError(t, i.Param(fakeParamPartial{}))
	i.In(&procgopackages.Packages{})
	i.Out(f)

	files, err := i.Files(ctx, gentest.NopReporter{})
	assert.NilError(t, err)
	assert.DeepEqual(t, overlaid(t, f), []string(nil))
	assert.Equal(t, len(files), 1)

	assert.NilError(t, i.Run(ctx, gentest.NopReporter{}))

	staged, err := fs.ReadFile(f.FS(), "gen.go")
	assert.NilError(t, err)
	assert.Equal(t, files[0].Path, "gen.go")
	assert.Equal(t, string(files[0].Content), string(staged))
}

// TestProcRequiredFields instantiates procs missing a required field.
func TestProcRequiredFields(t *testing.T) {
	var loaded *fileloader.PathInput

	noOptions := fakeProc(&loaded, nil)
	noOptions.Options = nil
	noLoader := fakeProc(&loaded, nil)
	noLoader.Loader = nil

	for _, c := range []struct {
		proc workflow.Proc
		err  string
	}{
		{noOptions, "base: proc of acme:fake: Options is nil"},
		{noLoader, "base: proc of acme:fake: Loader is nil"},
		{
			base.Proc[noInParamPartial, noInParam, base.None, string]{
				Generator: base.Generator[string]{Id: driveId},
			},
			"base: proc of acme:drive: Generator.Generate is nil",
		},
	} {
		_, err := c.proc.Instantiate(t.Context(), true)
		assert.Error(t, err, c.err)
	}
}

// nilLoader returns neither options nor an error.
type nilLoader struct{}

func (nilLoader) LoadOptionNamed(
	fs.FS, string, []*packages.Package, *fileloader.PathInput,
) (*genopt.Loaded[fakeOptions, fakeOptions, fakeOptions], error) {
	return nil, nil
}

func TestSourceProcNilLoaded(t *testing.T) {
	f, err := overlayfs.New(t.TempDir())
	assert.NilError(t, err)

	defer f.Close()

	var loaded *fileloader.PathInput

	p := fakeProc(&loaded, nil)
	p.Loader = nilLoader{}

	i := instantiate(t, p)
	assert.NilError(t, i.Param(fakeParamPartial{}))
	i.In(&procgopackages.Packages{})
	i.Out(f)
	assert.Error(
		t,
		i.Run(t.Context(), gentest.NopReporter{}),
		"base: acme:fake: the option loader returned no options and no error",
	)
}

func TestPrepareUnregisteredId(t *testing.T) {
	env, f, _ := driveEnv(t)

	g := yielding(nil)
	g.Id = base.Id{Namespace: "acme", Name: "unknown"}

	err := base.Drive(t.Context(), g, env, struct{}{}, base.Scope{}, f)
	assert.ErrorContains(t, err, "acme:unknown: the generator is not registered in env.Registry")

	env.Registry = nil
	err = base.Drive(t.Context(), g, env, struct{}{}, base.Scope{}, f)
	assert.ErrorContains(t, err, "acme:unknown: the generator is not registered in env.Registry")

	g.Id = base.Id{Namespace: "acme", Name: "linux"}
	err = base.Drive(t.Context(), g, env, struct{}{}, base.Scope{}, f)
	assert.ErrorIs(t, err, base.ErrInvalidId)
}

// TestDriveExplainsUnknownMark yields a Composed over files carrying a
// generated mark of no registered generator.
func TestDriveExplainsUnknownMark(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "legacy.go", "package x\n\n//kama:generated kama-drive\nfunc L() {}\n")
	writeFile(t, root, "gone.go", "package x\n\n//kama:generated acme:gone\nfunc G() {}\n")

	env, f, _ := rootEnv(t, root)
	assert.NilError(t, env.Registry.Register("kama:drive"))

	composed := func(name string) base.Composed {
		return base.Composed{Path: name, Package: "x", Parts: []base.Part{base.Decl("var V = 1")}}
	}

	err := base.Drive(t.Context(), yielding([]base.Output{
		composed("legacy.go"), composed("gone.go"),
	}), env, struct{}{}, base.Scope{}, f)
	assert.ErrorContains(
		t,
		err,
		`"legacy.go" exists and is not generated by acme:drive; a Composed replaces only a file`+
			` of its generator; it carries //kama:generated kama-drive, generated before the mark`+
			` was renamed to //kama:generated kama:drive; delete the file holding it and run again`,
	)
	assert.ErrorContains(
		t,
		err,
		`"gone.go" exists and is not generated by acme:drive; a Composed replaces only a file`+
			` of its generator; it carries //kama:generated acme:gone, which names no registered`+
			` generator`,
	)
}
