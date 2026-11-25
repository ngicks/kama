package jsonschema

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
)

const stubsFilePath = "api/api.kama_jsonschema.kama_validate.go"

// TestStubs writes the stubs of the methods the decoders call into the
// file of the validator generator, under its mark: Validate for every type
// carrying //kama:validate and ValidateShallow for those decoding
// validated values.
func TestStubs(t *testing.T) {
	f := procFs(t, map[string]string{"api/schema.json": `{
		"title": "pet", "type": "object",
		"properties": {"tag": {"type": "string", "enum": ["a", "b"]}}
	}`})

	runProc(t, f, procParam("api/schema.json", "", "", ""), gentest.Now())

	got, err := fs.ReadFile(f.FS(), stubsFilePath)
	assert.NilError(t, err)

	for _, want := range []string{
		"//kama:generated kama:validate\nfunc (v Pet) Validate(stopOnFirstError bool) error {",
		"//kama:generated kama:validate\nfunc (v Pet) ValidateShallow(stopOnFirstError bool) error {",
		"func (v PetTag) Validate(stopOnFirstError bool) error {",
		"func (v PetTag) ValidateShallow(stopOnFirstError bool) error {",
		`return errors.New("Pet.Validate is a stub; run validate.generate over the package")`,
	} {
		assert.Assert(t, strings.Contains(string(got), want), "want %q in\n%s", want, got)
	}
}

// TestStubsReplaceStaleMethods keeps the file of the validator generator
// while the declarations stay those of the existing file, and replaces it
// by stubs once they change, since its methods may refer to what the run
// drops.
func TestStubsReplaceStaleMethods(t *testing.T) {
	const methods = "package api\n\n//kama:generated kama:validate\n" +
		"func (v Pet) Validate(stopOnFirstError bool) error { return nil }\n"

	f := procFs(t, map[string]string{"api/schema.json": procSchema})

	runProc(t, f, procParam("api/schema.json", "", "", ""), gentest.Now())
	assert.NilError(t, f.Write(context.Background(), stubsFilePath, []byte(methods)))

	runProc(t, f, procParam("api/schema.json", "", "", ""), gentest.Now().Add(time.Hour))

	got, err := fs.ReadFile(f.FS(), stubsFilePath)
	assert.NilError(t, err)
	assert.Equal(t, string(got), methods)

	assert.NilError(t, f.Write(context.Background(), "api/schema.json", []byte(
		strings.Replace(procSchema, `"required"`, `"description": "A pet.", "required"`, 1),
	)))

	runProc(t, f, procParam("api/schema.json", "", "", ""), gentest.Now())

	got, err = fs.ReadFile(f.FS(), stubsFilePath)
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(got), "is a stub"), "%s", got)
}

// TestStubsWithoutValidate writes no stub for decoders calling no
// Validate.
func TestStubsWithoutValidate(t *testing.T) {
	f := procFs(t, map[string]string{"api/schema.json": procSchema})

	param := procParam("api/schema.json", "", "", "")
	param.Validate = und.Defined(false)

	runProc(t, f, param, gentest.Now())

	_, err := fs.Stat(f.FS(), stubsFilePath)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// TestHelperNameConflict rejects a declaration written by hand named like a
// helper the generated decoders call, and one standing in the helpers file.
func TestHelperNameConflict(t *testing.T) {
	for _, files := range []map[string]string{
		{"api/hand.go": "package api\n\nfunc kamaJsonschemaDecodeObject() {}\n"},
		{"api/zz_gen.helper.kama_jsonschema.go": "package api\n\nfunc hand() {}\n"},
	} {
		files["api/schema.json"] = procSchema
		f := procFs(t, files)

		i := newInstance(t, gentest.Now())
		assert.NilError(t, i.Param(procParam("api/schema.json", "", "", "")))
		i.Out(f)

		err := i.Run(context.Background(), &gentest.RecordReporter{})
		assert.ErrorIs(t, err, ErrConflict)
	}
}
