package validator

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
)

const shallowSrc = "package x\n\n" +
	"//kama:validate hook=False, decoded\ntype Leaf struct {\n" +
	"\t//kama:validate cmp(ge=0)\n\tN int\n}\n\n" +
	"type Leaves []Leaf\n\n" +
	"type U struct{}\n\n" +
	"func (U) Validate(bool) error { return nil }\n\n" +
	"func (U) ValidateShallow(bool) error { return nil }\n\n" +
	"type Us []U\n\n" +
	"//kama:validate hook=False\ntype Plain struct {\n" +
	"\t//kama:validate cmp(gt=0)\n\tN int\n}\n\n" +
	"//kama:validate hook=False, decoded\ntype Root struct {\n" +
	"\t//kama:validate cmp(gt=0)\n\tA int\n" +
	"\tL Leaf\n\tP *Leaf\n\tS []Leaf\n\tM map[string]Leaf\n" +
	"\tLs Leaves\n\tU U\n\tUs Us\n\tPlain Plain\n\tSelf *Root\n}\n"

// TestValidateShallow skips the values of types marked decoded, calls
// ValidateShallow on the targets and implementors having one, and Validate
// on the others. A type marked decoded with nothing to skip gets a
// ValidateShallow calling its Validate, since its decoder calls it.
func TestValidateShallow(t *testing.T) {
	out := generateAndBuild(t, map[string]string{"a.go": shallowSrc})
	funcs := gentest.Funcs(t, out["a.kama_validate.go"])

	shallow, ok := funcs["Root.ValidateShallow"]
	assert.Assert(t, ok, "%v", out["a.kama_validate.go"])
	assertContains(t, shallow,
		check(`validator.Field(kamaValidateCmp(v.A, ">", 0), "A")`),
		check(`validator.Field(v.Ls.ValidateShallow(stopOnFirstError), "Ls")`),
		check(`validator.Field(v.U.ValidateShallow(stopOnFirstError), "U")`),
		check(`validator.Field(v.Us.ValidateShallow(stopOnFirstError), "Us")`),
		check(`validator.Field(v.Plain.Validate(stopOnFirstError), "Plain")`),
	)
	assertNotContains(t, shallow, "v.L.", "v.P.", "(*v.P)", "v.S", "v.M", "v.Self")

	assertContains(t, funcs["Root.Validate"],
		check(`validator.Field(v.L.Validate(stopOnFirstError), "L")`),
		check(`validator.Field(v.Ls.Validate(stopOnFirstError), "Ls")`),
		check(`validator.Field(v.U.Validate(stopOnFirstError), "U")`),
	)

	assertContains(t, funcs["Leaf.ValidateShallow"], "return v.Validate(stopOnFirstError)")

	_, ok = funcs["Plain.ValidateShallow"]
	assert.Assert(t, !ok, "Plain has nothing to skip and is not marked decoded")
}

func TestValidateShallowErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		is   error
		msg  string
	}{
		{
			name: "hand-written ValidateShallow",
			src: "//kama:validate hook=False, decoded\ntype L struct{ N int }\n\n" +
				"//kama:validate hook=False\ntype R struct{ L L }\n\n" +
				"func (R) ValidateShallow(bool) error { return nil }\n",
			is:  ErrConflict,
			msg: "R declares ValidateShallow, which the generator writes",
		},
		{
			name: "field named ValidateShallow",
			src: "//kama:validate hook=False, decoded\ntype L struct{ N int }\n\n" +
				"//kama:validate hook=False\ntype R struct{ ValidateShallow L }\n",
			is:  ErrConflict,
			msg: "field ValidateShallow of",
		},
		{
			name: "decoded with a value",
			src:  "//kama:validate decoded(\"on\")\ntype L struct{ N int }\n",
			is:   ErrDirective,
			msg:  `unknown arg decoded("on")`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"a.go": "package x\n\n" + tc.src}, testRun{})
			assert.ErrorIs(t, err, tc.is)
			assert.ErrorContains(t, err, tc.msg)
		})
	}
}

// TestValidateEvaluated gives raw JSON text a ValidateEvaluated whose
// validators record into ev, and ref annotates through it.
func TestValidateEvaluated(t *testing.T) {
	out := generateAndBuild(t, map[string]string{"a.go": "package x\n\n" +
		"import \"encoding/json/jsontext\"\n\n" +
		"//kama:validate props(field(\"a\", kind(\"integer\")))\ntype A jsontext.Value\n\n" +
		"func (v A) RawJson() jsontext.Value { return jsontext.Value(v) }\n\n" +
		"//kama:validate unevalprops(ref(\"A\"), never)\ntype R jsontext.Value\n\n" +
		"func (v R) RawJson() jsontext.Value { return jsontext.Value(v) }\n",
	})
	funcs := gentest.Funcs(t, out["a.kama_validate.go"])

	assertContains(t, funcs["A.ValidateEvaluated"], "rawjson.Properties(v, stopOnFirstError, ev,")
	assertContains(t, funcs["A.Validate"], "rawjson.Properties(v, stopOnFirstError, nil,")
	assertContains(t, funcs["R.Validate"], "return A(v).ValidateEvaluated(stopOnFirstError, ev)")
}
