package metadata

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
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
	assert.NilError(t, f.Write("codegen/meta.yaml", []byte("name: yaml\n")))

	param := workflow.NewParam(
		map[string]any{"base": "codegen/meta", "formats": []any{"yaml", "json"}},
	)
	inst, err := Proc{}.BuildContext(ctx, param, false)
	assert.NilError(t, err)

	var doc Document
	inst.(*instance).In(f)
	inst.(*instance).Out(&doc)
	assert.NilError(t, inst.Run(ctx))

	assert.Equal(t, doc.Path, "codegen/meta.yaml")
	assert.DeepEqual(t, doc.Data, map[string]any{"name": "yaml"})
}

func TestProcParamErrors(t *testing.T) {
	ctx := context.Background()

	_, err := Proc{}.BuildContext(ctx, workflow.NewParam(map[string]any{}), true)
	assert.ErrorContains(t, err, "base is required")

	_, err = Proc{}.BuildContext(
		ctx,
		workflow.NewParam(map[string]any{"base": "m", "formats": []any{"ini"}}),
		true,
	)
	assert.ErrorIs(t, err, ErrUnknownFormat)
}
