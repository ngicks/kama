package validator

import (
	"testing"

	"gotest.tools/v3/assert"
)

// TestRequires tests the field and its siblings as they are, a pointer and
// an option included, and locates the errors at the struct.
func TestRequires(t *testing.T) {
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\nimport \"github.com/ngicks/option/opt\"\n\n" +
			"//kama:validate hook=False\ntype A struct {\n" +
			"\t//kama:validate requires(\"b\", \"C\")\n\tA *int `json:\"a\"`\n" +
			"\tB opt.Option[int] `json:\"b,omitzero\"`\n\tC []int\n" +
			"\t//kama:validate requires(\"a\")\n\tD int `json:\"-\"`\n}\n",
	}, "a.kama_validate.go", "A")

	assertContains(t, fn,
		check(`kamaValidateRequires(stopOnFirstError, "a", v.A != nil, []string{"b", "C"},`+
			` v.B.IsSome(), v.C != nil)`),
		check(`kamaValidateRequires(stopOnFirstError, "D", kamaValidateNonZero(v.D),`+
			` []string{"a"}, v.A != nil)`),
	)
	assertNotContains(t, fn, "if v.A != nil {")
}

func TestRequiresErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		is   error
		msgs []string
	}{
		{
			name: "unknown sibling",
			src: "//kama:validate hook=False\ntype A struct {\n" +
				"\t//kama:validate requires(\"b\", \"z\")\n" +
				"\tA int `json:\"a\"`\n\tB int `json:\"b\"`\n\tZ int `json:\"-\"`\n}\n",
			is: ErrUnsupported,
			msgs: []string{
				"//kama:validate requires on field A",
				`no sibling field encodes as the member "z"`,
			},
		},
		{
			name: "sibling without presence",
			src: "//kama:validate hook=False\ntype A struct {\n\t//kama:validate requires(\"b\")\n" +
				"\tA int `json:\"a\"`\n\tB struct{ L []int } `json:\"b\"`\n}\n",
			is:   ErrUnsupported,
			msgs: []string{`sibling "b" has no presence test`, "is not comparable"},
		},
		{
			name: "field without presence",
			src: "//kama:validate hook=False\ntype A struct {\n\t//kama:validate requires(\"b\")\n" +
				"\tA struct{ L []int } `json:\"a\"`\n\tB int `json:\"b\"`\n}\n",
			is:   ErrUnsupported,
			msgs: []string{"//kama:validate requires on field A", "is not comparable"},
		},
		{
			name: "bad args",
			src: "//kama:validate hook=False\ntype A struct {\n\t//kama:validate requires\n" +
				"\tA int\n}\n",
			is:   ErrUnsupported,
			msgs: []string{"want one or more JSON names of sibling fields"},
		},
		{
			name: "on a type",
			src:  "//kama:validate requires(\"a\")\ntype N int\n",
			is:   ErrDirective,
			msgs: []string{
				"//kama:validate requires on type N reads the siblings of a struct field;" +
					" write it on a field",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"a.go": "package x\n\n" + tc.src}, testRun{})
			assert.ErrorIs(t, err, tc.is)

			for _, msg := range tc.msgs {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}
