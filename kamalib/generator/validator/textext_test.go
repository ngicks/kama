package validator

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
)

const textExtSub = `package sub

type Box[T any] struct {
	v  T
	ok bool
}

func (b Box[T]) Has() bool { return b.ok }

func (b Box[T]) Get() T { return b.v }
`

const textExtA = `package x

import "%[1]s/sub"

//kama:validate
//kama:validate hook=False
type A struct {
	//kama:validate required, v=length(le=3)
	S sub.Box[string]
}
`

// textExtFile is an extension file describing sub.Box, with the type key
// and the body of the holder and of the validate section.
func textExtFile(t *testing.T, holder, validate string) string {
	key := `"` + modulePath(t) + `/sub.Box"`

	return "version = 1\n\n" +
		"[imports]\nopt = \"github.com/ngicks/option/opt\"\nunused = \"example.com/never/used\"\n\n" +
		"[helpers.has]\ndecl = '''func {{.Name}}(ok bool) bool { " +
		"return {{std \"unicode/utf8\"}}.RuneLen('a') == 1 && ok }'''\n\n" +
		"[types." + key + ".holder]\n" + holder + "\n\n" +
		"[types." + key + ".generator.validate.present]\n" + validate + "\n"
}

const textExtHolder = `role = "v"
type = "T"
guard = "{{.V}}.Has()"
value = "{{.V}}.Get()"`

// runTextExt generates the files with the extension file ext.toml and
// returns the generated files by base name, the warnings and the error.
func runTextExt(
	t *testing.T,
	files map[string]string,
	reg *ext.Registry,
) (map[string]string, []error, error) {
	t.Helper()

	f, opts := stage(t, files)
	pkgs, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	out, warnings, err := runGenerate(t, f, pkgs, testRun{
		Registry:       reg,
		ExtensionFiles: []fileloader.PathInput{{Base: gentest.InlineDir(t) + "/ext"}}})
	if err != nil {
		return nil, warnings, err
	}

	gentest.Write(t, f, out)

	if err := gentest.Build(f, opts); err != nil {
		return gentest.ByBase(out), warnings, err
	}

	return gentest.ByBase(out), warnings, nil
}

func textExtFiles(t *testing.T, ext string) map[string]string {
	return map[string]string{
		"a.go":       fmt.Sprintf(textExtA, modulePath(t)),
		"sub/sub.go": textExtSub,
		"ext.toml":   ext,
	}
}

// TestTextExtensionImports imports what a template names through std, pkg
// and helper, and nothing it declares without using.
func TestTextExtensionImports(t *testing.T) {
	files := textExtFiles(t, textExtFile(
		t,
		textExtHolder,
		`cond = '{{helper "has"}}({{pkg "opt"}}.Some({{.V}}.Has()).Value()) && `+
			`{{std "slices"}}.Contains([]bool{true}, {{.V}}.Has())'`,
	))

	out, warnings, err := runTextExt(t, files, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)

	assertContains(
		t,
		out["a.kama_validate.go"],
		`"slices"`,
		`"github.com/ngicks/option/opt"`,
		"kamaValidateExtHas(opt.Some(v.S.Has()).Value()) && slices.Contains([]bool{true}, v.S.Has())",
		"if v.S.Has() {",
		"kamaValidateLength(len(v.S.Get()), \"<=\", 3)",
	)
	assertContains(t, out["zz_gen.helper.kama_validate.go"], `"unicode/utf8"`,
		"func kamaValidateExtHas(ok bool) bool {")

	for _, src := range out {
		assertNotContains(t, src, "example.com/never/used")
	}
}

// TestTextExtensionPrecedence puts a type the extension file describes
// before a registered support of the same type: the role of the file
// applies.
func TestTextExtensionPrecedence(t *testing.T) {
	reg := ext.DefaultRegistry()
	reg.RegisterTypeSupport(bagSupport{path: modulePath(t) + "/sub"})

	files := textExtFiles(t, textExtFile(t, textExtHolder, `cond = "{{.V}}.Has()"`))

	_, warnings, err := runTextExt(t, files, reg)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)

	// Without the file, the support of reg names the value t.
	_, err = generate(t, map[string]string{
		"a.go":       fmt.Sprintf(textExtA, modulePath(t)),
		"sub/sub.go": textExtSub,
	}, testRun{Registry: reg})
	assert.ErrorContains(t, err, "offers no role v")
}

func TestTextExtensionErrors(t *testing.T) {
	for _, tc := range []struct {
		name, holder, cond, want string
	}{
		{
			name:   "bad template",
			holder: textExtHolder,
			cond:   `cond = "{{.V"`,
			want:   "ext.toml: types.\"" + "%[1]s/sub.Box\".generator.validate.present.cond: ",
		},
		{
			name:   "undeclared package",
			holder: textExtHolder,
			cond:   `cond = '{{pkg "nope"}}.Ok({{.V}})'`,
			want:   `pkg "nope": imports declares no such name`,
		},
		{
			name:   "no standard package",
			holder: textExtHolder,
			cond:   `cond = '{{std "example.com/x"}}.Ok({{.V}})'`,
			want:   `std "example.com/x": no standard library package`,
		},
		{
			name:   "undeclared helper",
			holder: textExtHolder,
			cond:   `cond = '{{helper "nope"}}({{.V}})'`,
			want:   `helper "nope": helpers declares no such name`,
		},
		{
			name:   "no type parameter",
			holder: "role = \"v\"\ntype = \"U\"\nguard = \"true\"\nvalue = \"{{.V}}\"",
			cond:   `cond = "true"`,
			want:   "holder.type: Box has no type parameter U",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := textExtFiles(t, textExtFile(t, tc.holder, tc.cond))

			_, _, err := runTextExt(t, files, nil)
			assert.ErrorIs(t, err, genext.ErrTextFile)
			assert.ErrorContains(t, err, strings.ReplaceAll(tc.want, "%[1]s", modulePath(t)))
		})
	}
}

// TestTextExtensionUnknownType reports a type the package of the key does
// not declare.
func TestTextExtensionUnknownType(t *testing.T) {
	files := textExtFiles(t, textExtFile(t, textExtHolder, `cond = "{{.V}}.Has()"`))
	files["ext.toml"] += "\n[types.\"" + modulePath(
		t,
	) + "/sub.Nope\".holder]\n" + textExtHolder + "\n"

	_, _, err := runTextExt(t, files, nil)
	assert.ErrorIs(t, err, genext.ErrTextFile)
	assert.ErrorContains(t, err, "package "+modulePath(t)+"/sub declares no type Nope")
}

// TestTextExtensionUnknownKey warns about a key that names nothing and
// generates.
func TestTextExtensionUnknownKey(t *testing.T) {
	files := textExtFiles(
		t,
		textExtFile(t, textExtHolder+"\nguardd = \"x\"", `cond = "{{.V}}.Has()"`),
	)
	files["ext.toml"] += "\n[types.\"" + modulePath(t) + "/sub.Box\".generator.clone]\nx = 1\n"

	_, warnings, err := runTextExt(t, files, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 2, "%v", warnings)
	assert.ErrorContains(
		t,
		warnings[0],
		"ext.toml: types.\""+modulePath(t)+"/sub.Box\".holder.guardd: unknown key",
	)
	assert.ErrorContains(
		t,
		warnings[1],
		"generator.clone: no generator of this program is named clone",
	)
}

// TestTextExtensionCompareOperand rejects an operand of the kind the
// section of the file does not take.
func TestTextExtensionCompareOperand(t *testing.T) {
	files := textExtFiles(t, textExtFile(t, textExtHolder, `cond = "{{.V}}.Has()"`))
	files["ext.toml"] += "\n[types.\"" + modulePath(
		t,
	) + "/sub.Box\".generator.validate.compare]\n" +
		"ops = [\"==\"]\noperand = \"string\"\ncond = \"{{.V}}.Get() {{.Op}} {{.Operand}}\"\n"
	files["a.go"] = strings.Replace(files["a.go"], "required, v=length(le=3)", "cmp(eq=1)", 1)

	_, _, err := runTextExt(t, files, nil)
	assert.ErrorIs(t, err, ext.ErrArgs)
	assert.ErrorIs(t, err, ext.ErrClaimed)
	assert.ErrorContains(t, err, "compares with a quoted string, got 1")
}

// TestTextExtensionMissingMethod shows that a template calling a method
// the type lacks generates and fails to compile: the generator does not
// type-check the code a template writes.
func TestTextExtensionMissingMethod(t *testing.T) {
	files := textExtFiles(t, textExtFile(t, textExtHolder, `cond = "{{.V}}.IsPresent()"`))

	_, _, err := runTextExt(t, files, nil)
	assert.ErrorContains(t, err, "IsPresent")
}
