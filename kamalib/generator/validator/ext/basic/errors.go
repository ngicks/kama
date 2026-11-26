package basic

import (
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// argsError marks err as an error of the args alone. It keeps the message
// of err. The built-in validators word their messages on their own.
func argsError(err error) error { return argsErr{err} }

type argsErr struct{ error }

func (e argsErr) Unwrap() error { return e.error }

func (argsErr) Is(target error) bool { return target == ext.ErrArgs }

// noArgs reports an arg of c, a call of a validator taking none.
func noArgs(r ext.Resolver, c expr.Call) error {
	_, err := ext.Params{}.Bind(r, c, nil)
	return err
}

// operandArgs are the parameters of a validator whose args are its
// operands, as oneof(a, b) has.
var operandArgs = ext.Params{Variadic: "operands", Rules: []string{"operands"}}
