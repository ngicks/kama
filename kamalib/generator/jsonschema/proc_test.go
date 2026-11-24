package jsonschema

import (
	"context"
	jsonv2 "encoding/json/v2"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/und"
	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	_ "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

func TestProcParam(t *testing.T) {
	cases := []struct {
		name  string
		param ProcParamPartial
		want  Options
		err   string
	}{
		{
			name:  "schema only",
			param: procParam("api/schema.json", "", "", ""),
			want:  Options{Package: "api", Schema: "api/schema.json", Dir: "api"},
		},
		{
			name:  "schema at the root",
			param: procParam("schema.json", "", "", ""),
			want:  Options{Schema: "schema.json", Dir: "."},
		},
		{
			name:  "everything set",
			param: procParam("schema/a.json", "gen/types", "model", "Root"),
			want: Options{
				Package: "model", TypeName: "Root", Schema: "schema/a.json", Dir: "gen/types",
			},
		},
		{
			name:  "package from dir",
			param: procParam("a.json", "gen/types", "", ""),
			want:  Options{Package: "types", Schema: "a.json", Dir: "gen/types"},
		},
		{
			name:  "schema with a pointer",
			param: procParam("tests/a.json#/0/schema", "", "", ""),
			want:  Options{Package: "tests", Schema: "tests/a.json#/0/schema", Dir: "tests"},
		},
		{
			name:  "schema with an empty pointer",
			param: procParam("api/a.json#", "", "", ""),
			want:  Options{Package: "api", Schema: "api/a.json#", Dir: "api"},
		},
		{name: "no schema", param: procParam("", "", "", ""), err: `invalid schema ""`},
		{name: "pointer only", param: procParam("#/0", "", "", ""), err: `invalid schema "#/0"`},
		{
			name:  "pointer without a slash",
			param: procParam("a.json#0", "", "", ""),
			err:   `invalid schema "a.json#0": invalid JSON pointer "0"`,
		},
		{
			name:  "pointer with a bad escape",
			param: procParam("a.json#/~2", "", "", ""),
			err:   `invalid JSON pointer "/~2"`,
		},
		{
			name:  "pointer with a bad percent escape",
			param: procParam("a.json#/%zz", "", "", ""),
			err:   `invalid schema "a.json#/%zz"`,
		},
		{name: "schema outside", param: procParam("../a.json", "", "", ""), err: "invalid schema"},
		{
			name:  "dir outside",
			param: procParam("a.json", "../x", "", ""),
			err:   `invalid dir "../x"`,
		},
		{
			name:  "dir not a package name",
			param: procParam("my-api/a.json", "", "", ""),
			err:   `package name "my-api"; set package`,
		},
		{
			name:  "package not a package name",
			param: procParam("a.json", "", "a.b", ""),
			err:   `package name "a.b"`,
		},
		{
			name:  "type name not exported",
			param: procParam("a.json", "", "", "pet"),
			err:   `type name "pet"`,
		},
		{
			name:  "type name not an identifier",
			param: procParam("a.json", "", "", "a b"),
			err:   `type name "a b"`,
		},
		{
			name:  "type name starting with a digit",
			param: procParam("a.json", "", "", "1x"),
			err:   `type name "1x"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := &instance{}

			err := i.Param(tc.param)
			if tc.err != "" {
				assert.ErrorIs(t, err, ErrOptions)
				assert.ErrorContains(t, err, tc.err)

				return
			}

			assert.NilError(t, err)
			assert.DeepEqual(t, i.opts, tc.want)
		})
	}
}

// TestProcParamUnknownKeys builds a spec as the workflow does: a key no
// field names is a warning of the build.
func TestProcParamUnknownKeys(t *testing.T) {
	var spec workflow.Spec
	assert.NilError(t, yaml.Unmarshal([]byte(`
version: 1
steps:
  root: {proc: overlayfs.open}
  types: {proc: jsonschema.generate, out: root, param: {schema: a.json, tpye_name: X}}
`), &spec))

	g, err := spec.Build(context.Background(), workflow.DefaultRegistry)
	assert.NilError(t, err)

	got := opiter.WrapSeq(slices.Values(g.Warnings())).
		Map(error.Error).
		CollectSlice()

	assert.DeepEqual(t, got, []string{"workflow: unknown key: steps.types.param.tpye_name"})
}

const procSchema = `{
  "title": "pet",
  "type": "object",
  "properties": {"name": {"type": "string"}},
  "required": ["name"]
}`

// TestProcRun generates from a schema file staged in the overlay and
// writes the file Generate gives into the directory of the schema.
func TestProcRun(t *testing.T) {
	f := procFs(t, map[string]string{"api/schema.json": procSchema})

	r := runProc(t, f, procParam("api/schema.json", "", "", ""), gentest.Now())
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)
	assert.DeepEqual(t, r.Messages, []string{"1 file written, 0 files unchanged"})

	var doc jsonschema.Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(procSchema), &doc))

	want, _, err := Generate(&doc, Options{
		Package: "api", Schema: "api/schema.json", Now: gentest.Now,
	})
	assert.NilError(t, err)

	got, err := fs.ReadFile(f.FS(), "api/api.kama_jsonschema.go")
	assert.NilError(t, err)
	assert.Equal(t, string(got), string(want[0].Content))
}

// TestProcRunPointer generates from the schema a pointer names inside a
// larger document. The header records the param as given.
func TestProcRunPointer(t *testing.T) {
	f := procFs(t, map[string]string{
		"tests/doc.json": `[{"description": "x", "schemas": {"a/b": {"~c d": ` + procSchema + `}}}]`,
	})

	const schema = "tests/doc.json#/0/schemas/a~1b/~0c%20d"

	r := runProc(t, f, procParam(schema, "", "", ""), gentest.Now())
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)

	var doc jsonschema.Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(procSchema), &doc))

	want, _, err := Generate(&doc, Options{Package: "tests", Schema: schema, Now: gentest.Now})
	assert.NilError(t, err)

	assert.Equal(t, want[0].Path, "tests/tests.kama_jsonschema.go")

	got, err := fs.ReadFile(f.FS(), "tests/tests.kama_jsonschema.go")
	assert.NilError(t, err)
	assert.Equal(t, string(got), string(want[0].Content))
	assert.Assert(t, strings.Contains(string(got), `schema: "`+schema+`"`))
}

// TestSchemaValueKeepsText takes the value a pointer names from the raw
// file, so the order of members and the text of numbers survive.
func TestSchemaValueKeepsText(t *testing.T) {
	f := procFs(t, map[string]string{
		"a.json": `{"s": {"type": "number", "minimum": 1.0, "$comment": "x"}, "t": true}`,
	})

	got, err := schemaValue(f.FS(), "a.json#/s")
	assert.NilError(t, err)
	assert.Equal(t, string(got), `{"type": "number", "minimum": 1.0, "$comment": "x"}`)

	got, err = schemaValue(f.FS(), "a.json#/t")
	assert.NilError(t, err)
	assert.Equal(t, string(got), "true")
}

// TestProcRunWarnsUnknownKeywords passes the warnings of Generate to the
// reporter.
func TestProcRunWarnsUnknownKeywords(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/schema.json": `{"title": "pet", "type": "object", "propertys": {}}`,
	})

	r := runProc(t, f, procParam("api/schema.json", "", "", ""), gentest.Now())
	assert.Equal(t, len(r.Warnings), 1, "%v", r.Warnings)
	assert.ErrorIs(t, r.Warnings[0], ErrUnknownKeyword)
	assert.ErrorContains(t, r.Warnings[0], "#/propertys")
}

// TestProcRunPackageFromRoot takes the base name of the root of the out
// for a schema at the root.
func TestProcRunPackageFromRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "petstore")
	assert.NilError(t, os.Mkdir(root, 0o755))

	f, err := overlayfs.New(root)
	assert.NilError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	assert.NilError(t, f.Write(context.Background(), "schema.json", []byte(procSchema)))

	runProc(t, f, procParam("schema.json", "", "", ""), gentest.Now())

	got, err := fs.ReadFile(f.FS(), "petstore.kama_jsonschema.go")
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(got), "\npackage petstore\n"))
}

// TestProcRunStale runs the proc twice. The second run at a later time
// keeps the file whose header alone would change. A run writing another
// package reports the file of the first as stale.
func TestProcRunStale(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/schema.json": procSchema,
		// A file written by hand in the output directory is never stale.
		"api/hand.go": "package api\n\nfunc hand() {}\n",
	})

	runProc(t, f, procParam("api/schema.json", "", "", ""), gentest.Now())

	r := runProc(t, f, procParam("api/schema.json", "", "", ""), gentest.Now().Add(time.Hour))
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)
	assert.DeepEqual(t, r.Messages, []string{"0 files written, 1 file unchanged"})

	r = runProc(t, f, procParam("api/schema.json", "", "pets", ""), gentest.Now())
	assert.DeepEqual(t, r.Messages, []string{"1 file written, 0 files unchanged"})
	assert.Equal(t, len(r.Warnings), 1, "%v", r.Warnings)
	assert.ErrorIs(t, r.Warnings[0], base.ErrStale)
	assert.ErrorContains(
		t, r.Warnings[0], "api/api.kama_jsonschema.go was generated by kama:jsonschema",
	)
}

func TestProcRunFails(t *testing.T) {
	cases := []struct {
		name    string
		schema  string
		pointer string
		is      error
		msg     string
	}{
		{
			name:   "unsupported",
			schema: `{"title": "a", "type": "object", "properties": {"x": {"not": {}}}}`,
			is:     ErrUnsupported,
			msg:    "jsonschema: api/schema.json: jsonschema: unsupported schema: #/properties/x/not:",
		},
		{name: "invalid json", schema: `{"title": `, msg: "jsonschema: api/schema.json: "},
		{name: "missing", is: fs.ErrNotExist, msg: "api/schema.json"},
		{
			name:    "missing with a pointer",
			pointer: "/0",
			is:      fs.ErrNotExist,
			msg:     "api/schema.json",
		},
		{
			name:    "no member",
			schema:  `{"a": {}}`,
			pointer: "/b",
			msg:     `jsonschema: api/schema.json#/b: no member "b"`,
		},
		{
			name:    "index out of range",
			schema:  `[{}]`,
			pointer: "/1",
			msg:     "api/schema.json#/1: index 1 out of range of 1 elements",
		},
		{
			name:    "index with a leading zero",
			schema:  `[{}]`,
			pointer: "/00",
			msg:     `api/schema.json#/00: "00" is no array index`,
		},
		{
			name:    "name into an array",
			schema:  `[{}]`,
			pointer: "/a",
			msg:     `api/schema.json#/a: "a" is no array index`,
		},
		{
			name:    "step into a scalar",
			schema:  `{"a": 1}`,
			pointer: "/a/b",
			msg:     `api/schema.json#/a/b: token "b" steps into a value of kind number`,
		},
		{
			name:    "no schema",
			schema:  `[{"description": "x"}]`,
			pointer: "/0/description",
			msg:     "api/schema.json#/0/description: points to a value of kind string, not to a schema",
		},
		{
			name:    "invalid json under a pointer",
			schema:  `{"a": `,
			pointer: "/a",
			msg:     "jsonschema: api/schema.json#/a: ",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			if tc.schema != "" {
				files["api/schema.json"] = tc.schema
			}

			f := procFs(t, files)

			schema := "api/schema.json"
			if tc.pointer != "" {
				schema += "#" + tc.pointer
			}

			i := &instance{}
			assert.NilError(t, i.Param(procParam(schema, "", "", "")))
			i.Out(f)

			err := i.Run(context.Background(), gentest.NopReporter{})
			if tc.is != nil {
				assert.ErrorIs(t, err, tc.is)
			}

			assert.ErrorContains(t, err, tc.msg)
		})
	}
}

// procParam returns the param a spec holding these keys decodes into. An
// empty argument leaves its key out.
func procParam(schema, dir, pkg, typeName string) ProcParamPartial {
	set := func(s string) und.Und[string] {
		if s == "" {
			return und.Undefined[string]()
		}

		return und.Defined(s)
	}

	return ProcParamPartial{
		Schema: set(schema), Dir: set(dir), Package: set(pkg), TypeName: set(typeName),
	}
}

// procFs returns an overlay over a new directory holding files.
func procFs(t *testing.T, files map[string]string) *overlayfs.Fs {
	t.Helper()

	f, err := overlayfs.New(t.TempDir())
	assert.NilError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	for name, src := range files {
		assert.NilError(t, f.Write(context.Background(), name, []byte(src)))
	}

	return f
}

// runProc runs the proc with param over f at now.
func runProc(
	t *testing.T,
	f *overlayfs.Fs,
	param ProcParamPartial,
	now time.Time,
) *gentest.RecordReporter {
	t.Helper()

	i := &instance{}
	assert.NilError(t, i.Param(param))
	i.opts.Now = func() time.Time { return now }
	i.Out(f)

	r := &gentest.RecordReporter{}
	assert.NilError(t, i.Run(context.Background(), r))

	return r
}
