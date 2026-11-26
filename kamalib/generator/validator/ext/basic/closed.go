package basic

import (
	"errors"
	"fmt"
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Closed is the validator //kama:validate closed: a value of a struct holds
// no JSON member beyond those its fields encode as, as JSON Schema
// additionalProperties false requires. It takes no arg.
//
// It is a StructRule, written in the doc comment of a struct type. The
// embedded fallback field, tagged json:",embed", holds the other members,
// and Closed reports each of its entries, in the order of the keys, as an
// error located at the member. The fallback field must be a map with a key
// of a string underlying type. A struct without one holds no other member
// and always passes.
type Closed struct{}

// Name returns "closed".
func (Closed) Name() string { return "closed" }

// ClosedOptions are the options of //kama:validate closed. It has none.
type ClosedOptions struct{}

// Parse takes no args.
func (Closed) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	return ClosedOptions{}, noArgs(r, c)
}

// Check rejects every type: closed reads the fallback field of a struct,
// which Struct gives.
func (ClosedOptions) Check(types.Type) error { return errClosedStruct }

// Emit fails as Check does.
func (ClosedOptions) Emit(ext.Emitter, types.Type, ext.Gen) (ext.Gen, error) {
	return nil, errClosedStruct
}

// Struct returns the rule reporting the entries of the fallback field of s.
func (ClosedOptions) Struct(s ext.Struct) (ext.Rule, error) {
	t, v, ok := s.Fallback()
	if !ok {
		return closedOf{}, nil
	}

	m, isMap := t.Underlying().(*types.Map)
	if !isMap || !isStringType(m.Key()) {
		return nil, fmt.Errorf(
			"the embedded fallback field of type %s is no map keyed by strings, "+
				"whose entries closed reports", t,
		)
	}

	return closedOf{fallback: v}, nil
}

var errClosedStruct = errors.New("closed reads the members of a struct;" +
	" write it in the doc comment of a struct type")

// closedOf is Closed applied to a struct whose fallback field fallback
// spells, nil for none.
type closedOf struct {
	fallback ext.Gen
}

// Check accepts any type. Struct checked the struct already.
func (closedOf) Check(types.Type) error { return nil }

// Emit calls kamaValidateClosed with the fallback map.
func (c closedOf) Emit(e ext.Emitter, _ types.Type, _ ext.Gen) (ext.Gen, error) {
	if c.fallback == nil {
		return lang.Expr("error(nil)"), nil
	}

	e.Declare(closedName, closedDecl)

	return lang.Expr("%s(%s, %s)", closedName, c.fallback, e.StopOnFirstError()), nil
}

const closedName = "kamaValidateClosed"

func closedDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr("func %s[%s ~string, %s any](%s map[%s]%s, %s bool) error",
			closedName, shadow("K"), shadow("V"), shadow("m"), ref("K"), ref("V"),
			shadow("stopOnFirstError")),
		lang.Stmt("var %s []error", shadow("errs")),
		lang.For(nil,
			lang.Expr("_, %s := range %s.Sorted(%s.Keys(%s))",
				shadow("k"), imp.Import("slices"), imp.Import("maps"), ref("m")),
			nil,
			lang.Steps(
				lang.Stmt(
					`%s := %s.Field(%s.New(%s.ErrClosed, "member is not allowed"), string(%s))`,
					shadow("err"), v, v, v, ref("k"),
				),
				lang.If(
					lang.Expr("%s", ref("stopOnFirstError")),
					lang.Stmt("return %s", ref("err")),
					nil,
				),
				lang.Stmt("%s = append(%s, %s)", ref("errs"), ref("errs"), ref("err")),
			),
		),
		lang.Stmt("return %s.Join(%s...)", v, ref("errs")),
	)
}
