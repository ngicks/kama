package basic

import (
	"fmt"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

// argsError marks err as an error of the args alone. It keeps the message
// of err. The built-in validators word their messages on their own.
func argsError(err error) error { return argsErr{err} }

type argsErr struct{ error }

func (e argsErr) Unwrap() error { return e.error }

func (argsErr) Is(target error) bool { return target == ext.ErrArgs }

// noArg reports an argument given to a validator that takes none.
func noArg(arg string) error {
	if arg == "" {
		return nil
	}

	return argsError(fmt.Errorf("unexpected arg %q; want none", arg))
}
