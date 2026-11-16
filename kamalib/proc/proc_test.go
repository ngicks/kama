package proc_test

import (
	"context"
	"slices"
	"testing"

	"github.com/ngicks/opiter"
	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	_ "github.com/ngicks/kama/kamalib/proc/fileloader"
	_ "github.com/ngicks/kama/kamalib/proc/gopackages"
	_ "github.com/ngicks/kama/kamalib/proc/overlayfs"
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

	names := slices.Collect(opiter.WrapSeq(slices.Values(g.Nodes())).Map(nodeName).Iter())
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

func nodeName(n workflow.Node) string { return n.Name }
