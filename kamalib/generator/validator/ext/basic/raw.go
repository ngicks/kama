package basic

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

const jsontextPath = "encoding/json/jsontext"

// jsonNumber matches the text of a JSON number.
var jsonNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// rawCall returns the call of the function name of the package at
// RawJsonPath with v and args.
func rawCall(e ext.Emitter, name string, v ext.Gen, args ...ext.Gen) ext.Gen {
	return lang.Expr(
		"%s.%s(%s)",
		e.Import(ext.RawJsonPath),
		name,
		lang.Join(", ", append([]ext.Gen{v}, args...)...),
	)
}

// rawComparisons returns the calls of the function name, one per
// comparison of cs, joined as joinErrors does. The operand is passed as a
// string when quoted is true, else as the integer literal it is.
func rawComparisons(e ext.Emitter, name string, v ext.Gen, cs []Comparison, quoted bool) ext.Gen {
	return joinCalls(e, cs, func(c Comparison) ext.Gen {
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
