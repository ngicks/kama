package base_test

import (
	"context"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	procoverlayfs "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

// plainParam is a param with no partial variant.
type plainParam struct {
	Name    string            `json:"name"`
	Tags    []string          `json:"tags"`
	Labels  map[string]string `json:"labels"`
	Unknown map[string]any    `json:",embed"`
}

func (plainParam) Default() plainParam {
	return plainParam{
		Name:   "default",
		Tags:   []string{"a", "b"},
		Labels: map[string]string{"k": "v"},
	}
}

func (p *plainParam) UnknownKeys() [][]string { return unknownkey.Paths(p.Unknown) }

// TestParamOf runs a proc taking a ParamOf through a workflow: a step
// leaving a key out keeps its default.
func TestParamOf(t *testing.T) {
	ctx := context.Background()

	proc := base.Proc[base.ParamOf[plainParam], plainParam, base.None, plainParam]{
		Generator: base.Generator[plainParam]{
			Id: driveId,
			Generate: func(_ context.Context, _ base.Env, p plainParam) iter.Seq2[base.Output, error] {
				return func(yield func(base.Output, error) bool) {
					yield(
						whole("param.txt", fmt.Sprintf("%s %v %v", p.Name, p.Tags, p.Labels)),
						nil,
					)
				}
			},
		},
		Input: func(
			_ context.Context,
			p plainParam,
			_ base.None,
			_ *overlayfs.Fs,
		) (plainParam, base.Scope, error) {
			return p, base.Scope{}, nil
		},
	}

	reg := workflow.NewRegistry()
	assert.NilError(t, reg.Register("overlayfs.open", procoverlayfs.NewOpenProc))
	assert.NilError(t, reg.Register("overlayfs.copy_down", procoverlayfs.NewCopyDownProc))
	assert.NilError(t, reg.Register(driveId.ProcKind(), func() workflow.Proc { return proc }))

	for _, c := range []struct {
		name     string
		param    map[string]any
		want     string
		warnings int
	}{
		{"no param", nil, "default [a b] map[k:v]", 0},
		{"name only", map[string]any{"name": "x"}, "x [a b] map[k:v]", 0},
		{
			name:  "slice replaced, map merged",
			param: map[string]any{"tags": []string{"c"}, "labels": map[string]string{"l": "w"}},
			want:  "default [c] map[k:v l:w]",
		},
		{"unknown key", map[string]any{"typo": 1}, "default [a b] map[k:v]", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			// copy_down checks that the staged files compile, which takes a
			// module.
			writeFile(t, root, "go.mod", "module example.com/p\n")

			g, err := workflow.Spec{
				Version: 1,
				Steps: map[string]workflow.Step{
					"root": {Proc: "overlayfs.open", Param: map[string]any{"root": root}},
					"gen":  {Proc: driveId.ProcKind(), Out: workflow.Names{"root"}, Param: c.param},
					"done": {
						Proc:  "overlayfs.copy_down",
						In:    workflow.Names{"root"},
						After: workflow.Names{"gen"},
					},
				},
			}.Build(ctx, reg)
			assert.NilError(t, err)
			assert.Equal(t, len(g.Warnings()), c.warnings, "%v", g.Warnings())

			assert.NilError(t, g.Run(ctx, workflow.ProgressControl{}))

			got, err := os.ReadFile(filepath.Join(root, "param.txt"))
			assert.NilError(t, err)
			assert.Equal(t, string(got), c.want)
		})
	}
}
