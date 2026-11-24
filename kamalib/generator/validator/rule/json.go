package rule

import (
	"fmt"
	"go/token"
	"go/types"
)

// Json is the validator //kama:validate json(<validator>, ...): the value
// encoded as JSON passes every operand as raw JSON text, as JSON Schema
// applies its keywords to the JSON value whatever Go type holds it. It
// brings the validators of raw JSON text, such as not, props or contains,
// to a value of a struct, a slice or a scalar type.
//
// It applies to a type of any kind but a pointer, an opt.Option and an
// und.Und, so the generator applies it to the value one holds, and the
// encoding of an absent value, null, is never checked. Its operands apply
// to jsontext.Value. It returns the errors of the operands. A value that
// does not encode is an error wrapping validation.ErrEncode. It is a
// StructRule too, written in the doc comment of a struct type as well as on
// a field.
type Json struct{}

// Name returns "json".
func (Json) Name() string { return "json" }

// JsonOptions are the options of //kama:validate json(<validator>, ...).
type JsonOptions struct {
	// Operands are the validators of the args, in order.
	Operands []Operand
}

// Parse decodes arg into operands through r.
func (Json) Parse(arg string, r Resolver) (Rule, error) {
	ops, err := ParseOperands(arg, r)
	if err != nil {
		return nil, err
	}

	return JsonOptions{Operands: ops}, nil
}

// Check accepts a type that holds no other value, when every operand
// applies to jsontext.Value.
func (o JsonOptions) Check(t types.Type) error {
	if _, _, _, ok := HeldValue(t, nil); ok {
		return fmt.Errorf("%s may hold no value, whose encoding json would check", t)
	}

	return checkOperands(o.Operands, jsontextValue())
}

// Emit calls rawjson.Encoded with a function literal of the operands.
func (o JsonOptions) Emit(e Emitter, _ types.Type, v Gen) (Gen, error) {
	f, err := funcLit("m", lang.Expr("%s.Value", e.Import(jsontextPath)), func(m Gen) (Gen, error) {
		errs, err := emitOperands(e, o.Operands, jsontextValue(), m)
		if err != nil {
			return nil, err
		}

		return joinErrors(e, errs), nil
	})
	if err != nil {
		return nil, err
	}

	return rawCall(e, "Encoded", v, f), nil
}

// Struct returns o, which validates the encoding of the struct.
func (o JsonOptions) Struct(Struct) (Rule, error) { return o, nil }

// jsontextValue returns jsontext.Value, the type the operands of json
// check. The rule sees no package importing encoding/json/jsontext, so it
// builds the type, which IsRawJson tells by its package path and name.
func jsontextValue() types.Type {
	pkg := types.NewPackage(jsontextPath, "jsontext")

	return types.NewNamed(
		types.NewTypeName(token.NoPos, pkg, "Value", nil),
		types.NewSlice(types.Typ[types.Byte]),
		nil,
	)
}
