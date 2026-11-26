package textmod

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// stage writes files into an overlay as gentest.Stage does, but byte for
// byte, so that a file need not be gofmt-clean, and loads their packages.
func stage(t *testing.T, files map[string]string) (*overlayfs.Fs, Input) {
	t.Helper()

	f, opts := gentest.Stage(t, files)

	for name, src := range files {
		assert.NilError(t, f.WriteFormatted(path.Join(gentest.InlineDir(t), name), []byte(src)))
	}

	pkgs, err := gopackages.Load(t.Context(), f, opts)
	assert.NilError(t, err)

	return f, Input{Pkgs: base.SourcePackages(pkgs), Root: f.Root()}
}

// runGen runs Generator on in and returns the files it yields keyed by their
// path relative to the inline directory of t.
func runGen(t *testing.T, f *overlayfs.Fs, in Input) (map[string]string, []error, error) {
	t.Helper()

	files, warnings, err := gentest.Run(
		context.Background(),
		Generator(),
		base.DefaultRegistry(),
		gentest.Now(),
		in,
		base.Scope{},
		f,
	)

	out := map[string]string{}

	for _, file := range files {
		rel, rerr := filepath.Rel(filepath.Join(f.Root(), gentest.InlineDir(t)), file.Path)
		assert.NilError(t, rerr)

		out[filepath.ToSlash(rel)] = string(file.Content)
	}

	return out, warnings, err
}

const kindsSrc = "package p\n\nimport \"time\"\n\n" +
	"//kama:textmod untypedInt\nconst   UntypedInt  =   3 // keep   spacing\n\n" +
	"//kama:textmod typedInt\nconst TypedInt int8 = -5\n\n" +
	"//kama:textmod dur\nvar Dur time.Duration = 5\n\n" +
	"//kama:textmod big\nconst Big = 1180591620717411303424\n\n" +
	"//kama:textmod flt\nconst Flt = 1.5\n\n" +
	"//kama:textmod typedFlt\nvar TypedFlt float64 = 2\n\n" +
	"//kama:textmod fltToInt\nconst FltToInt = 1.5\n\n" +
	"type Name string\n\n" +
	"//kama:textmod typedStr\nconst TypedStr Name = \"a\"\n\n" +
	"//kama:textmod typedBool\nvar TypedBool bool = false\n\n" +
	"//kama:textmod str\nconst Str = \"hello\"\n\n" +
	"//kama:textmod raw\nconst Raw = `line1\nline2`\n\n" +
	"//kama:textmod rawToInterp\nconst RawToInterp = `x`\n\n" +
	"//kama:textmod b\nvar B = true\n\n" +
	"const (\n" +
	"\t//kama:textmod grpDoc\n\tGrpDoc = \"a\"\n" +
	"\tGrpTrail = \"b\" //kama:textmod grpTrail\n" +
	"\tUntouched = \"c\"\n)\n\n" +
	"//kama:textmod ver, format=\"ver\"\nconst Ver = \"v0.0.0-devel\"\n\n" +
	"//kama:textmod verDevel, format=\"ver\"\nconst VerDevel = \"v1.2.3\"\n\n" +
	"//kama:textmod at, format=\"date-time\"\nconst At = \"2026-01-02T03:04:05Z\"\n\n" +
	"//kama:textmod \"app-version\"\nconst AppVersion = \"1\"\n\n" +
	"//kama:textmod neg\nconst Neg = -7\n\n" +
	"//kama:textmod noMod\nconst NoMod = 1\n\n" +
	"func f() {\n\t//kama:textmod local\n\tconst local = 10\n\t_ = local\n}\n"

var kindsMods = map[string]string{
	"untypedInt":  "{{ add .Value 1 }}",
	"typedInt":    "{{ sub .Value 1 }}",
	"dur":         "{{ mul .Value 2 }}",
	"big":         "{{ add .Value 1 }}",
	"flt":         "{{ add .Value 0.25 }}",
	"typedFlt":    "{{ add .Value 0.5 }}",
	"fltToInt":    "{{ add .Value 0.5 }}",
	"typedStr":    "{{ .Value }}{{ .Value }}",
	"typedBool":   "true",
	"str":         "{{ upper .Value }}",
	"raw":         "{{ .Value }}-{{ .Name }}\n",
	"rawToInterp": "a`b",
	"b":           "{{ not .Value }}",
	"grpDoc":      "{{ .Value }}{{ .Vars.suffix }}",
	"grpTrail":    "{{ .Id }}",
	"ver":         `{{ .Value | withPre "" }}`,
	"verDevel":    `{{ .Value | withCore 1 3 0 | withPre "devel" }}`,
	"at":          `{{ .Value | addDuration "36h" | rfc3339 }}`,
	"app-version": "{{ .Vars.version }}",
	"neg":         "{{ sub 0 .Value }}",
	"local":       "{{ add .Value 1 }}",
}

func TestGenerateKinds(t *testing.T) {
	f, in := stage(t, map[string]string{"p/a.go": kindsSrc})
	in.Mods = kindsMods
	in.Vars = map[string]any{"suffix": "!", "version": "2.0.0"}

	files, warnings, err := runGen(t, f, in)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)

	want := strings.NewReplacer(
		"UntypedInt  =   3 ", "UntypedInt  =   4 ",
		"TypedInt int8 = -5", "TypedInt int8 = -6",
		"Dur time.Duration = 5", "Dur time.Duration = 10",
		"1180591620717411303424", "1180591620717411303425",
		"Flt = 1.5", "Flt = 1.75",
		"TypedFlt float64 = 2", "TypedFlt float64 = 2.5",
		"FltToInt = 1.5", "FltToInt = 2.0",
		`TypedStr Name = "a"`, `TypedStr Name = "aa"`,
		"TypedBool bool = false", "TypedBool bool = true",
		`Str = "hello"`, `Str = "HELLO"`,
		"`line1\nline2`", "`line1\nline2-Raw`",
		"RawToInterp = `x`", "RawToInterp = \"a`b\"",
		"B = true", "B = false",
		`GrpDoc = "a"`, `GrpDoc = "a!"`,
		`GrpTrail = "b"`, `GrpTrail = "grpTrail"`,
		`"v0.0.0-devel"`, `"v0.0.0"`,
		`"v1.2.3"`, `"v1.3.0-devel"`,
		`"2026-01-02T03:04:05Z"`, `"2026-01-03T15:04:05Z"`,
		`AppVersion = "1"`, `AppVersion = "2.0.0"`,
		"Neg = -7", "Neg = 7",
		"local = 10", "local = 11",
	).Replace(kindsSrc)

	assert.DeepEqual(t, files, map[string]string{"p/a.go": want})
}

// TestGenerateIdempotent runs mods setting values from vars on their own
// output, which changes nothing. The packages stay as they were loaded
// before the first run, so the second run takes its positions from the
// edited content.
func TestGenerateIdempotent(t *testing.T) {
	const src = "package p\n\n" +
		"//kama:textmod ver, format=\"ver\"\nconst Ver = \"v0.0.0-devel\"\n\n" +
		"//kama:textmod help\nconst Help = `usage:\n# begin\nold\n# end\n`\n\n" +
		"//kama:textmod n\nconst N = 0x10\n"

	f, in := stage(t, map[string]string{"p/a.go": src})
	in.Mods = map[string]string{
		"ver":  "{{ .Vars.version }}",
		"help": `{{ .Value | replaceBetween "# begin" "# end" .Vars.body }}`,
		"n":    "{{ .Value }}",
	}
	in.Vars = map[string]any{"version": "v1.10.0", "body": "  new line\n"}

	files, _, err := runGen(t, f, in)
	assert.NilError(t, err)

	want := strings.NewReplacer(
		`"v0.0.0-devel"`, `"v1.10.0"`,
		"# begin\nold\n# end", "# begin\n  new line\n# end",
	).Replace(src)
	assert.DeepEqual(t, files, map[string]string{"p/a.go": want})

	assert.NilError(t, f.WriteFormatted(path.Join(gentest.InlineDir(t), "p/a.go"), []byte(want)))

	files, _, err = runGen(t, f, in)
	assert.NilError(t, err)
	assert.Equal(t, len(files), 0, "%v", files)
}

func TestGenerateErrors(t *testing.T) {
	const src = "package p\n\n" +
		"//kama:textmod names\nvar A, B = 1, 2\n\n" +
		"const (\n\t//kama:textmod iotaC\n\tC0 = iota\n\t//kama:textmod cont\n\tC1\n)\n\n" +
		"//kama:textmod noInit\nvar NoInit int\n\n" +
		"//kama:textmod call\nvar Call = len(\"x\")\n\n" +
		"//kama:textmod bin\nconst Bin = 1 + 2\n\n" +
		"//kama:textmod comp\nvar Comp = []int{1}\n\n" +
		"//kama:textmod ident\nconst Ident = C0\n\n" +
		"//kama:textmod r\nconst R = 'a'\n\n" +
		"//kama:textmod fmtInt, format=\"ver\"\nconst FmtInt = 1\n\n" +
		"//kama:textmod fmtUnknown, format=\"semver\"\nconst FmtUnknown = \"x\"\n\n" +
		"//kama:textmod onType\ntype T int\n\n" +
		"//kama:textmod onGroup\nconst (\n\tG1 = 1\n\tG2 = 2\n)\n\n" +
		"//kama:textmod app-version\nconst Hyphen = \"x\"\n\n" +
		"//kama:textmod twice\nconst Twice = 1 //kama:textmod twice2\n\n" +
		"//kama:textmod small\nvar Small int8 = 1\n\n" +
		"//kama:textmod badInt\nconst BadInt = 1\n\n" +
		"//kama:textmod badBool\nconst BadBool = true\n\n" +
		"//kama:textmod badVer, format=\"ver\"\nconst BadVer = \"v1.0.0\"\n\n" +
		"//kama:textmod badAt, format=\"date-time\"\nconst BadAt = \"2026-01-02T03:04:05Z\"\n\n" +
		"//kama:textmod missing\nconst Missing = \"x\"\n\n" +
		"//kama:textmod unknownKey, color=\"red\"\nconst UnknownKey = 1\n\n" +
		"//kama:textmod hexFloat\nconst HexFloat = 1.5\n\n" +
		"//kama:textmod badFloat\nconst BadFloat = 1.5\n"

	const gen = "// Code generated by hand. DO NOT EDIT.\n\npackage p\n\n" +
		"//kama:textmod inGenerated\nconst InGenerated = 1\n"

	const marked = "package p\n\n//kama:generated kama:partial\nfunc g() {}\n\n" +
		"//kama:textmod inMarked\nconst InMarked = 1\n"

	f, in := stage(t, map[string]string{"p/a.go": src, "p/gen.go": gen, "p/marked.go": marked})
	in.Mods = map[string]string{
		"small":    "{{ 200 }}",
		"badInt":   "x",
		"badBool":  "yes",
		"badVer":   "nope",
		"badAt":    "{{ .Value }}",
		"missing":  "{{ .Vars.nope }}",
		"parseErr": "{{ .Value",
		"hexFloat": "0x2",
		"badFloat": "abc",
	}

	_, warnings, err := runGen(t, f, in)
	assert.Assert(t, err != nil)

	for _, want := range []string{
		"p/a.go:3:1: A: the spec declares 2 names",
		"p/a.go:7:2: C0: the value is iota",
		"p/a.go:9:2: C1: the spec has no value",
		"p/a.go:13:1: NoInit: the spec has no value",
		"p/a.go:16:1: Call: the value is a call",
		"p/a.go:19:1: Bin: the value is a binary expression",
		"p/a.go:22:1: Comp: the value is a composite literal",
		"p/a.go:25:1: Ident: the value is the identifier C0",
		"R: a rune literal is not supported",
		`format "ver" on a value of kind int`,
		`unknown format "semver"`,
		"p/a.go:37:1: the directive is on no var or const spec",
		"the directive is on a group of 2 specs",
		`quote an id holding a -, as "app-version"`,
		"the spec is tagged at",
		`"small" at`, "overflows int8",
		`"badInt" at`, "not a Go int literal",
		`"badBool" at`, "is not true or false",
		`"badVer" at`, "is not a version",
		`"badAt" at`, "end the template with rfc3339",
		`"missing" at`, `map has no entry for key "nope"`,
		`"parseErr"`,
		`"hexFloat" at`, "an integer literal on a float spec spelling no type",
		`"badFloat" at`, "not a Go float literal",
		"p/gen.go:5:1: the file is the output of a generator",
		"p/marked.go:6:1: the file is the output of a generator",
	} {
		assert.Assert(t, strings.Contains(err.Error(), want), "%q in\n%v", want, err)
	}

	assert.Assert(t, errors.Is(err, ErrDirective))
	assert.Assert(t, errors.Is(err, ErrTarget))
	assert.Assert(t, errors.Is(err, ErrMod))

	// parseErr does not parse, and no spec carries it.
	assert.Equal(t, len(warnings), 2, "%v", warnings)
	assert.Assert(t, errors.Is(warnings[0], ErrUnknownKey))
	assert.ErrorContains(t, warnings[0], "p/a.go:70:1: color")
	assert.Assert(t, errors.Is(warnings[1], ErrUnusedMod))
}

// TestGenerateDuplicateIds tags one id in two packages and two files, in
// an order that differs from the order of the files of the loaded
// packages, and gives a mod no spec carries.
func TestGenerateDuplicateIds(t *testing.T) {
	const tagged = "//kama:textmod dup\nconst V = 1\n"

	f, in := stage(t, map[string]string{
		"pb/a.go": "package pb\n\n" + tagged,
		"pa/y.go": "package pa\n\n" + tagged,
		"pa/x.go": "package pa\n\nconst W = 0\n\n" + strings.Replace(tagged, "V", "X", 1),
	})
	in.Mods = map[string]string{"dup": "{{ add .Value 1 }}", "unused": "1"}

	files, warnings, err := runGen(t, f, in)
	assert.NilError(t, err)
	assert.DeepEqual(t, files, map[string]string{
		"pa/x.go": "package pa\n\nconst W = 0\n\n//kama:textmod dup\nconst X = 2\n",
	})

	assert.Equal(t, len(warnings), 2, "%v", warnings)
	assert.Assert(t, errors.Is(warnings[0], ErrDuplicateId))

	dir := gentest.InlineDir(t)
	assert.ErrorContains(t, warnings[0], strings.Join([]string{
		dir + "/pa/x.go:5:1", dir + "/pa/y.go:3:1", dir + "/pb/a.go:3:1",
	}, ", "))
	assert.Assert(t, errors.Is(warnings[1], ErrUnusedMod))
	assert.ErrorContains(t, warnings[1], `"unused"`)
}
