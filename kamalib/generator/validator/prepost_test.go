package validator

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
)

const prePostFuncs = "func pre(A, bool) error { return nil }\n\n" +
	"func post(A, bool) error { return nil }\n"

func TestPrePost(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// typeName is the type whose Validate is checked.
		typeName string
		want     []string
	}{
		{
			name: "struct",
			src: "//kama:validate pre=\"pre\", post=\"post\", hook=False\ntype A struct {\n" +
				"\t//kama:validate cmp(gt=0)\n\tN int\n}\n\n" + prePostFuncs,
			typeName: "A",
			want: []string{
				"func (v *A) Validate(stopOnFirstError bool) (err error) {",
				"defer func() { err = validator.Join(err, " +
					"validator.Root(post((*v), stopOnFirstError))) }()",
				check("validator.Root(pre((*v), stopOnFirstError))"),
				check(`validator.Field(kamaValidateCmp(v.N, ">", 0), "N")`),
			},
		},
		{
			// A function taking a pointer gets the receiver of Validate itself.
			name: "struct by pointer",
			src: "//kama:validate pre=\"pre\", post=\"post\", hook=False\ntype A struct{ N int }\n\n" +
				"func pre(*A, bool) error { return nil }\n\nfunc post(*A, bool) error { return nil }\n",
			typeName: "A",
			want: []string{
				"defer func() { err = validator.Join(err, validator.Root(post(v, stopOnFirstError))) }()",
				check("validator.Root(pre(v, stopOnFirstError))"),
			},
		},
		{
			name:     "named non-struct type with no validator",
			src:      "//kama:validate pre=\"pre\", post=\"post\"\ntype A int\n\n" + prePostFuncs,
			typeName: "A",
			want: []string{
				"(err error) {",
				"defer func() { err = validator.Join(err, validator.Root(post(v, stopOnFirstError))) }()",
				check("validator.Root(pre(v, stopOnFirstError))"),
			},
		},
		{
			name:     "post alone",
			src:      "//kama:validate post=\"post\"\ntype A int\n\n" + prePostFuncs,
			typeName: "A",
			want: []string{
				"defer func() { err = validator.Join(err, validator.Root(post(v, stopOnFirstError))) }()",
				"return nil",
			},
		},
		{
			name: "generic",
			src: "//kama:validate pre=\"preB\"\ntype B[T any] []T\n\n" +
				"func preB[U any](B[U], bool) error { return nil }\n",
			typeName: "B",
			want:     []string{check("validator.Root(preB(v, stopOnFirstError))")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn := validateOf(t, map[string]string{"a.go": "package x\n\n" + tc.src},
				"a.kama_validate.go", tc.typeName)
			assertContains(t, fn, tc.want...)
		})
	}

	// pre runs before the validators of the type.
	fn := validateOf(t, map[string]string{
		"a.go": "package x\n\n//kama:validate cmp(gt=0), pre=\"pre\"\ntype A int\n\n" + prePostFuncs,
	}, "a.kama_validate.go", "A")
	assert.Assert(t, strings.Index(fn, "pre(v") < strings.Index(fn, "kamaValidateCmp"), fn)
}

// TestPrePostShallow calls the functions in ValidateShallow of its own and
// not in one passing the call on to Validate, so that they run once.
func TestPrePostShallow(t *testing.T) {
	out := generateAndBuild(t, map[string]string{"a.go": "package x\n\n" +
		"//kama:validate decoded, pre=\"preD\", post=\"postD\"\ntype D struct{ N int }\n\n" +
		"//kama:validate pre=\"preO\", post=\"postO\"\ntype O struct{ D D }\n\n" +
		"func preD(D, bool) error { return nil }\n\nfunc postD(D, bool) error { return nil }\n\n" +
		"func preO(O, bool) error { return nil }\n\nfunc postO(O, bool) error { return nil }\n\n" +
		"func (D) validateN(bool) error { return nil }\n\n" +
		"func (O) validateD(bool) error { return nil }\n",
	})
	funcs := gentest.Funcs(t, out["a.kama_validate.go"])

	assertContains(t, funcs["D.ValidateShallow"], "return v.Validate(stopOnFirstError)")
	assertNotContains(t, funcs["D.ValidateShallow"], "preD", "postD", "err error")
	assertContains(t, funcs["O.ValidateShallow"], "preO((*v),", "postO((*v),", "(err error)")
	assertNotContains(t, funcs["O.ValidateShallow"], "v.D.ValidateShallow")
}

// TestPrePostEvaluated calls the functions in ValidateEvaluated too, which
// ref calls in place of Validate in the scope of unevalprops.
func TestPrePostEvaluated(t *testing.T) {
	out := generateAndBuild(t, map[string]string{"a.go": "package x\n\n" +
		"import \"encoding/json/jsontext\"\n\n" +
		"//kama:validate kind(\"object\"), pre=\"preR\", post=\"postR\"\ntype R jsontext.Value\n\n" +
		"func (v R) RawJson() jsontext.Value { return jsontext.Value(v) }\n\n" +
		"func preR(R, bool) error { return nil }\n\nfunc postR(R, bool) error { return nil }\n",
	})
	funcs := gentest.Funcs(t, out["a.kama_validate.go"])

	for _, name := range []string{"R.Validate", "R.ValidateEvaluated"} {
		assertContains(t, funcs[name], "(err error) {",
			check("validator.Root(preR(v, stopOnFirstError))"),
			"validator.Root(postR(v, stopOnFirstError))",
		)
	}
}

func TestPrePostErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		is   error
		msgs []string
	}{
		{
			name: "missing",
			src:  "//kama:validate pre=\"nope\"\ntype A int\n",
			is:   ErrHook,
			msgs: []string{
				"a.go:3:1", `//kama:validate pre="nope" on`, "names no package-level function nope",
				"declare func nope(v A, stopOnFirstError bool) error",
			},
		},
		{
			name: "method",
			src: "//kama:validate post=\"check\"\ntype A int\n\n" +
				"func (A) check(bool) error { return nil }\n",
			is:   ErrHook,
			msgs: []string{"names no package-level function check"},
		},
		{
			name: "wrong signature",
			src:  "//kama:validate pre=\"pre\"\ntype A int\n\nfunc pre(*A, bool) error { return nil }\n",
			is:   ErrHook,
			msgs: []string{"names pre with signature func(*", "declare func pre(v A,"},
		},
		{
			name: "generic spelled",
			src: "//kama:validate post=\"post\"\ntype A[T any, U comparable] []T\n\n" +
				"func post(A[int, int], bool) error { return nil }\n",
			is: ErrHook,
			msgs: []string{
				"declare func post[T any, U comparable](v A[T, U], stopOnFirstError bool)",
			},
		},
		{
			name: "hand-written Validate",
			src:  "//kama:validate\ntype A struct{}\n\nfunc (A) Validate(bool) error { return nil }\n",
			is:   ErrConflict,
			msgs: []string{
				"declares Validate itself; remove the method",
				`pre="<func>" or post="<func>"`, "or into a field hook",
			},
		},
		{
			name: "given twice",
			src:  "//kama:validate pre=\"a\"\n//kama:validate pre=\"b\"\ntype A int\n",
			is:   ErrDirective,
			msgs: []string{"a.go:3:1", "pre given twice on type"},
		},
		{
			name: "no function name",
			src:  "//kama:validate post\ntype A int\n",
			is:   ErrDirective,
			msgs: []string{"item post on", "is a keyword item; write post=<value>"},
		},
		{
			name: "on a field",
			src:  "//kama:validate\ntype A struct {\n\t//kama:validate pre=\"p\"\n\tN int\n}\n",
			is:   ErrDirective,
			msgs: []string{"pre on field", "write it in the doc comment of the type"},
		},
		{
			name: "on an ignored type",
			src: "//kama:validate ignore, post=\"p\"\ntype A int\n\n" +
				"func p(A, bool) error { return nil }\n",
			is:   ErrDirective,
			msgs: []string{`//kama:validate post="p" on`, "A, which gets no Validate"},
		},
		{
			name: "on a type group",
			src: "//kama:validate pre=\"p\"\ntype (\n\tA struct{}\n\tB struct{}\n)\n\n" +
				"func p(A, bool) error { return nil }\n",
			is:   ErrDirective,
			msgs: []string{`//kama:validate pre="p" on a type group`},
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

// TestOptionFilePrePost adds the functions of an option file entry to the
// directives of the type, as for a type another generator wrote, without a
// warning. An entry setting anything else replaces the directives.
func TestOptionFilePrePost(t *testing.T) {
	out, warnings, err := generateWithWarnings(t, map[string]string{
		"a.go": "package x\n\n//kama:validate cmp(gt=0)\ntype A int\n\n" +
			"//kama:validate\ntype B struct {\n\t//kama:validate required\n\tN int\n}\n\n" +
			"//kama:validate cmp(gt=0)\ntype C int\n\n" + prePostFuncs +
			"func (B) validateN(bool) error { return nil }\n\n" +
			"func preB(B, bool) error { return nil }\n\nfunc postC(C, bool) error { return nil }\n",
		"kama_validate.yaml": "version: 1\ntypes:\n" +
			"  A:\n    pre: pre\n    post: post\n" +
			"  B:\n    pre: preB\n" +
			"  C:\n    post: postC\n    validators:\n      - cmp(lt=9)\n",
	}, nil)
	assert.NilError(t, err)

	funcs := gentest.Funcs(t, out["a.kama_validate.go"])
	assertContains(t, funcs["A.Validate"],
		check("validator.Root(pre(v, stopOnFirstError))"),
		"post(v, stopOnFirstError)",
		check(`kamaValidateCmp(v, ">", 0)`),
	)
	assertContains(t, funcs["B.Validate"],
		check("validator.Root(preB((*v), stopOnFirstError))"),
		check(`validator.Field(requiredComparable(v.N), "N")`),
		check(`validator.Field(v.validateN(stopOnFirstError), "N")`),
	)
	assertContains(t, funcs["C.Validate"], "postC(v,", check(`kamaValidateCmp(v, "<", 9)`))
	assertNotContains(t, funcs["C.Validate"], `">", 0`)

	assert.Equal(t, len(warnings), 1, "%v", warnings)
	assert.ErrorContains(t, warnings[0], modulePath(t)+".C")
}
