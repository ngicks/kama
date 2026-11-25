package jsonschema

import (
	"context"
	jsonv2 "encoding/json/v2"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/generator/validator"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	procpackages "github.com/ngicks/kama/kamalib/proc/gopackages"
)

const stubsFilePath = "api/api.kama_jsonschema.kama_validate.go"

// TestStubs writes the stubs of the methods the decoders call into the
// file of the validator generator, under its mark: Validate for every type
// carrying //kama:validate and ValidateShallow for those decoding
// validated values.
func TestStubs(t *testing.T) {
	f := procFs(t, map[string]string{"api/api.go": "package api\n", "api/kama_jsonschema.json": `{
		"title": "pet", "type": "object",
		"properties": {"tag": {"type": "string", "enum": ["a", "b"]}}
	}`})

	runProc(t, f, ProcParamPartial{}, gentest.Now())

	got, err := fs.ReadFile(f.FS(), inline(t, stubsFilePath))
	assert.NilError(t, err)

	for _, want := range []string{
		"//kama:generated kama:validate\nfunc (v Pet) Validate(stopOnFirstError bool) error {",
		"//kama:generated kama:validate\nfunc (v Pet) ValidateShallow(stopOnFirstError bool) error {",
		"func (v PetTag) Validate(stopOnFirstError bool) error {",
		"func (v PetTag) ValidateShallow(stopOnFirstError bool) error {",
		`return validator.New(validator.ErrStub, "Pet.Validate; run validate.generate ` +
			`over the package")`,
	} {
		assert.Assert(t, strings.Contains(string(got), want), "want %q in\n%s", want, got)
	}
}

// TestStubsReplaceStaleMethods keeps the file of the validator generator
// while the declarations stay those of the existing file, and replaces it
// by stubs once they change, since its methods may refer to what the run
// drops.
func TestStubsReplaceStaleMethods(t *testing.T) {
	// The methods the decoders call, so that the package loads.
	const methods = "package api\n\n//kama:generated kama:validate\n" +
		"func (v Pet) Validate(stopOnFirstError bool) error { return nil }\n\n" +
		"//kama:generated kama:validate\n" +
		"func (v Pet) ValidateShallow(stopOnFirstError bool) error { return nil }\n"

	f := procFs(
		t,
		map[string]string{"api/api.go": "package api\n", "api/kama_jsonschema.json": procSchema},
	)

	runProc(t, f, ProcParamPartial{}, gentest.Now())
	assert.NilError(t, f.Write(context.Background(), inline(t, stubsFilePath), []byte(methods)))

	runProc(t, f, ProcParamPartial{}, gentest.Now().Add(time.Hour))

	got, err := fs.ReadFile(f.FS(), inline(t, stubsFilePath))
	assert.NilError(t, err)
	assert.Equal(t, string(got), methods)

	assert.NilError(t, f.Write(context.Background(), inline(t, "api/kama_jsonschema.json"), []byte(
		strings.Replace(procSchema, `"required"`, `"description": "A pet.", "required"`, 1),
	)))

	runProc(t, f, ProcParamPartial{}, gentest.Now())

	got, err = fs.ReadFile(f.FS(), inline(t, stubsFilePath))
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(got), "validator.ErrStub"), "%s", got)
}

// TestStubsWithoutValidate writes no stub for decoders calling no
// Validate.
func TestStubsWithoutValidate(t *testing.T) {
	f := procFs(
		t,
		map[string]string{"api/api.go": "package api\n", "api/kama_jsonschema.json": procSchema},
	)

	runProc(t, f, ProcParamPartial{Validate: und.Defined(false)}, gentest.Now())

	_, err := fs.Stat(f.FS(), inline(t, stubsFilePath))
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// TestHelperNameConflict rejects a declaration written by hand named like a
// helper the generated decoders call, and one standing in the helpers file.
func TestHelperNameConflict(t *testing.T) {
	for _, files := range []map[string]string{
		{"api/hand.go": "package api\n\nfunc kamaJsonschemaDecodeObject() {}\n"},
		{"api/zz_gen.helper.kama_jsonschema.go": "package api\n\nfunc hand() {}\n"},
	} {
		files["api/api.go"] = "package api\n"
		files["api/kama_jsonschema.json"] = procSchema

		_, err := runProcErr(t, procFs(t, files), ProcParamPartial{}, gentest.Now())
		assert.ErrorIs(t, err, ErrConflict)
	}
}

// TestHelperNameOtherPackage takes no declaration of the external test
// package sharing the directory for one named like a helper.
func TestHelperNameOtherPackage(t *testing.T) {
	f := procFs(t, map[string]string{
		"api/api.go":               "package api\n",
		"api/kama_jsonschema.json": procSchema,
		"api/x_test.go":            "package api_test\n\nfunc kamaJsonschemaDecodeObject() {}\n",
	})

	runProc(t, f, ProcParamPartial{}, gentest.Now())
}

// TestHelperScanIgnoresSchemaText generates from a schema whose names, a
// description and an enum value spell identifiers of the helper prefix.
// Only identifiers of the code select helpers, so the run succeeds, the
// package builds and the helpers file holds only helpers the code calls.
func TestHelperScanIgnoresSchemaText(t *testing.T) {
	f := procFs(t, map[string]string{
		"h/h.go": "package h\n",
		"h/kama_jsonschema.json": `{"title": "kamaJsonschemaFoo", "type": "object",
			"description": "See kamaJsonschemaNote and kamaJsonschemaDecode.",
			"properties": {
				"kamaJsonschemaVersion": {"type": "string"},
				"tag": {"enum": ["kamaJsonschemaX"]}}}`,
	})

	runProc(t, f, ProcParamPartial{}, gentest.Now())
	buildFs(t, f)

	got, err := fs.ReadFile(f.FS(), inline(t, "h/zz_gen.helper.kama_jsonschema.go"))
	assert.NilError(t, err)

	for _, unused := range []string{"kamaJsonschemaRequiredNullable", "kamaJsonschemaUnmarshal("} {
		assert.Assert(t, !strings.Contains(string(got), "func "+unused), "%s in\n%s", unused, got)
	}
}

// validateFs runs validate.generate over the packages procFs staged for t
// in f and checks that they build then.
func validateFs(t *testing.T, f *overlayfs.Fs, tests bool) {
	t.Helper()

	ctx := context.Background()
	load := gopackages.Options{Patterns: inlinePatterns(t, f), Tests: tests}

	pkgs, err := gopackages.Load(ctx, f, load)
	assert.NilError(t, err)

	inst, err := validator.NewProc().Instantiate(ctx, false)
	assert.NilError(t, err)

	v, ok := inst.(validateInstance)
	assert.Assert(t, ok, "%T", inst)
	assert.NilError(t, v.Param(validator.ProcParamPartial{}))
	v.In(&procpackages.Packages{Pkgs: pkgs})
	v.Out(f)
	assert.NilError(t, v.Run(ctx, &gentest.RecordReporter{}))
	assert.NilError(t, gentest.Build(f, load))
}

// buildFs checks that the packages procFs staged for t in f build.
func buildFs(t *testing.T, f *overlayfs.Fs) {
	t.Helper()

	assert.NilError(t, gentest.Build(f, gopackages.Options{Patterns: inlinePatterns(t, f)}))
}

// stubSchema is a schema whose root refers to the definition named def.
func stubSchema(def string) string {
	return `{"title": "Top", "type": "object", "properties": {"p": {"$ref": "#/$defs/` + def +
		`"}}, "$defs": {"` + def + `": {"type": "object", "properties": {` +
		`"x": {"type": "integer", "minimum": 0}}}}}`
}

// TestStubsLifecycle changes the schema and the validate param between runs
// over a package whose validator file exists, and checks that the package
// builds after each run, with or without validate.generate after it.
func TestStubsLifecycle(t *testing.T) {
	ctx := context.Background()
	schema := inline(t, "c/kama_jsonschema.json")
	noValidate := ProcParamPartial{Validate: und.Defined(false)}

	f := procFs(
		t,
		map[string]string{"c/c.go": "package c\n", "c/kama_jsonschema.json": stubSchema("Pt")},
	)

	runProc(t, f, ProcParamPartial{}, gentest.Now())
	validateFs(t, f, false)

	// A definition renamed under validate false drops the type the methods
	// of the validator file refer to.
	assert.NilError(t, f.Write(ctx, schema, []byte(stubSchema("Q"))))
	runProc(t, f, noValidate, gentest.Now())
	buildFs(t, f)

	// So does validate turning off with the declarations otherwise equal.
	runProc(t, f, ProcParamPartial{}, gentest.Now())
	validateFs(t, f, false)
	runProc(t, f, noValidate, gentest.Now())
	buildFs(t, f)
	runProc(t, f, noValidate, gentest.Now())
	buildFs(t, f)

	// A struct becoming a union.
	runProc(t, f, ProcParamPartial{}, gentest.Now())
	validateFs(t, f, false)
	assert.NilError(t, f.Write(ctx, schema, []byte(`{"title": "Top", "type": "object",
		"properties": {"p": {"oneOf": [{"type": "string"}, {"type": "integer"}]}}}`)))
	runProc(t, f, ProcParamPartial{}, gentest.Now())
	buildFs(t, f)
}

// TestStubsKeepHandWrittenMethods writes no stub for a method the package
// declares by hand, so that the package still builds. Whether the
// validator generator accepts such a method is its own matter.
func TestStubsKeepHandWrittenMethods(t *testing.T) {
	f := procFs(t, map[string]string{
		"c/c.go": "package c\n\nfunc (v Top) Validate(bool) error { return nil }\n\n" +
			"func (v Pt) ValidateShallow(bool) error { return nil }\n",
		"c/kama_jsonschema.json": stubSchema("Pt"),
	})

	runProc(t, f, ProcParamPartial{}, gentest.Now())

	got, err := fs.ReadFile(f.FS(), inline(t, "c/c.kama_jsonschema.kama_validate.go"))
	assert.NilError(t, err)
	assert.Assert(t, !strings.Contains(string(got), "func (v Top) Validate("), "%s", got)
	assert.Assert(t, !strings.Contains(string(got), "func (v Pt) ValidateShallow("), "%s", got)
	assert.Assert(t, strings.Contains(string(got), "func (v Top) ValidateShallow("), "%s", got)
	assert.Assert(t, strings.Contains(string(got), "func (v Pt) Validate("), "%s", got)

	buildFs(t, f)
}

// TestStubsFailDecoding decodes a valid value into a root union whose
// wrappers hold stubs only. Decoding fails on the stub rather than taking
// the value for the unknown member.
func TestStubsFailDecoding(t *testing.T) {
	f := procFs(t, map[string]string{
		"u/u.go": "package u\n",
		"u/kama_jsonschema.json": `{"title": "Top",
			"oneOf": [{"type": "string", "minLength": 2}, {"type": "integer"}]}`,
		"u/u_test.go": `package u

import (
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/ngicks/kama/kamaruntime/validator"
)

func TestDecode(t *testing.T) {
	var v Top
	err := json.Unmarshal([]byte(` + "`" + `"abcd"` + "`" + `), &v)
	if !errors.Is(err, validator.ErrStub) {
		t.Fatalf("got %v, %T", err, v.Kind)
	}
}
`,
	})

	runProc(t, f, ProcParamPartial{}, gentest.Now())
	goTestFs(t, f, inline(t, "u"))
}

// goTestFs builds the tests of the package of dir as f shows it and runs
// them.
func goTestFs(t *testing.T, f *overlayfs.Fs, dir string) {
	t.Helper()

	files, err := f.Overlay()
	assert.NilError(t, err)

	tmp := t.TempDir()
	replace := map[string]string{}

	for name, content := range files {
		staged := filepath.Join(tmp, strconv.Itoa(len(replace))+filepath.Ext(name))
		assert.NilError(t, os.WriteFile(staged, content, 0o644))
		replace[name] = staged
	}

	b, err := jsonv2.Marshal(map[string]any{"Replace": replace})
	assert.NilError(t, err)

	overlay := filepath.Join(tmp, "overlay.json")
	assert.NilError(t, os.WriteFile(overlay, b, 0o644))

	// The test binary runs in no directory of the package, which the
	// overlay holds alone.
	bin := filepath.Join(tmp, "pkg.test")
	cmd := exec.Command("go", "test", "-c", "-vet=off", "-overlay="+overlay, "-o", bin, "./"+dir)
	cmd.Dir = f.Root()

	out, err := cmd.CombinedOutput()
	assert.NilError(t, err, "%s", out)

	cmd = exec.Command(bin)
	cmd.Dir = tmp

	out, err = cmd.CombinedOutput()
	assert.NilError(t, err, "%s", out)
}
