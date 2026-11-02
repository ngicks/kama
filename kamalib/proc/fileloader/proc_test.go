package fileloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

func TestProcRun(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	assert.NilError(t, os.MkdirAll(filepath.Join(root, "codegen"), 0o755))
	assert.NilError(
		t,
		os.WriteFile(filepath.Join(root, "codegen", "meta.json"), []byte(`{"name":"json"}`), 0o644),
	)

	f, err := overlayfs.New(root)
	assert.NilError(t, err)
	defer f.Close()

	// A staged yaml file wins over the json file on disk.
	assert.NilError(t, f.Write(ctx, "codegen/meta.yaml", []byte("name: yaml\n")))

	inst, err := Proc{}.Instantiate(ctx, false)
	assert.NilError(t, err)

	i := inst.(*instance)
	assert.NilError(
		t,
		i.Param(
			ProcParam{
				Base:    "codegen/meta",
				Formats: []fileloader.Format{fileloader.FormatYaml, fileloader.FormatJson},
			},
		),
	)
	i.In(f)
	assert.NilError(t, i.Run(ctx))

	doc := i.AsInput()
	assert.Equal(t, doc.Path, "codegen/meta.yaml")
	assert.DeepEqual(t, doc.Data, map[string]any{"name": "yaml"})
}

func TestProcParamErrors(t *testing.T) {
	i := &instance{}

	assert.ErrorContains(t, i.Param(ProcParam{}), "base is required")
	assert.ErrorIs(
		t,
		i.Param(ProcParam{Base: "m", Formats: []fileloader.Format{"ini"}}),
		fileloader.ErrUnknownFormat,
	)
}
