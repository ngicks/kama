package basic

import (
	"errors"
	"fmt"
	"go/token"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// parseOperandArgs resolves the args of c, a validator whose args are its
// operands, through r.
func parseOperandArgs(r ext.Resolver, c expr.Call) ([]Operand, error) {
	args, err := operandArgs.Bind(r, c, nil)
	if err != nil {
		return nil, err
	}

	return ParseOperands(argValues(args), r)
}

// typeArg returns the arg of c, the name of a type of the package, as ref
// and decode take it.
func typeArg(r ext.Resolver, c expr.Call) (string, error) {
	var args struct {
		Type *string `json:"type"`
	}

	if _, err := (ext.Params{Positional: []string{"type"}}).Bind(r, c, &args); err != nil {
		return "", err
	}

	if args.Type == nil {
		return "", argsError(fmt.Errorf("want a type name, as in %s(\"T\")", c.Name))
	}

	if !token.IsIdentifier(*args.Type) {
		return "", argsError(fmt.Errorf("%q is no type name", *args.Type))
	}

	return *args.Type, nil
}

var errNoComparison = errors.New("want one or more comparisons")
