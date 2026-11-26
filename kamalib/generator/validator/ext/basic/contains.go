package basic

import (
	"encoding/json/jsontext"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Contains is the validator //kama:validate contains(<validator>, ...,
// ge=<n>, ...): the number of elements of an array passing every operand
// satisfies each comparison, as JSON Schema contains, minContains and
// maxContains test. The comparisons are the keyword args of length.
// Without them the number is at least 1, as in contains(kind("string")).
//
// It applies to raw JSON text only, as IsRawJson tells, and passes a value
// of another kind. Its error wraps validator.ErrContains. As an Annotator
// it records each element passing the operands.
type Contains struct{}

// Name returns "contains".
func (Contains) Name() string { return "contains" }

// ContainsArgs are the args of contains(...), which Parse reads and Call
// spells: the operands, then the comparisons of the number of elements
// passing them, each the operand of its keyword as JSON text.
type ContainsArgs struct {
	Operands []expr.Node    `json:"operands"`
	Eq       jsontext.Value `json:"eq"`
	Ge       jsontext.Value `json:"ge"`
	Gt       jsontext.Value `json:"gt"`
	Le       jsontext.Value `json:"le"`
	Lt       jsontext.Value `json:"lt"`
	Ne       jsontext.Value `json:"ne"`
}

// Call returns the call of contains spelling a.
func (a ContainsArgs) Call() (expr.Call, error) { return containsParams.Call(Contains{}.Name(), a) }

var containsParams = ext.Params{
	Variadic: operandArgs.Variadic,
	Keywords: comparisonKeywords,
	Rules:    operandArgs.Rules,
}

// ContainsOptions are the options of //kama:validate contains(...).
type ContainsOptions struct {
	// Counts are the comparisons of the number of elements passing.
	Counts []Comparison
	// Operands are the validators an element passes.
	Operands []Operand
}

// Parse decodes the args of c into the comparisons, the keyword args, and
// the operands, the positional args, resolved through r.
func (Contains) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	counts, rest, err := parseComparisons(r, c, false)
	if err != nil {
		return nil, err
	}

	if counts == nil {
		counts = []Comparison{{Op: ">=", Operand: "1"}}
	}

	counts, err = lengths(counts)
	if err != nil {
		return nil, err
	}

	ops, err := ParseOperands(argValues(rest), r)
	if err != nil {
		return nil, err
	}

	return ContainsOptions{Counts: counts, Operands: ops}, nil
}

// Check accepts raw JSON text whose elements every operand applies to.
func (o ContainsOptions) Check(t types.Type) error { return checkRaw("contains", o.Operands, t) }

// Emit calls rawjson.Contains recording nothing.
func (o ContainsOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	return o.Annotate(e, t, v, noRecord)
}

// Annotate calls rawjson.Contains with a function literal of the operands
// and the counts.
func (o ContainsOptions) Annotate(e ext.Emitter, t types.Type, v, ev ext.Gen) (ext.Gen, error) {
	f, err := valueFunc(e, o.Operands, t)
	if err != nil {
		return nil, err
	}

	counts := opiter.WrapSeq(slices.Values(o.Counts)).
		Map(func(c Comparison) ext.Gen {
			n, _ := strconv.ParseInt(strings.TrimPrefix(c.Operand, "+"), 0, 64)

			return lang.Expr("%s.Count{Op: %s, N: %d}",
				e.Import(ext.RawJsonPath), strconv.Quote(c.Op), n)
		}).
		CollectSlice()

	return rawCall(e, "Contains", v, append([]ext.Gen{ev, f}, counts...)...), nil
}
