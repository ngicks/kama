package jsonschema

import (
	"context"
	jsonv2 "encoding/json/v2"
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/und"
	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procpackages "github.com/ngicks/kama/kamalib/proc/gopackages"
	_ "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

func TestProcParam(t *testing.T) {
	cases := []struct {
		name  string
		param ProcParamPartial
		err   string
	}{
		{name: "defaults"},
		{name: "type name", param: typeNameParam("Root")},
		{name: "type name not exported", param: typeNameParam("pet"), err: `type name "pet"`},
		{name: "type name not an identifier", param: typeNameParam("a b"), err: `type name "a b"`},
		{
			name:  "type name starting with a digit",
			param: typeNameParam("1x"),
			err:   `type name "1x"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := newInstance(t, gentest.Now()).Param(tc.param)
			if tc.err == "" {
				assert.NilError(t, err)
				return
			}

			assert.ErrorIs(t, err, ErrOptions)
			assert.ErrorContains(t, err, tc.err)
		})
	}
}

// TestProcParamUnknownKeys builds a spec as the workflow does: a key no
// field names is a warning of the build, a key of a remote included.
func TestProcParamUnknownKeys(t *testing.T) {
	var spec workflow.Spec
	assert.NilError(t, yaml.Unmarshal([]byte(`
version: 1
steps:
  root: {proc: overlayfs.open}
  load: {proc: packages.load, in: root}
  types:
    proc: jsonschema.generate
    in: load
    out: root
    param: {tpye_name: X, remotes: [{uri: "http://x.test/", dir: a, flie: b}]}
`), &spec))

	g, err := spec.Build(context.Background(), workflow.DefaultRegistry)
	assert.NilError(t, err)

	got := opiter.WrapSeq(slices.Values(g.Warnings())).
		Map(error.Error).
		Collect(slices.Sorted)

	assert.DeepEqual(t, got, []string{
		"workflow: unknown key: steps.types.param.remotes.0.flie",
		"workflow: unknown key: steps.types.param.tpye_name",
	})
}

func TestProcParamRemotes(t *testing.T) {
	for _, tc := range []struct {
		remote Remote
		err    string
	}{
		{Remote{Dir: "a"}, "remote 0 has no uri"},
		{Remote{Uri: "http://x.test/"}, `remote "http://x.test/" needs either dir or file`},
		{Remote{Uri: "http://x.test/", Dir: "a", File: "b"}, "needs either dir or file"},
		{Remote{Uri: "http://x.test/", Dir: "../a"}, `invalid path "../a"`},
		{Remote{Uri: "http://x.test/", File: "../a.json#/b"}, `invalid path "../a.json"`},
		{Remote{Uri: "http://x.test/", File: "a.json#b"}, `invalid JSON pointer "b"`},
	} {
		err := newInstance(t, gentest.Now()).Param(remotesParam(tc.remote))
		assert.ErrorIs(t, err, ErrOptions)
		assert.ErrorContains(t, err, tc.err)
	}
}

// TestProcRunRemotes generates a schema referring to three other
// documents, two of which remotes with dir map and one a remote with file.
func TestProcRunRemotes(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/api.go": "package api\n",
		"api/kama_jsonschema.json": `{"title": "pet", "type": "object", "properties": {
			"tag": {"$ref": "http://x.test/schemas/tag.json"},
			"owner": {"$ref": "http://y.test/owner#/$defs/name"},
			"kind": {"$ref": "kind.json"}}}`,
		"remote/schemas/tag.json": `{"type": "string", "maxLength": 3}`,
		"owner.json":              `{"$defs": {"name": {"type": "string"}}}`,
		"local/kind.json":         `{"type": "string", "enum": ["cat", "dog"]}`,
	})

	r := runProc(t, f, remotesParam(
		Remote{Uri: "http://x.test/", Dir: inline(t, "remote")},
		Remote{Uri: "http://y.test/owner", File: inline(t, "owner.json")},
		// The root has no $id, so kind.json resolves to /kind.json.
		Remote{Uri: "/", Dir: inline(t, "local")},
	), gentest.Now())
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)

	got, err := fs.ReadFile(f.FS(), inline(t, "api/api.kama_jsonschema.go"))
	assert.NilError(t, err)

	// gofmt aligns the fields of a struct, so runs of spaces compare as one.
	spaced := strings.Join(strings.Fields(string(got)), " ")
	for _, want := range []string{
		"type Tag string", "opt.Option[Tag]", "Owner opt.Option[string]", "KindCat Kind = \"cat\"",
	} {
		assert.Assert(t, strings.Contains(spaced, want), "%s: %s", want, got)
	}
}

// TestProcRunRemotePointer generates a schema referring to a document a
// remote with file names by a pointer into a larger file, under a key
// holding / and a space.
func TestProcRunRemotePointer(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/api.go": "package api\n",
		"api/kama_jsonschema.json": `{"title": "pet", "type": "object", "properties": {
			"tag": {"$ref": "http://x.test/tag.json"}}}`,
		"remote.json": `{"docs": {"http://x.test/tag json": {"type": "string", "maxLength": 3}}}`,
	})

	r := runProc(t, f, remotesParam(Remote{
		Uri:  "http://x.test/tag.json",
		File: inline(t, "remote.json") + "#/docs/http:~1~1x.test~1tag%20json",
	}), gentest.Now())
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)

	got, err := fs.ReadFile(f.FS(), inline(t, "api/api.kama_jsonschema.go"))
	assert.NilError(t, err)

	spaced := strings.Join(strings.Fields(string(got)), " ")
	assert.Assert(t, strings.Contains(spaced, "opt.Option[Tag]"), "%s", got)
}

// TestProcRunRemoteMissing checks that a reference no remote covers fails
// naming the reference and the place holding it.
func TestProcRunRemoteMissing(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/api.go": "package api\n",
		"api/kama_jsonschema.json": `{"title": "pet", "properties": {
			"a": {"$ref": "http://z.test/a.json"}}}`,
	})

	_, err := runProcErr(
		t, f, remotesParam(Remote{Uri: "http://x.test/", Dir: "remote"}), gentest.Now(),
	)
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorContains(t, err,
		`#/properties/a/$ref: reference "http://z.test/a.json" to "http://z.test/a.json": `+
			"no remote covers the URI")
}

const procSchema = `{
  "title": "pet",
  "type": "object",
  "properties": {"name": {"type": "string"}},
  "required": ["name"]
}`

// TestProcRun generates from the schema file of each package into the
// directory of the package: the default file of one, the file a directive
// names for the other. A package without a schema file generates nothing.
func TestProcRun(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/api.go":               "package api\n",
		"api/kama_jsonschema.json": procSchema,
		"named/named.go": "//kama:option-file jsonschema=../schemas/pet.schema.json\n\n" +
			"package named\n",
		"schemas/pet.schema.json": procSchema,
		"none/none.go":            "package none\n",
	})

	r := runProc(t, f, ProcParamPartial{}, gentest.Now())
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)
	// The declarations, their helpers and the stubs of their Validate
	// methods, for each package.
	assert.DeepEqual(t, r.Messages, []string{"6 files written, 0 files unchanged"})

	var doc jsonschema.Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(procSchema), &doc))

	for _, c := range []struct{ pkg, schema string }{
		{"api", "api/kama_jsonschema.json"},
		{"named", "schemas/pet.schema.json"},
	} {
		want, _, err := generateFiles(&doc, Options{
			Package: c.pkg, Schema: inline(t, c.schema), Validate: true,
		})
		assert.NilError(t, err)

		got, err := fs.ReadFile(f.FS(), inline(t, c.pkg+"/"+c.pkg+".kama_jsonschema.go"))
		assert.NilError(t, err)
		assert.Equal(t, string(got), string(want[0].Content))
	}
}

// TestProcRunTests generates, with tests loaded, from a schema file named
// in a file of a package, in an in-package test file and in an external
// test package sharing the directory of a package. The files of the
// external test package are test files, and every package builds with its
// tests after validate.generate.
func TestProcRunTests(t *testing.T) {
	f := procFs(t, map[string]string{
		"d/d.go":      "//kama:option-file jsonschema=a.json\n\npackage d\n",
		"d/x_test.go": "//kama:option-file jsonschema=b.json\n\npackage d_test\n",
		"d/a.json":    strings.Replace(procSchema, "pet", "a", 1),
		"d/b.json":    strings.Replace(procSchema, "pet", "b", 1),
		"e/e.go":      "package e\n",
		"e/e_test.go": "//kama:option-file jsonschema=s.json\n\npackage e\n",
		"e/s.json":    procSchema,
	})

	r, err := runProcLoad(
		t, f, ProcParamPartial{}, gentest.Now(),
		gopackages.Options{AllowTypeErrors: true, Tests: true},
	)
	assert.NilError(t, err)
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)

	validateFs(t, f, true)

	for _, name := range []string{
		"d/d.kama_jsonschema.go",
		"d/zz_gen.helper.kama_jsonschema.go",
		"d/d.kama_jsonschema_test.go",
		"d/zz_gen.helper.kama_jsonschema_test.go",
		"e/e.kama_jsonschema.go",
		"e/zz_gen.helper.kama_jsonschema.go",
	} {
		_, err := fs.Stat(f.FS(), inline(t, name))
		assert.NilError(t, err, name)
	}
}

// TestProcRunSchemaNamedTwice takes one file named by two directives for
// one schema file.
func TestProcRunSchemaNamedTwice(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/api.go": "//kama:option-file jsonschema=a.json\n" +
			"//kama:option-file jsonschema=./a.json\n\npackage api\n",
		"api/a.json": procSchema,
	})

	r := runProc(t, f, ProcParamPartial{}, gentest.Now())
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)
}

// TestProcRunTestOnlyPackage runs over a directory holding test files
// only, which a load without tests gives as a package without files.
func TestProcRunTestOnlyPackage(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/api.go":               "package api\n",
		"api/kama_jsonschema.json": procSchema,
		"t/t_test.go":              "package t\n",
		"t/kama_jsonschema.json":   procSchema,
	})

	runProc(t, f, ProcParamPartial{}, gentest.Now())

	_, err := fs.Stat(f.FS(), inline(t, "t/t.kama_jsonschema.go"))
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// TestProcRunPackageName takes the name of the package, which the base name
// of its directory need not be.
func TestProcRunPackageName(t *testing.T) {
	f := procFs(t, map[string]string{
		"my-api/api.go":               "package api\n",
		"my-api/kama_jsonschema.json": procSchema,
	})

	runProc(t, f, ProcParamPartial{}, gentest.Now())

	got, err := fs.ReadFile(f.FS(), inline(t, "my-api/api.kama_jsonschema.go"))
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(got), "\npackage api\n"))
}

// TestSchemaValueKeepsText takes the value a pointer names from the raw
// file, so the order of members and the text of numbers survive.
func TestSchemaValueKeepsText(t *testing.T) {
	fsys := fstest.MapFS{"a.json": {
		Data: []byte(`{"s": {"type": "number", "minimum": 1.0, "$comment": "x"}, "t": true}`),
	}}

	got, err := schemaValue(fsys, "a.json#/s")
	assert.NilError(t, err)
	assert.Equal(t, string(got), `{"type": "number", "minimum": 1.0, "$comment": "x"}`)

	got, err = schemaValue(fsys, "a.json#/t")
	assert.NilError(t, err)
	assert.Equal(t, string(got), "true")

	got, err = schemaValue(fsys, "a.json#")
	assert.NilError(t, err)
	assert.Equal(t, string(got), string(fsys["a.json"].Data))
}

func TestSchemaValueErrors(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		pointer string
		is      error
		msg     string
	}{
		{name: "missing", pointer: "/0", is: fs.ErrNotExist, msg: "a.json"},
		{name: "no member", file: `{"a": {}}`, pointer: "/b", msg: `a.json#/b: no member "b"`},
		{
			name:    "index out of range",
			file:    `[{}]`,
			pointer: "/1",
			msg:     "a.json#/1: index 1 out of range of 1 elements",
		},
		{
			name:    "index with a leading zero",
			file:    `[{}]`,
			pointer: "/00",
			msg:     `a.json#/00: "00" is no array index`,
		},
		{
			name:    "name into an array",
			file:    `[{}]`,
			pointer: "/a",
			msg:     `a.json#/a: "a" is no array index`,
		},
		{
			name:    "step into a scalar",
			file:    `{"a": 1}`,
			pointer: "/a/b",
			msg:     `a.json#/a/b: token "b" steps into a value of kind number`,
		},
		{
			name:    "no schema",
			file:    `[{"description": "x"}]`,
			pointer: "/0/description",
			msg:     "a.json#/0/description: points to a value of kind string, not to a schema",
		},
		{name: "invalid json under a pointer", file: `{"a": `, pointer: "/a", msg: "a.json#/a: "},
		{
			name:    "pointer without a slash",
			file:    `{}`,
			pointer: "0",
			msg:     `invalid JSON pointer "0"`,
		},
		{
			name:    "pointer with a bad escape",
			file:    `{}`,
			pointer: "/~2",
			msg:     `invalid JSON pointer "/~2"`,
		},
		{name: "pointer with a bad percent escape", file: `{}`, pointer: "/%zz", msg: `"%zz"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			if tc.file != "" {
				fsys["a.json"] = &fstest.MapFile{Data: []byte(tc.file)}
			}

			_, err := schemaValue(fsys, "a.json#"+tc.pointer)
			if tc.is != nil {
				assert.ErrorIs(t, err, tc.is)
			}

			assert.ErrorContains(t, err, tc.msg)
		})
	}
}

// TestProcRunWarnsUnknownKeywords passes the warnings of Generate to the
// reporter.
func TestProcRunWarnsUnknownKeywords(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/api.go":               "package api\n",
		"api/kama_jsonschema.json": `{"title": "pet", "type": "object", "propertys": {}}`,
	})

	r := runProc(t, f, ProcParamPartial{}, gentest.Now())
	assert.Equal(t, len(r.Warnings), 1, "%v", r.Warnings)
	assert.ErrorIs(t, r.Warnings[0], ErrUnknownKeyword)
	assert.ErrorContains(t, r.Warnings[0], "#/propertys")
}

// TestProcRunStale runs the proc three times. The second run at a later
// time keeps the file whose header alone would change. A run on the
// package once it names no schema file reports the files of the first as
// stale.
func TestProcRunStale(t *testing.T) {
	// A file written by hand in the directory of the package is never
	// stale.
	const hand = "package api\n\nfunc hand() {}\n"

	f := procFs(t, map[string]string{
		"api/schema.json": procSchema,
		"api/hand.go":     "//kama:option-file jsonschema=schema.json\n\n" + hand,
	})

	runProc(t, f, ProcParamPartial{}, gentest.Now())

	// The stubs exist and the declarations are those they were written for,
	// so the second run leaves them alone.
	r := runProc(t, f, ProcParamPartial{}, gentest.Now().Add(time.Hour))
	assert.Equal(t, len(r.Warnings), 0, "%v", r.Warnings)
	assert.DeepEqual(t, r.Messages, []string{"0 files written, 2 files unchanged"})

	assert.NilError(t, f.Write(context.Background(), inline(t, "api/hand.go"), []byte(hand)))

	r = runProc(t, f, ProcParamPartial{}, gentest.Now())

	got := opiter.WrapSeq(slices.Values(r.Warnings)).
		Map(func(w error) string {
			assert.ErrorIs(t, w, base.ErrStale)

			name, _, _ := strings.Cut(w.Error(), " holds declarations generated by kama:jsonschema")

			return path.Base(name)
		}).
		CollectSlice()

	assert.DeepEqual(t, got, []string{"api.kama_jsonschema.go", "zz_gen.helper.kama_jsonschema.go"})
}

func TestProcRunFails(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		is    error
		msg   string
	}{
		{
			name: "unsupported",
			files: map[string]string{"api/kama_jsonschema.json": `{"title": "a", "type": "object",
				"properties": {"x": {"$dynamicRef": "#a"}}}`},
			is: ErrUnsupported,
			msg: "/api/kama_jsonschema.json: jsonschema: unsupported schema: " +
				"#/properties/x/$dynamicRef:",
		},
		{
			name:  "invalid json",
			files: map[string]string{"api/kama_jsonschema.json": `{"title": `},
			msg:   "/api/kama_jsonschema.json: ",
		},
		{
			name: "missing",
			files: map[string]string{
				"api/api.go": "//kama:option-file jsonschema=schema.json\n\npackage api\n",
			},
			is:  fs.ErrNotExist,
			msg: "api.go:1:1",
		},
		{
			name: "two schema files",
			files: map[string]string{
				"api/api.go": "//kama:option-file jsonschema=a.json jsonschema=b.json\n\npackage api\n",
				"api/a.json": procSchema,
				"api/b.json": procSchema,
			},
			is:  ErrOptions,
			msg: "/api has 2 schema files; want one",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tc.files["api/api.go"]; !ok {
				tc.files["api/api.go"] = "package api\n"
			}

			_, err := runProcErr(t, procFs(t, tc.files), ProcParamPartial{}, gentest.Now())
			if tc.is != nil {
				assert.ErrorIs(t, err, tc.is)
			}

			assert.ErrorContains(t, err, tc.msg)
		})
	}
}

func typeNameParam(name string) ProcParamPartial {
	return ProcParamPartial{TypeName: und.Defined(name)}
}

func remotesParam(remotes ...Remote) ProcParamPartial {
	return ProcParamPartial{Remotes: und.Defined(remotes)}
}

// procInstance is an instance of the proc of NewProc.
type procInstance interface {
	Param(p ProcParamPartial) error
	In(pkgs *procpackages.Packages)
	Out(f *overlayfs.Fs)
	Run(ctx context.Context, r workflow.Reporter) error
}

// newInstance returns an instance of the proc whose runs take place at now.
func newInstance(t *testing.T, now time.Time) procInstance {
	t.Helper()

	p := proc
	p.Now = func() time.Time { return now }

	inst, err := p.Instantiate(context.Background(), false)
	assert.NilError(t, err)

	i, ok := inst.(procInstance)
	assert.Assert(t, ok, "%T", inst)

	return i
}

// inline returns name, a slash-separated path relative to the directory
// procFs stages the files of t in, relative to the root of the overlay.
func inline(t *testing.T, name string) string { return path.Join(gentest.InlineDir(t), name) }

// procFs stages files as gentest.Stage does. They lie in this module, so
// that the packages the proc generates load with the modules the generated
// code imports.
func procFs(t *testing.T, files map[string]string) *overlayfs.Fs {
	t.Helper()

	f, _ := gentest.Stage(t, files)

	return f
}

// runProc runs the proc as runProcErr does, which must succeed.
func runProc(
	t *testing.T,
	f *overlayfs.Fs,
	param ProcParamPartial,
	now time.Time,
) *gentest.RecordReporter {
	t.Helper()

	r, err := runProcErr(t, f, param, now)
	assert.NilError(t, err)

	return r
}

// runProcErr loads the packages procFs staged for t from f, allowing type
// errors as the first load of a spec does, and runs the proc with param over
// them into f at now.
func runProcErr(
	t *testing.T,
	f *overlayfs.Fs,
	param ProcParamPartial,
	now time.Time,
) (*gentest.RecordReporter, error) {
	t.Helper()

	return runProcLoad(t, f, param, now, gopackages.Options{AllowTypeErrors: true})
}

// runProcLoad is runProcErr loading the packages with load, whose patterns
// it sets.
func runProcLoad(
	t *testing.T,
	f *overlayfs.Fs,
	param ProcParamPartial,
	now time.Time,
	load gopackages.Options,
) (*gentest.RecordReporter, error) {
	t.Helper()

	ctx := context.Background()

	load.Patterns = inlinePatterns(t, f)

	pkgs, err := gopackages.Load(ctx, f, load)
	assert.NilError(t, err)

	i := newInstance(t, now)
	assert.NilError(t, i.Param(param))
	i.In(&procpackages.Packages{Pkgs: pkgs})
	i.Out(f)

	r := &gentest.RecordReporter{}

	return r, i.Run(ctx, r)
}

// inlinePatterns returns a pattern for each directory procFs staged a Go
// file of t in. The go command matches no package of ./... in a directory
// the overlay holds alone, so each one is named.
func inlinePatterns(t *testing.T, f *overlayfs.Fs) []string {
	t.Helper()

	var patterns []string

	assert.NilError(t, fs.WalkDir(f.FS(), gentest.InlineDir(t), func(
		name string, d fs.DirEntry, err error,
	) error {
		if err == nil && !d.IsDir() && path.Ext(name) == ".go" {
			patterns = append(patterns, "./"+path.Dir(name))
		}

		return err
	}))

	slices.Sort(patterns)

	return slices.Compact(patterns)
}
