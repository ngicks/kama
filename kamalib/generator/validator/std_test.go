package validator

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

// TestStdOperandErrors reports an operand a type of the standard library
// does not read at generation.
func TestStdOperandErrors(t *testing.T) {
	cases := []struct {
		name  string
		field string
		msg   string
	}{
		{"time not RFC 3339", `//kama:validate cmp(ge="2020-01-01")` + "\n\tF time.Time",
			`"2020-01-01" is no RFC 3339 time`},
		{"time number", "//kama:validate cmp(ge=1)\n\tF time.Time",
			"a time.Time compares with an RFC 3339 string"},
		{"time multiple", "//kama:validate multipleof(2)\n\tF time.Time",
			"a time.Time has no multiple"},
		{"duration", `//kama:validate cmp(lt="5 minutes")` + "\n\tF time.Duration",
			`"5 minutes" is no duration`},
		{"big int fraction", "//kama:validate cmp(lt=1.5)\n\tF *big.Int",
			"a *big.Int takes an integer, got 1.5"},
		{"big string", `//kama:validate cmp(lt="1")` + "\n\tF *big.Rat",
			`a *big.Rat compares with a number, got the string "1"`},
		{"big float multiple", "//kama:validate multipleof(2)\n\tF *big.Float",
			"multipleof takes a *big.Int"},
		{"big int fractional divisor", "//kama:validate multipleof(0.5)\n\tF *big.Int",
			"a *big.Int takes an integer, got 0.5"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{
				"a.go": "package x\n\nimport (\n\t\"math/big\"\n\t\"time\"\n)\n\n" +
					"var _ = big.NewInt\n\nvar _ = time.Now\n\n" +
					"//kama:validate hook=False\ntype A struct {\n\t" + tc.field + "\n}\n",
			}, testRun{})
			assert.ErrorIs(t, err, ErrUnsupported)
			assert.ErrorContains(t, err, tc.msg)
			assert.ErrorIs(t, err, ext.ErrClaimed)
			assert.Assert(t, !strings.Contains(err.Error(), "for the value it holds"), err.Error())

			if tc.name != "time multiple" && tc.name != "big float multiple" {
				assert.ErrorIs(t, err, ext.ErrArgs)
			}
		})
	}
}

// TestStdHandleGeneric spells the guard of a unique.Handle of a type
// parameter, as of a type parameter named like a local of Validate.
func TestStdHandleGeneric(t *testing.T) {
	for tp, recv := range map[string]string{"T": "v", "v": "v2"} {
		t.Run(tp, func(t *testing.T) {
			fn := validateOf(t, map[string]string{
				"a.go": "package x\n\nimport \"unique\"\n\n" +
					"//kama:validate hook=False\ntype Box[" + tp + " comparable] struct {\n" +
					"\t//kama:validate t=zero\n\tH unique.Handle[" + tp + "]\n}\n",
			}, "a.kama_validate.go", "Box")

			assertContains(t, fn,
				"if "+recv+".H != (unique.Handle["+tp+"]{}) {",
				"if validate"+tp+" != nil {",
			)
		})
	}
}
