package overlayfs

import (
	"context"
	"errors"
	"fmt"
	"go/types"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procgopackages "github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

// TestGateFixture runs testdata/gate/spec.yaml, whose generate step emits
// code that parses and does not compile. The copy_down step must fail,
// leaving the project directory as it was.
func TestGateFixture(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	assert.NilError(t, os.CopyFS(root, os.DirFS("testdata/gate")))
	// The fixture has no go.mod of its own: a go.mod under testdata would be
	// a module the pre-commit hook lints on its own.
	assert.NilError(t, os.WriteFile(
		filepath.Join(root, "go.mod"), []byte("module example.com/gate\n\ngo 1.27\n"), 0o644,
	))

	b, err := os.ReadFile(filepath.Join(root, "spec.yaml"))
	assert.NilError(t, err)

	var spec workflow.Spec
	assert.NilError(t, yaml.Unmarshal(b, &spec))

	reg := workflow.NewRegistry()
	assert.NilError(t, reg.Register("overlayfs.open", NewOpenProc))
	assert.NilError(t, reg.Register("overlayfs.copy_down", NewCopyDownProc))
	assert.NilError(t, reg.Register("packages.load", procgopackages.NewProc))
	assert.NilError(
		t,
		reg.Register("fixture.generate", func() workflow.Proc { return brokenGenerator{} }),
	)

	before := readTree(t, root)

	// The spec opens the working directory, as a spec run by go generate does.
	t.Chdir(root)

	g, err := spec.Build(ctx, reg)
	assert.NilError(t, err)

	err = g.Run(ctx, workflow.ProgressControl{})
	assert.ErrorIs(t, err, workflow.ErrRun)
	assert.ErrorContains(t, err, `step "final"`)

	var pkgErrs gopackages.PackageErrors
	assert.Assert(t, errors.As(err, &pkgErrs), "err: %v", err)
	assert.DeepEqual(t, slices.Collect(maps.Keys(pkgErrs)), []string{"example.com/gate/model"})
	assert.ErrorContains(t, err, "undefined: typeNameOf")

	assert.DeepEqual(t, readTree(t, root), before)
}

// readTree returns the content of every file under root, keyed by its
// slash-separated path relative to root.
func readTree(t *testing.T, root string) map[string]string {
	t.Helper()

	out := map[string]string{}
	fsys := os.DirFS(root)

	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}

		out[p] = string(b)

		return nil
	})
	assert.NilError(t, err)

	return out
}

// brokenGenerator is a small incorrect generator. For every struct type of
// the loaded packages it emits a TypeName method calling typeNameOf, a
// helper it never emits.
type brokenGenerator struct{}

func (brokenGenerator) ParamType() reflect.Type   { return nil }
func (brokenGenerator) OutTypes() []reflect.Type  { return []reflect.Type{fsType} }
func (brokenGenerator) AsInputType() reflect.Type { return nil }

func (brokenGenerator) InTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*procgopackages.Packages]()}
}

func (brokenGenerator) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &brokenInstance{}, nil
}

func (brokenGenerator) CleanUp(context.Context) error { return nil }

type brokenInstance struct {
	pkgs *procgopackages.Packages
	fs   *overlayfs.Fs
}

func (i *brokenInstance) Param() error { return nil }

func (i *brokenInstance) In(pkgs *procgopackages.Packages) { i.pkgs = pkgs }

func (i *brokenInstance) Out(fs *overlayfs.Fs) { i.fs = fs }

func (i *brokenInstance) Run(ctx context.Context, _ workflow.Reporter) error {
	for _, pkg := range i.pkgs.Pkgs {
		var src strings.Builder

		fmt.Fprintf(&src, "package %s\n", pkg.Name)

		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			if _, ok := scope.Lookup(name).Type().Underlying().(*types.Struct); ok {
				fmt.Fprintf(
					&src, "\nfunc (v %s) TypeName() string { return typeNameOf(v) }\n", name,
				)
			}
		}

		dir, err := filepath.Rel(i.fs.Root(), pkg.Dir)
		if err != nil {
			return err
		}

		name := path.Join(filepath.ToSlash(dir), pkg.Name+".gen.go")
		if err := i.fs.Write(ctx, name, []byte(src.String())); err != nil {
			return err
		}
	}

	return nil
}
