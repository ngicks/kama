package rule

import (
	"fmt"
	"go/types"
	"regexp"
	"strconv"

	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// RawJsonPath is the import path of the package whose functions the
// built-in validators call on raw JSON text.
const RawJsonPath = "github.com/ngicks/kama/kamaruntime/validator/rawjson"

// IsRawJson reports whether a value of t is raw JSON text, which a
// validator checks with the semantics of JSON Schema: t is jsontext.Value
// of encoding/json/jsontext, or a type of the underlying type []byte
// declaring the method RawJson() jsontext.Value, as a type defined over
// jsontext.Value does to tell itself apart from other byte slices.
//
// A validator accepting such a type in Rule.Check tests only the values of
// the JSON kinds it applies to and passes any other, as a JSON Schema
// keyword ignores a value of another kind: minimum passes a string. The
// built-in validators do so through the functions of the package at
// RawJsonPath.
func IsRawJson(t types.Type) bool {
	if typequery.IsNamed(t, jsontextPath, "Value") {
		return true
	}

	if !isBytesType(t) {
		return false
	}

	sel := types.NewMethodSet(t).Lookup(nil, "RawJson")
	if sel == nil {
		return false
	}

	sig := sel.Type().(*types.Signature)

	return sig.Params().Len() == 0 && sig.Results().Len() == 1 &&
		typequery.IsNamed(sig.Results().At(0).Type(), jsontextPath, "Value")
}

const jsontextPath = "encoding/json/jsontext"

// jsonNumber matches the text of a JSON number.
var jsonNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// rawCall returns the call of the function name of the package at
// RawJsonPath with v and args.
func rawCall(e Emitter, name string, v Gen, args ...Gen) Gen {
	return lang.Expr(
		"%s.%s(%s)",
		e.Import(RawJsonPath),
		name,
		lang.Join(", ", append([]Gen{v}, args...)...),
	)
}

// rawComparisons returns the calls of the function name, one per
// comparison of cs, joined as joinErrors does. The operand is passed as a
// string when quoted is true, else as the integer literal it is.
func rawComparisons(e Emitter, name string, v Gen, cs []Comparison, quoted bool) Gen {
	return joinCalls(e, cs, func(c Comparison) Gen {
		operand := c.Operand
		if quoted {
			operand = strconv.Quote(operand)
		}

		return rawCall(e, name, v, lang.Expr("%s", strconv.Quote(c.Op)), lang.Expr("%s", operand))
	})
}

// checkRawNumbers reports an operand of cs that is no JSON number, which
// the functions of the package at RawJsonPath take.
func checkRawNumbers(cs []Comparison) error {
	for _, c := range cs {
		if !jsonNumber.MatchString(c.Operand) {
			return fmt.Errorf("%s is no JSON number, which raw JSON text compares with", c.Operand)
		}
	}

	return nil
}
