package textmod

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

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

type procInstance interface {
	Param(p base.ParamOf[ProcParam]) error
	In(pkgs *gopackages.Packages)
	Out(f *overlayfs.Fs)
	Files(ctx context.Context, r workflow.Reporter) ([]base.File, error)
}

// TestFixtureUpToDate runs the proc with the param of the replace step of
// spec.yaml on the fixture. Its mods set the values the fixture holds, so
// the run changes no file.
func TestFixtureUpToDate(t *testing.T) {
	ctx := context.Background()

	b, err := os.ReadFile(filepath.Join(fixtureDir, "spec.yaml"))
	assert.NilError(t, err)

	var spec workflow.Spec
	assert.NilError(t, yaml.Unmarshal(b, &spec))

	raw, err := json.Marshal(spec.Steps["replace"].Param)
	assert.NilError(t, err)

	var param base.ParamOf[ProcParam]
	assert.NilError(t, json.Unmarshal(raw, &param))
	assert.Equal(t, len(param.ToValue().Mods), 4)

	f, err := overlayfs.New(fixtureDir)
	assert.NilError(t, err)

	defer f.Close()

	pkgs, err := pkggopackages.Load(ctx, f, pkggopackages.Options{Patterns: []string{"./..."}})
	assert.NilError(t, err)

	inst, err := NewProc().Instantiate(ctx, false)
	assert.NilError(t, err)

	i, ok := inst.(procInstance)
	assert.Assert(t, ok, "%T", inst)
	assert.NilError(t, i.Param(param))
	i.In(&gopackages.Packages{Pkgs: pkgs})
	i.Out(f)

	r := &gentest.RecordReporter{}
	files, err := i.Files(ctx, r)
	assert.NilError(t, err)
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)
	assert.Equal(t, len(files), 0, "%v", files)

	// Another version var changes the version only.
	var next base.ParamOf[ProcParam]
	assert.NilError(
		t,
		json.Unmarshal([]byte(strings.Replace(string(raw), "v1.10.0", "v2.0.0", 1)), &next),
	)
	assert.NilError(t, i.Param(next))

	files, err = i.Files(ctx, r)
	assert.NilError(t, err)
	assert.Equal(t, len(files), 1, "%v", files)

	want, err := os.ReadFile(filepath.Join(fixtureDir, "version/version.go"))
	assert.NilError(t, err)
	assert.Equal(t, files[0].Path, "version/version.go")
	assert.Equal(
		t,
		string(files[0].Content),
		strings.Replace(string(want), `"v1.10.0"`, `"v2.0.0"`, 1),
	)
}
