package contextkey

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

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

func TestProcParamErrors(t *testing.T) {
	i := &instance{}

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

	i := &instance{}
	assert.NilError(t, i.Param(ProcParam{Dir: "gen", Package: "p", KeyTypeFunc: "k"}))
	i.In(&fileloader.Document{Path: "t.yaml", Data: data})
	i.Out(f)
	assert.NilError(t, i.Run(ctx))

	ov, err := f.Overlay()
	assert.NilError(t, err)
	assert.DeepEqual(t, slices.Sorted(maps.Keys(ov)), []string{
		filepath.Join(f.Root(), "gen", "a.contextkey.go"),
		filepath.Join(f.Root(), "gen", "b.contextkey.go"),
	})
}

func TestProcRunNoEntries(t *testing.T) {
	i := &instance{}
	assert.NilError(t, i.Param(ProcParam{Package: "p", KeyTypeFunc: "k"}))
	i.In(&fileloader.Document{Path: "t.yaml", Data: map[string]any{}})
	assert.ErrorIs(t, i.Run(context.Background()), ErrNoEntries)
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
