package proc_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/ngicks/opiter"
	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	_ "github.com/ngicks/kama/kamalib/proc/fileloader"
	_ "github.com/ngicks/kama/kamalib/proc/gopackages"
	procoverlayfs "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

func decode(t *testing.T, src string) workflow.Spec {
	t.Helper()

	var spec workflow.Spec
	assert.NilError(t, yaml.Unmarshal([]byte(src), &spec))

	return spec
}

func TestBuildGraph(t *testing.T) {
	spec := decode(t, `
version: 1
steps:
  root:
    proc: overlayfs.open
    param:
      root: ./does/not/exist/yet
  packages:
    proc: packages.load
    in: root
    param:
      patterns: ["./..."]
      build_flags: ["-tags=gen"]
  meta:
    proc: file.load
    in: root
    param:
      base: codegen/meta
      formats: [yaml, json]
  final:
    proc: overlayfs.copy_down
    in: root
    after: [packages, meta]
`)

	// Formation is a dry run: the missing root is not touched.
	g, err := spec.Build(context.Background(), workflow.DefaultRegistry)
	assert.NilError(t, err)

	names := opiter.WrapSeq(slices.Values(g.Nodes())).
		Map(func(n workflow.Node) string { return n.Name }).
		CollectSlice()

	assert.DeepEqual(t, names, []string{"root", "meta", "packages", "final"})
}

func TestBuildGraphErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want error
	}{
		{
			"copy_down fed packages",
			`
version: 1
steps:
  root: {proc: overlayfs.open}
  packages: {proc: packages.load, in: root}
  final: {proc: overlayfs.copy_down, in: packages}
`,
			workflow.ErrTypeMismatch,
		},
		{
			"param list where a map is expected",
			`
version: 1
steps:
  root: {proc: overlayfs.open, param: ["./"]}
`,
			workflow.ErrParam,
		},
		{
			"copy_down named as a value",
			`
version: 1
steps:
  root: {proc: overlayfs.open}
  final: {proc: overlayfs.copy_down, in: root}
  again: {proc: overlayfs.copy_down, in: final}
`,
			workflow.ErrInvalidStep,
		},
		{
			"gopackages without in",
			`
version: 1
steps:
  packages: {proc: packages.load}
`,
			workflow.ErrArity,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := decode(t, c.src).Build(context.Background(), workflow.DefaultRegistry)
			assert.ErrorIs(t, err, c.want)
		})
	}
}

// emitFile is a file emitProc stages.
type emitFile struct {
	name string
	src  string
}

// emitProc stands in for a generator. Its step stages files, in order, into
// the filesystem named in its out list.
type emitProc struct {
	files []emitFile
}

func (emitProc) ParamType() reflect.Type   { return nil }
func (emitProc) InTypes() []reflect.Type   { return nil }
func (emitProc) AsInputType() reflect.Type { return nil }

func (emitProc) OutTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (p emitProc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &emitInstance{files: p.files}, nil
}

func (emitProc) CleanUp(context.Context) error { return nil }

type emitInstance struct {
	files []emitFile
	fs    *overlayfs.Fs
}

func (i *emitInstance) Param() error { return nil }

func (i *emitInstance) In() {}

func (i *emitInstance) Out(fs *overlayfs.Fs) { i.fs = fs }

func (i *emitInstance) Run(ctx context.Context, _ workflow.Reporter) error {
	for _, f := range i.files {
		if err := i.fs.Write(ctx, f.name, []byte(f.src)); err != nil {
			return err
		}
	}

	return nil
}

// readTree returns the content of every file under root, keyed by its
// slash-separated path relative to root.
func readTree(t *testing.T, root string) map[string]string {
	t.Helper()

	out := map[string]string{}

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}

		out[filepath.ToSlash(rel)] = string(b)

		return nil
	})
	assert.NilError(t, err)

	return out
}

func TestWrongOutputIsNotCopiedDown(t *testing.T) {
	const valid = "package x\n\nfunc A() int { return 1 }\n"

	cases := []struct {
		name  string
		files []emitFile
		// failedStep is the step Run fails at. Empty means Run succeeds.
		failedStep string
		// errorPackages are the paths of the packages whose errors the
		// gopackages.PackageErrors of the error holds.
		errorPackages []string
		// fastIgnore sets fast_ignore_compile_errors of copy_down.
		fastIgnore bool
		// copied are the files copy_down adds to the project directory.
		copied map[string]string
	}{
		{
			name: "source that does not parse, after a valid file",
			files: []emitFile{
				{name: "a.gen.go", src: valid},
				{name: "b.gen.go", src: "package x\nfunc {"},
			},
			failedStep: "emit",
		},
		{
			name: "source that parses and does not compile",
			files: []emitFile{
				{name: "a.gen.go", src: "package x\n\nfunc A() int { return B() }\n"},
			},
			failedStep:    "final",
			errorPackages: []string{"example.invalid/x"},
		},
		{
			name: "compile error in a directory that is only staged",
			files: []emitFile{
				{name: "a.gen.go", src: valid},
				{name: "sub/b.gen.go", src: "package sub\n\nvar C int = \"c\"\n"},
			},
			failedStep:    "final",
			errorPackages: []string{"example.invalid/x/sub"},
		},
		{
			name: "source that does not compile, with the check disabled",
			files: []emitFile{
				{name: "a.gen.go", src: "package x\n\nfunc A() int { return B() }\n"},
			},
			fastIgnore: true,
			copied:     map[string]string{"a.gen.go": "package x\n\nfunc A() int { return B() }\n"},
		},
		{
			name:   "valid source",
			files:  []emitFile{{name: "sub/a.gen.go", src: valid}},
			copied: map[string]string{"sub/a.gen.go": valid},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			assert.NilError(t, os.WriteFile(
				filepath.Join(
					root,
					"go.mod",
				),
				[]byte("module example.invalid/x\n\ngo 1.27\n"),
				0o644,
			))
			assert.NilError(t, os.WriteFile(
				filepath.Join(root, "x.go"), []byte("package x\n"), 0o644,
			))

			want := readTree(t, root)
			maps.Copy(want, c.copied)

			reg := workflow.NewRegistry()
			assert.NilError(t, reg.Register("overlayfs.open", procoverlayfs.NewOpenProc))
			assert.NilError(t, reg.Register("overlayfs.copy_down", procoverlayfs.NewCopyDownProc))
			assert.NilError(t, reg.Register(
				"test.emit", func() workflow.Proc { return emitProc{files: c.files} },
			))

			spec := decode(t, fmt.Sprintf(`
version: 1
steps:
  root: {proc: overlayfs.open, param: {root: %q}}
  emit: {proc: test.emit, out: root}
  final: {proc: overlayfs.copy_down, in: root, after: emit, param: {fast_ignore_compile_errors: %t}}
`, root, c.fastIgnore))

			g, err := spec.Build(ctx, reg)
			assert.NilError(t, err)

			err = g.Run(ctx, workflow.ProgressControl{})

			if c.failedStep == "" {
				assert.NilError(t, err)
			} else {
				assert.ErrorIs(t, err, workflow.ErrRun)
				assert.ErrorContains(t, err, fmt.Sprintf("step %q", c.failedStep))
			}

			pkgErrs, _ := errors.AsType[gopackages.PackageErrors](err)
			assert.DeepEqual(t, slices.Sorted(maps.Keys(pkgErrs)), c.errorPackages)

			assert.DeepEqual(t, readTree(t, root), want)
		})
	}
}
