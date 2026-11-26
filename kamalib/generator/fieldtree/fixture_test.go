package fieldtree

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/textmod"
	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	pkggopackages "github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	_ "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

const fixtureDir = "internal/fixture"

func TestFixtureSpecBuild(t *testing.T) {
	gentest.BuildSpecs(t, fixtureDir)
}

type textmodInstance interface {
	Param(p base.ParamOf[textmod.ProcParam]) error
	In(pkgs *gopackages.Packages)
	Out(f *overlayfs.Fs)
	Optional(in map[string]any)
	Files(ctx context.Context, r workflow.Reporter) ([]base.File, error)
}

// TestFixtureUpToDate runs the steps tree and replace of spec.yaml on the
// fixture. The help text holds the tree already, so the run changes no
// file.
func TestFixtureUpToDate(t *testing.T) {
	ctx := context.Background()

	b, err := os.ReadFile(filepath.Join(fixtureDir, "spec.yaml"))
	assert.NilError(t, err)

	var spec workflow.Spec
	assert.NilError(t, yaml.Unmarshal(b, &spec))

	raw, err := json.Marshal(spec.Steps["replace"].Param)
	assert.NilError(t, err)

	var param base.ParamOf[textmod.ProcParam]
	assert.NilError(t, json.Unmarshal(raw, &param))

	f, err := overlayfs.New(fixtureDir)
	assert.NilError(t, err)

	defer f.Close()

	pkgs, err := pkggopackages.Load(ctx, f, pkggopackages.Options{Patterns: []string{"./..."}})
	assert.NilError(t, err)

	r := &gentest.RecordReporter{}

	treeInst, err := NewProc().Instantiate(ctx, false)
	assert.NilError(t, err)

	tree := treeInst.(*instance)
	assert.NilError(t, tree.Param(ProcParam{}))
	tree.In(&gopackages.Packages{Pkgs: pkgs})
	assert.NilError(t, tree.Run(ctx, r))

	inst, err := textmod.NewProc().Instantiate(ctx, false)
	assert.NilError(t, err)

	i, ok := inst.(textmodInstance)
	assert.Assert(t, ok, "%T", inst)
	assert.NilError(t, i.Param(param))
	i.In(&gopackages.Packages{Pkgs: pkgs})
	i.Out(f)
	i.Optional(map[string]any{"tree": tree.AsInput()})

	files, err := i.Files(ctx, r)
	assert.NilError(t, err)
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)

	for _, file := range files {
		t.Log(string(file.Content))
	}

	assert.Equal(t, len(files), 0)
}
