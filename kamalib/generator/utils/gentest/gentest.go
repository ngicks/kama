// Package gentest holds scaffolding for the tests of the generators.
//
// Only test files import this package.
package gentest

import (
	"cmp"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v4"
	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

// Now is the time the header of the files generated in tests records.
func Now() time.Time { return time.Date(2026, 11, 4, 10, 12, 33, 0, time.UTC) }

// NopReporter drops what a proc reports.
type NopReporter struct{}

func (NopReporter) Warn(error)      {}
func (NopReporter) Progress(string) {}

// RecordReporter records what a proc reports.
type RecordReporter struct {
	Warnings []error
	Messages []string
}

func (r *RecordReporter) Warn(w error)        { r.Warnings = append(r.Warnings, w) }
func (r *RecordReporter) Progress(msg string) { r.Messages = append(r.Messages, msg) }

// InlineDir is the directory under testdata Stage writes the files of t to.
func InlineDir(t *testing.T) string {
	return "testdata/inline/" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
}

// Stage writes files, keyed by slash-separated path relative to InlineDir,
// into an overlay over the working directory built with opts, and returns
// the overlay and the options loading their packages. The files live in the
// overlay only.
func Stage(
	t *testing.T,
	files map[string]string,
	opts ...overlayfs.Option,
) (*overlayfs.Fs, gopackages.Options) {
	t.Helper()

	f, err := overlayfs.New(".", opts...)
	assert.NilError(t, err)
	t.Cleanup(func() { f.Close() })

	dir := InlineDir(t)
	dirs := map[string]bool{}

	for name, src := range files {
		assert.NilError(t, f.Write(context.Background(), path.Join(dir, name), []byte(src)))
		dirs["./"+path.Dir(path.Join(dir, name))] = true
	}

	return f, gopackages.Options{Patterns: slices.Sorted(maps.Keys(dirs))}
}

// Load type-checks files, staged as Stage does, as packages of the module.
func Load(t *testing.T, files map[string]string, opts ...overlayfs.Option) []*packages.Package {
	t.Helper()

	_, pkgs := LoadFs(t, files, opts...)

	return pkgs
}

// LoadFs is Load returning the overlay the files are staged into too.
func LoadFs(
	t *testing.T,
	files map[string]string,
	opts ...overlayfs.Option,
) (*overlayfs.Fs, []*packages.Package) {
	t.Helper()

	f, loadOpts := Stage(t, files, opts...)
	pkgs, err := gopackages.Load(context.Background(), f, loadOpts)
	assert.NilError(t, err)

	return f, pkgs
}

// Run runs g on in as base.Drive does with out, a registry and the time
// now, and stages nothing. It returns the files formatted by out, with
// absolute paths under the root of out and sorted by path, the warnings g
// reported followed by those of the stale check of scope, and the errors.
func Run[I any](
	ctx context.Context,
	g base.Generator[I],
	reg *genmark.Registry,
	now time.Time,
	in I,
	scope base.Scope,
	out *overlayfs.Fs,
) ([]base.File, []error, error) {
	r := &RecordReporter{}
	env := base.Env{Reporter: r, Now: now, Registry: reg, Out: out.FS()}

	files, err := base.Assemble(ctx, g, env, in)
	if err != nil {
		return nil, r.Warnings, err
	}

	stale, err := base.StaleCheck{Registry: reg, Generator: g.Id}.InScope(out.FS(), scope, files)
	if err != nil {
		return nil, r.Warnings, err
	}

	formatted := make([]base.File, 0, len(files))

	for _, f := range files {
		content, err := out.Format(ctx, f.Path, f.Content)
		if err != nil {
			return nil, r.Warnings, err
		}

		formatted = append(formatted, base.File{
			Path:    filepath.Join(out.Root(), filepath.FromSlash(f.Path)),
			Content: content,
		})
	}

	slices.SortFunc(formatted, func(a, b base.File) int { return cmp.Compare(a.Path, b.Path) })

	return formatted, slices.Concat(r.Warnings, stale), nil
}

// Write writes files, generated under the root of f, into f.
func Write(t *testing.T, f *overlayfs.Fs, files []base.File) {
	t.Helper()

	for _, o := range files {
		rel, err := filepath.Rel(f.Root(), o.Path)
		assert.NilError(t, err)
		assert.NilError(t, f.Write(context.Background(), filepath.ToSlash(rel), o.Content))
	}
}

// Build type-checks the packages opts loads from f. go/types reports an
// unused import and an unused variable as errors, which fail the load as
// long as the overlay stages the files as written.
func Build(f *overlayfs.Fs, opts gopackages.Options) error {
	_, err := gopackages.Load(context.Background(), f, opts)
	return err
}

// ByBase returns the content of files keyed by base name.
func ByBase(files []base.File) map[string]string {
	m := map[string]string{}
	for _, f := range files {
		m[filepath.Base(f.Path)] = string(f.Content)
	}

	return m
}

// Funcs returns the source of each function declared in src keyed by name,
// methods keyed by receiver type and name as in T.M.
func Funcs(t *testing.T, src string) map[string]string {
	t.Helper()

	fset, decls := FuncDecls(t, src)

	out := map[string]string{}
	for name, fd := range decls {
		out[name] = src[fset.Position(fd.Pos()).Offset:fset.Position(fd.End()).Offset]
	}

	return out
}

// FuncDecls parses src and returns each function declared in it keyed as
// Funcs keys it.
func FuncDecls(t *testing.T, src string) (*token.FileSet, map[string]*ast.FuncDecl) {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	assert.NilError(t, err)

	out := map[string]*ast.FuncDecl{}

	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}

		name := fd.Name.Name
		if fd.Recv != nil {
			name = receiverName(fd.Recv.List[0].Type) + "." + name
		}

		out[name] = fd
	}

	return fset, out
}

// BuildSpecs decodes the spec.yaml of each of dirs, builds it against
// workflow.DefaultRegistry and checks that the build has no warnings. The
// test registers the procs the specs name.
func BuildSpecs(t *testing.T, dirs ...string) {
	for _, dir := range dirs {
		t.Run(dir, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(dir, "spec.yaml"))
			assert.NilError(t, err)

			var spec workflow.Spec
			assert.NilError(t, yaml.Unmarshal(b, &spec))

			// Build only dry-runs, so the fixture directory is not touched.
			g, err := spec.Build(context.Background(), workflow.DefaultRegistry)
			assert.NilError(t, err)
			assert.Assert(t, len(g.Warnings()) == 0, "%v", g.Warnings())
		})
	}
}

func receiverName(recv ast.Expr) string {
	for {
		switch x := recv.(type) {
		case *ast.StarExpr:
			recv = x.X
		case *ast.IndexExpr:
			recv = x.X
		case *ast.IndexListExpr:
			recv = x.X
		default:
			return recv.(*ast.Ident).Name
		}
	}
}
