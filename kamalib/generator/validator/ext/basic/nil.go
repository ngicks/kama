package basic

import (
	"fmt"
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
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

// Parse takes no args.
func (Nil) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	return NilOptions{}, noArgs(r, c)
}

// Check accepts nillable types.
func (NilOptions) Check(t types.Type) error {
	if !nillable(t) {
		return fmt.Errorf("%s is not nillable", t)
	}

	return nil
}

// Emit calls kamaValidateNil.
func (NilOptions) Emit(e ext.Emitter, _ types.Type, v ext.Gen) (ext.Gen, error) {
	e.Declare(nilHelperName, nilHelperDecl)
	return lang.Expr("%s(%s == nil)", nilHelperName, v), nil
}

const nilHelperName = "kamaValidateNil"

func nilHelperDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

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
