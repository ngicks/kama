package validator

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
)

// generateWithOptionFiles generates from files with the directives and the
// package option files of their packages.
func generateWithOptionFiles(t *testing.T, files map[string]string) (map[string]string, error) {
	t.Helper()

	f, opts := stage(t, files)
	pkgs, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)

	loaded, err := NewLoader(nil).LoadOption(f.FS(), f.Root(), base.SourcePackages(pkgs))
	if err != nil {
		return nil, loadError(err)
	}

	out, warnings, err := Generate(pkgs, testOptions(t, Options{Loaded: loaded}))
	if err != nil {
		return nil, err
	}

	assert.Equal(t, len(warnings), 0, "%v", warnings)

	return byBase(out), nil
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
		check(`validation.Field(requiredComparable(v.Name), "Name")`),
		check(`validation.Field(kamaValidateRequired(v.P != nil, "nil"), "P")`),
		check(`validation.Field(v.validateN(stopOnFirstError), "N")`),
	)
	assertNotContains(t, src, "B)")
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
			yaml: "version: 1\ntypes:\n  A:\n    fields:\n      N:\n        validators:\n" +
				"          - name: required\n            args: '\"unterminated'\n",
			is:  ErrOptionFile,
			msg: "args of validator required",
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
