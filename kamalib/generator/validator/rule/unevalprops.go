package rule

import (
	"errors"
	"fmt"
	"go/types"
)

// UnevalProps is the validator //kama:validate unevalprops(<scope>,
// <validator>, ...): the operand scope holds, and each member of an object
// that scope did not evaluate passes every other operand, as JSON Schema
// unevaluatedProperties tests. scope is the rest of the schema, usually
// and(...) of its other keywords, which reports what it evaluated as
// Annotator tells.
//
// It applies to raw JSON text only, as IsRawJson tells. Values of other
// kinds pass the operands after scope. It returns the errors of scope and
// those of the other operands, each located at its member. As an Annotator
// it records what scope evaluated and each member.
type UnevalProps struct{}

// Name returns "unevalprops".
func (UnevalProps) Name() string { return "unevalprops" }

// UnevaluatedOptions are the options of unevalprops and of unevalitems,
// which UnevalItems declares.
type UnevaluatedOptions struct {
	// Items is true for unevalitems.
	Items bool
	// Scope is the first operand.
	Scope Operand
	// Operands are the validators of the members or the elements scope did
	// not evaluate.
	Operands []Operand
}

// Parse decodes arg into the scope and operands through r.
func (UnevalProps) Parse(arg string, r Resolver) (Rule, error) {
	return parseUnevaluated(arg, r, false)
}

func parseUnevaluated(arg string, r Resolver, items bool) (Rule, error) {
	ops, err := ParseOperands(arg, r)
	if err != nil {
		return nil, err
	}

	if len(ops) == 0 {
		return nil, argsError(errors.New("want the operand scope first"))
	}

	return UnevaluatedOptions{Items: items, Scope: ops[0], Operands: ops[1:]}, nil
}

func (o UnevaluatedOptions) name() string {
	if o.Items {
		return "unevalitems"
	}

	return "unevalprops"
}

// Check accepts raw JSON text that scope applies to and whose members or
// elements every other operand applies to.
func (o UnevaluatedOptions) Check(t types.Type) error {
	if err := checkRaw(o.name(), o.Operands, t); err != nil {
		return err
	}

	if err := o.Scope.Rule.Check(t); err != nil {
		return fmt.Errorf("operand %s: %w", o.Scope.Text, err)
	}

	return nil
}

// Emit calls rawjson.UnevaluatedMembers or rawjson.UnevaluatedElements
// recording nothing.
func (o UnevaluatedOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	return o.Annotate(e, t, v, noRecord)
}

// Annotate calls rawjson.UnevaluatedMembers or rawjson.UnevaluatedElements
// with a function literal of scope and one of the other operands.
func (o UnevaluatedOptions) Annotate(e Emitter, t types.Type, v, ev Gen) (Gen, error) {
	scope, err := scopeFunc(e, []Operand{o.Scope}, t, v)
	if err != nil {
		return nil, err
	}

	f, err := valueFunc(e, o.Operands, t)
	if err != nil {
		return nil, err
	}

	name := "UnevaluatedMembers"
	if o.Items {
		name = "UnevaluatedElements"
	}

	return rawCall(e, name, v, e.StopOnFirstError(), ev, scope, f), nil
}
