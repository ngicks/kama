package builtin

import (
	"context"
	"testing"

	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/workflow"
)

func registry(t *testing.T) *workflow.Registry {
	t.Helper()

	reg := workflow.NewRegistry()
	assert.NilError(t, Register(reg))
	return reg
}

func decode(t *testing.T, src string) workflow.Spec {
	t.Helper()

	var spec workflow.Spec
	assert.NilError(t, yaml.Unmarshal([]byte(src), &spec))
	return spec
}

func TestBuildBuiltinGraph(t *testing.T) {
	spec := decode(t, `
version: 1
steps:
  root:
    proc: fs
    param:
      root: ./does/not/exist/yet
  packages:
    proc: gopackages
    in: root
    param:
      patterns: ["./..."]
      build_flags: ["-tags=gen"]
  meta:
    proc: metadata
    in: root
    param:
      base: codegen/meta
      formats: [yaml, json]
  final:
    proc: copy_down
    in: root
    after: [packages, meta]
`)

	// Formation is a dry run: the missing root is not touched.
	g, err := spec.Build(context.Background(), registry(t))
	assert.NilError(t, err)

	var names []string
	for _, n := range g.Nodes() {
		names = append(names, n.Name)
	}
	assert.DeepEqual(t, names, []string{"root", "meta", "packages", "final"})
}

func TestBuildBuiltinGraphErrors(t *testing.T) {
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
  root: {proc: fs}
  packages: {proc: gopackages, in: root}
  final: {proc: copy_down, in: packages}
`,
			workflow.ErrTypeMismatch,
		},
		{
			"param list where a map is expected",
			`
version: 1
steps:
  root: {proc: fs, param: ["./"]}
`,
			workflow.ErrBuild,
		},
		{
			"gopackages without in",
			`
version: 1
steps:
  packages: {proc: gopackages}
`,
			workflow.ErrArity,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := decode(t, c.src).Build(context.Background(), registry(t))
			assert.ErrorIs(t, err, c.want)
		})
	}
}
