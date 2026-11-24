package rule

import (
	"fmt"
	"go/token"
	"go/types"
)

// Ref is the validator //kama:validate ref=<type>: the value converted to
// type, a type of the generated package defined over raw JSON text, passes
// the Validate of type, as JSON Schema $ref applies the schema it names.
// type names a type declaring Validate(bool) error, as one whose doc
// comment names validators does. A type may name itself, as ref=Tree
// inside props in the doc comment of Tree does: each call validates a
// member or an element, so the recursion ends with the value.
//
// It applies to raw JSON text only, as IsRawJson tells. It returns the
// errors of that Validate. It records no annotation, so a scope of
// unevalprops or unevalitems does not see what the type evaluated.
type Ref struct{}

// Name returns "ref".
func (Ref) Name() string { return "ref" }

// RefOptions are the options of //kama:validate ref=<type>.
type RefOptions struct {
	// Type is the name of the type.
	Type string
}

// Parse decodes arg into the name of a type.
func (Ref) Parse(arg string, _ Resolver) (Rule, error) {
	if !token.IsIdentifier(arg) {
		return nil, argsError(fmt.Errorf("%q is no type name; want ref=<type>", arg))
	}

	return RefOptions{Type: arg}, nil
}

// Check accepts raw JSON text.
func (o RefOptions) Check(t types.Type) error {
	if !IsRawJson(t) {
		return fmt.Errorf("%s is no raw JSON text, which ref converts to %s", t, o.Type)
	}

	return nil
}

// Emit calls Validate of the type on the value converted.
func (o RefOptions) Emit(e Emitter, _ types.Type, v Gen) (Gen, error) {
	return lang.Expr("%s(%s).Validate(%s)", o.Type, v, e.StopOnFirstError()), nil
}
