package basic

import (
	"fmt"
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// annotate returns the expression of r validating v of type t and recording
// into ev, through Annotate when r implements it.
func annotate(e ext.Emitter, r ext.Rule, t types.Type, v, ev ext.Gen) (ext.Gen, error) {
	if a, ok := r.(ext.Annotator); ok {
		return a.Annotate(e, t, v, ev)
	}

	return r.Emit(e, t, v)
}

// annotateOperands returns the expression of each operand of ops validating
// v of type t and recording into ev.
func annotateOperands(
	e ext.Emitter,
	ops []Operand,
	t types.Type,
	v, ev ext.Gen,
) ([]ext.Gen, error) {
	out := make([]ext.Gen, 0, len(ops))

	for _, op := range ops {
		g, err := annotate(e, op.Rule, t, v, ev)
		if err != nil {
			return nil, fmt.Errorf("operand %s: %w", op.Text, err)
		}

		out = append(out, g)
	}

	return out, nil
}

// noRecord is the ev of a rule recording nothing.
var noRecord = lang.Expr("nil")

// rawValue returns jsontext.Value for raw JSON text t, the type a rule
// reaching into t passes its members and elements as. t is jsontext.Value,
// or a type whose method RawJson returns it.
func rawValue(t types.Type) types.Type {
	if typequery.IsNamed(t, jsontextPath, "Value") {
		return t
	}

	sel := types.NewMethodSet(t).Lookup(nil, "RawJson")

	return sel.Type().(*types.Signature).Results().At(0).Type()
}

// checkRaw reports t that is no raw JSON text, which the rule name reaches
// into, and every operand of ops that cannot apply to its members or
// elements.
func checkRaw(name string, ops []Operand, t types.Type) error {
	if !ext.IsRawJson(t) {
		return fmt.Errorf("%s is no raw JSON text, which %s reaches into", t, name)
	}

	return checkOperands(ops, rawValue(t))
}

// valueFunc returns a function literal of type rawjson.Func returning the
// errors of ops on its parameter, of the type rawValue gives for t.
func valueFunc(e ext.Emitter, ops []Operand, t types.Type) (ext.Gen, error) {
	inner := rawValue(t)

	return funcLit(
		"m",
		lang.Expr("%s.Value", e.Import(jsontextPath)),
		func(m ext.Gen) (ext.Gen, error) {
			errs, err := emitOperands(e, ops, inner, m)
			if err != nil {
				return nil, err
			}

			return joinErrors(e, errs), nil
		},
	)
}

// scopeFunc returns a function literal of type rawjson.Scope returning the
// errors of ops on v of type t, joined, recording into its parameter.
func scopeFunc(e ext.Emitter, ops []Operand, t types.Type, v ext.Gen) (ext.Gen, error) {
	return funcLit(
		"ev",
		lang.Expr("*%s.Evaluated", e.Import(ext.RawJsonPath)),
		func(ev ext.Gen) (ext.Gen, error) {
			errs, err := annotateOperands(e, ops, t, v, ev)
			if err != nil {
				return nil, err
			}

			return joinErrors(e, errs), nil
		},
	)
}

// funcLit returns a function literal of one parameter of type typ, named
// after role, returning the expression of type error body builds from the
// parameter. body runs at once, while the name is given when the literal
// renders: a name fresh in the scope it renders in, so that a literal
// nested in another shadows nothing the expression reads.
func funcLit(role string, typ ext.Gen, body func(param ext.Gen) (ext.Gen, error)) (ext.Gen, error) {
	var name string

	b, err := body(func(ext.Context) (string, error) { return name, nil })
	if err != nil {
		return nil, err
	}

	return func(c ext.Context) (string, error) {
		name = c.Scope().Declare(role)
		return lang.Expr("func(%s %s) error {\nreturn %s\n}", name, typ, b)(c)
	}, nil
}

// parseNamed parses kv, an entry written key(<word>, <validator>, ...), into
// its first arg, a bare or quoted word without a value, and the operands
// after it.
func parseNamed(kv dircmnt.KeyValue, r ext.Resolver) (string, []Operand, error) {
	if len(kv.Args) == 0 || kv.Args[0].Call || kv.Args[0].HasValue {
		return "", nil, argsError(fmt.Errorf(
			"%s: want a name or a pattern first, as in %s(\"a\", kind=string)", kv.Key, kv.Key,
		))
	}

	ops, err := ParseOperands(dircmnt.SpellCallArgs(kv.Args[1:]), r)
	if err != nil {
		return "", nil, fmt.Errorf("%s %q: %w", kv.Key, kv.Args[0].Key, err)
	}

	return kv.Args[0].Key, ops, nil
}

// branchOperands returns the expression of each operand of ops validating
// v of type t in a call of rawjson.Branch, which adds what the operand
// evaluated to ev when it holds.
func branchOperands(e ext.Emitter, ops []Operand, t types.Type, v, ev ext.Gen) ([]ext.Gen, error) {
	out := make([]ext.Gen, 0, len(ops))

	for _, op := range ops {
		f, err := scopeFunc(e, []Operand{op}, t, v)
		if err != nil {
			return nil, err
		}

		out = append(out, lang.Expr("%s.Branch(%s, %s)", e.Import(ext.RawJsonPath), ev, f))
	}

	return out, nil
}
