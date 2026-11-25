package validator

import (
	"context"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
)

// generateWithOptionFiles generates from files with the directives and the
// package option files of their packages, and fails on a warning.
func generateWithOptionFiles(t *testing.T, files map[string]string) (map[string]string, error) {
	t.Helper()

	out, warnings, err := generateWithWarnings(t, files, nil)
	if err != nil {
		return nil, err
	}

	assert.Equal(t, len(warnings), 0, "%v", warnings)

	return out, nil
}

// generateWithWarnings generates from files with the directives, the
// package option files of their packages and the option file
// optionFile finds relative to the directory of the test. It returns the
// warnings of Generate.
func generateWithWarnings(
	t *testing.T,
	files map[string]string,
	optionFile fileloader.Input,
) (map[string]string, []error, error) {
	t.Helper()

	f, opts := stage(t, files)
	pkgs, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	loaded, err := NewLoader(optionFile).LoadOption(f.FS(), f.Root(), base.SourcePackages(pkgs))
	if err != nil {
		return nil, nil, loadError(err)
	}

	out, warnings, err := runGenerate(t, f, pkgs, testRun{Loaded: loaded})
	if err != nil {
		return nil, nil, err
	}

	return gentest.ByBase(out), warnings, nil
}

// optionFileOf finds the option file validate.yaml in the directory of t.
func optionFileOf(t *testing.T) fileloader.Input {
	return fileloader.PathInput{
		Base:    gentest.InlineDir(t) + "/validate",
		Formats: []fileloader.Format{fileloader.FormatYaml},
	}
}

func TestPackageOptionFile(t *testing.T) {
	out, err := generateWithOptionFiles(t, map[string]string{
		"a.go": "package x\n\ntype A struct {\n\tName string\n\tP    *int\n\tN    int\n}\n\n" +
			"func (A) validateN(bool) error { return nil }\n\ntype B struct{ N int }\n",
		"kama_validate.yaml": `version: 1
types:
  A:
    hook: false
    fields:
      Name:
        validators:
          - name: required
      P:
        validators:
          - name: required
            args: ""
      N:
        hook: true
  B:
    ignore: true
`,
	})
	assert.NilError(t, err)

	src := out["a.kama_validate.go"]
	assertContains(t, src,
		"package_option_file: ",
		check(`validator.Field(requiredComparable(v.Name), "Name")`),
		check(`validator.Field(kamaValidateRequired(v.P != nil, "nil"), "P")`),
		check(`validator.Field(v.validateN(stopOnFirstError), "N")`),
	)
	assertNotContains(t, src, "B)")
}

// TestOptionFileTypeValidators gives a type validators of its own, as its
// directive would, and warns of an unknown key of such a validator.
func TestOptionFileTypeValidators(t *testing.T) {
	out, warnings, err := generateWithWarnings(t, map[string]string{
		"a.go": "package x\n\ntype N int\n",
		"kama_validate.yaml": "version: 1\ntypes:\n  N:\n    validators:\n" +
			"      - name: cmp\n        args: \">0\"\n        ww: 1\n",
	}, nil)
	assert.NilError(t, err)
	assertContains(t, gentest.Funcs(t, out["a.kama_validate.go"])["N.Validate"],
		check(`kamaValidateCmp(v, ">", 0)`),
	)
	assert.Equal(t, len(warnings), 1)
	assert.ErrorIs(t, warnings[0], genopt.ErrUnknownKey)
	assert.ErrorContains(t, warnings[0], "types.N.validators.0.ww")

	_, err = generateWithOptionFiles(t, map[string]string{
		"a.go": "package x\n\ntype N int\n",
		"kama_validate.yaml": "version: 1\ntypes:\n  N:\n    validators:\n" +
			"      - name: cmp\n        args: '>\"1'\n",
	})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorContains(t, err, "types.N.validators.0: package option file cmp option on type N")
}

func TestOptionFileErrors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		is   error
		msg  string
	}{
		{
			name: "bad args",
			yaml: "version: 1\ntypes:\n  A:\n    hook: false\n    fields:\n      N:\n" +
				"        validators:\n          - name: cmp\n            args: '>\"1'\n",
			is:  ErrUnsupported,
			msg: "types.A.fields.N.validators.0: package option file cmp option on field N",
		},
		{
			name: "no name",
			yaml: "version: 1\ntypes:\n  A:\n    fields:\n      N:\n        validators:\n" +
				"          - args: x\n",
			is:  ErrOptionFile,
			msg: "validator without name",
		},
		{
			name: "twice",
			yaml: "version: 1\ntypes:\n  A:\n    hook: false\n    fields:\n" +
				"      N:\n        validators:\n" +
				"          - name: required\n          - name: required\n",
			is:  ErrDirective,
			msg: "validator required given twice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generateWithOptionFiles(t, map[string]string{
				"a.go":               "package x\n\ntype A struct{ N int }\n",
				"kama_validate.yaml": tc.yaml,
			})
			assert.ErrorIs(t, err, tc.is)
			assert.ErrorContains(t, err, tc.msg)
		})
	}
}

// TestOptionFileUnknownKeys warns of each unknown key of an option file
// entry and applies the rest of the entry.
func TestOptionFileUnknownKeys(t *testing.T) {
	out, warnings, err := generateWithWarnings(t, map[string]string{
		"a.go": "package x\n\ntype A struct{ N int }\n",
		"kama_validate.yaml": "version: 1\nzz: 1\ntypes:\n  A:\n    hook: false\n    yy: 1\n" +
			"    fields:\n      N:\n        xx: 1\n        validators:\n" +
			"          - name: required\n            ww: 1\n",
	}, nil)
	assert.NilError(t, err)
	assertContains(t, out["a.kama_validate.go"],
		check(`validator.Field(requiredComparable(v.N), "N")`),
	)

	msgs := make([]string, len(warnings))
	for i, w := range warnings {
		assert.ErrorIs(t, w, genopt.ErrUnknownKey)
		msgs[i] = w.Error()[strings.LastIndex(w.Error(), ": ")+2:]
	}

	assert.DeepEqual(t, msgs, []string{
		"zz",
		"types.A.yy",
		"types.A.fields.N.validators.0.ww",
		"types.A.fields.N.xx",
	})
}

// TestSubDirective rejects //kama:validate:<sub> and shows the arg of
// //kama:validate standing for it.
func TestSubDirective(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "type hook",
			src:  "//kama:validate\n//kama:validate:hook off\ntype A struct{ N int }\n",
			want: "a.go:4:1: //kama:validate:hook; validators and hook are args of" +
				" //kama:validate, as in //kama:validate hook=off",
		},
		{
			name: "type validator",
			src:  "//kama:validate:required\ntype A struct{ N int }\n",
			want: "as in //kama:validate required",
		},
		{
			name: "free-floating",
			src:  "//kama:validate:hook on\n\ntype A struct{ N int }\n",
			want: "as in //kama:validate hook=on",
		},
		{
			name: "comparisons",
			src: "//kama:validate\ntype A struct {\n" +
				"\t//kama:validate:length >=1 <=10\n\tN string\n}\n",
			want: `as in //kama:validate length=">=1 <=10"`,
		},
		{
			name: "quoted pattern",
			src: "//kama:validate\ntype A struct {\n" +
				"\t//kama:validate:regexp \"^a b$\"\n\tN string\n}\n",
			want: `as in //kama:validate regexp="^a b$"`,
		},
		{
			name: "quoted literals",
			src: "//kama:validate\ntype A struct {\n" +
				"\t//kama:validate:const \"red\" \"dark blue\"\n\tN string\n}\n",
			want: "as in //kama:validate const=`\"red\" \"dark blue\"`",
		},
		{
			name: "quoted comparison",
			src: "//kama:validate\ntype A struct {\n" +
				"\t//kama:validate:cmp >\"a b\"\n\tN string\n}\n",
			want: "as in //kama:validate cmp=`>\"a b\"`",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"a.go": "package x\n\n" + tc.src}, testRun{})
			assert.ErrorIs(t, err, ErrDirective)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// TestOptionFileAllTypes marks every type of a package by all_types, which
// skips the types that get no Validate as //kama:validate package does.
func TestOptionFileAllTypes(t *testing.T) {
	out, warnings, err := generateWithWarnings(t, map[string]string{
		"a.go": "package x\n\ntype A struct{}\n\ntype L []int\n\ntype ID int\n",
		"validate.yaml": "version: 1\npackages:\n  " + modulePath(t) + ":\n" +
			"    all_types: true\n",
	}, optionFileOf(t))
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)

	src := out["a.kama_validate.go"]
	assertContains(t, src, "func (v A) Validate(", "func (v L) Validate(")
	assertNotContains(t, src, "func (v ID)")
}

// TestOptionFileDirective reads the package option file a free-floating
// //kama:option-file names instead of kama_validate.yaml.
func TestOptionFileDirective(t *testing.T) {
	out, err := generateWithOptionFiles(t, map[string]string{
		"a.go": "//kama:option-file validate=custom.yaml\n\npackage x\n\n" +
			"type A struct{ N int }\n\ntype B struct{ N int }\n",
		"custom.yaml": "version: 1\ntypes:\n  A:\n    hook: false\n    fields:\n" +
			"      N:\n        validators:\n          - name: required\n",
		"kama_validate.yaml": "version: 1\ntypes:\n  B:\n    hook: false\n",
	})
	assert.NilError(t, err)

	src := out["a.kama_validate.go"]
	assertContains(t, src,
		"package_option_file: ",
		"custom.yaml",
		check(`validator.Field(requiredComparable(v.N), "N")`),
	)
	assertNotContains(t, src, "func (v B)")
}

// TestOptionFileOverride replaces the directives of a type by its entry in
// the option file, and that entry by the entry in the package option file,
// with a warning for each.
func TestOptionFileOverride(t *testing.T) {
	out, warnings, err := generateWithWarnings(t, map[string]string{
		"a.go": "package x\n\n//kama:validate\ntype A struct {\n" +
			"\t//kama:validate required\n\tN int\n}\n\n" +
			"//kama:validate\n//kama:validate hook=off\ntype B struct {\n" +
			"\t//kama:validate required\n\tN int\n}\n",
		"validate.yaml": "version: 1\npackages:\n  " + modulePath(t) + ":\n" +
			"    types:\n      A:\n        hook: false\n        fields:\n" +
			"          N:\n            validators:\n              - name: zero\n" +
			"      B:\n        hook: false\n",
		"kama_validate.yaml": "version: 1\ntypes:\n  B:\n    hook: false\n    fields:\n" +
			"      N:\n        validators:\n          - name: cmp\n            args: '>0'\n",
	}, optionFileOf(t))
	assert.NilError(t, err)

	src := out["a.kama_validate.go"]
	assertContains(t, src,
		check(`validator.Field(kamaValidateZeroComparable(v.N), "N")`),
		check(`validator.Field(kamaValidateCmp(v.N, ">", 0), "N")`),
	)
	assertNotContains(t, src, "requiredComparable", "validateN")

	assert.Equal(t, len(warnings), 3, "%v", warnings)

	var msgs []string

	for _, w := range warnings {
		assert.ErrorIs(t, w, genopt.ErrOverridden)
		msgs = append(msgs, w.Error())
	}

	joined := strings.Join(msgs, "\n")
	assertContains(t, joined, modulePath(t)+".A", modulePath(t)+".B")
}
