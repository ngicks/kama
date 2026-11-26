package validator

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// keywordError reports the keyword item of vo, which names nothing on
// place. A keyword naming a validator of reg is a rule written as name=value,
// which a directive spells as a call.
func keywordError(reg *ext.Registry, vo ValidatorOptions, place string) error {
	if _, ok := reg.Lookup(vo.Item.Name); !ok {
		return fmt.Errorf(
			"%w: %s: unknown item %s on %s", ErrDirective, vo.src.origin(), vo.Item, place,
		)
	}

	return fmt.Errorf(
		"%w: %s: %s on %s names the validator %s as a keyword; write it as a call, as in %s",
		ErrDirective, vo.src.origin(), vo.Item, place, vo.Item.Name, callHint(vo.Item),
	)
}

// comparisonRules are the validators whose args are comparisons.
var comparisonRules = []string{"length", "runes", "members", "cmp"}

// callHint spells the call that stands for the keyword item a, as in
// length(ge=1, le=3) for length=">=1 <=3". It spells name(...) when it
// cannot tell the args.
func callHint(a expr.Arg) string {
	text, isString := a.Value.(expr.String)

	switch {
	case isString && slices.Contains(comparisonRules, a.Name):
		args, ok := comparisonArgs(text.Value)
		if ok {
			return expr.Call{Name: a.Name, Args: args}.String()
		}
	case isString && slices.Contains([]string{"kind", "requires", "required"}, a.Name):
		args := opiter.WrapSeq(slices.Values(strings.Fields(text.Value))).
			Map(func(w string) expr.Arg { return expr.Arg{Value: expr.String{Value: w}} }).
			CollectSlice()

		return expr.Call{Name: a.Name, Args: args}.String()
	case isString, a.Name != "const" && isNumber(a.Value):
		return expr.Call{Name: a.Name, Args: []expr.Arg{{Value: a.Value}}}.String()
	}

	if c, ok := a.Value.(expr.Call); ok && !c.Parens && len(c.Args) == 0 {
		return expr.Call{
			Name: a.Name,
			Args: []expr.Arg{{Value: expr.String{Value: c.Name}}},
		}.String()
	}

	return a.Name + "(...)"
}

func isNumber(n expr.Node) bool {
	_, ok := n.(expr.Number)
	return ok
}

// comparisonArgs spells the comparisons text, as in >=1 <=3, as keyword
// args, as in ge=1, le=3.
func comparisonArgs(text string) ([]expr.Arg, bool) {
	ops := []string{">=", "<=", "==", "!=", ">", "<"}
	keywords := []string{"ge", "le", "eq", "ne", "gt", "lt"}

	var args []expr.Arg

	for tok := range strings.FieldsSeq(text) {
		i := slices.IndexFunc(ops, func(op string) bool { return strings.HasPrefix(tok, op) })
		if i < 0 {
			return nil, false
		}

		v, err := expr.Parse(strings.TrimPrefix(tok, ops[i]))
		if err != nil || len(v) != 1 {
			return nil, false
		}

		args = append(args, expr.Arg{Name: keywords[i], Value: v[0].Value})
	}

	return args, len(args) > 0
}
