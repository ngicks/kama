package rule

import (
	"fmt"
	"go/types"
)

// Nil is the validator //kama:validate nil: the value is nil.
//
// It applies to pointer, slice, map, channel, function and interface
// types.
type Nil struct{}

// Name returns "nil".
func (Nil) Name() string { return "nil" }

// NilOptions are the options of //kama:validate nil. Nil takes no arg.
type NilOptions struct{}

// Parse accepts only the empty arg.
func (Nil) Parse(arg string, _ Resolver) (Rule, error) {
	return NilOptions{}, noArg(arg)
}

// Check accepts nillable types.
func (NilOptions) Check(t types.Type) error {
	if !nillable(t) {
		return fmt.Errorf("%s is not nillable", t)
	}

	return nil
}

// Emit calls kamaValidateNil.
func (NilOptions) Emit(e Emitter, _ types.Type, v Gen) (Gen, error) {
	e.Declare(nilHelperName, nilHelperDecl)
	return lang.Expr("%s(%s == nil)", nilHelperName, v), nil
}

const nilHelperName = "kamaValidateNil"

func nilHelperDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s bool) error", nilHelperName, shadow("ok")),
		lang.If(
			lang.Expr("!%s", ref("ok")),
			lang.Stmt(`return %s.New(%s.ErrNil, "want nil, got a non-nil value")`, v, v),
			nil,
		),
		lang.Stmt("return nil"),
	)
}
