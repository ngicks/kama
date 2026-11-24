package rule

import (
	"fmt"
	"go/token"
	"go/types"
)

// Decode is the validator //kama:validate decode=<type>: the value decodes
// into type, a type of the generated package, as JSON Schema $ref applies
// the schema it names. The decoder of type must reject a value type does
// not admit, as the decoder of a struct the jsonschema generator declares
// does by calling its Validate. Decode never calls Validate itself: a
// decoder validating on decoding would validate a value twice, and a
// recursive value as many times as two to the power of its depth.
//
// It applies to raw JSON text only, as IsRawJson tells. Its error wraps
// validation.ErrDecode. It records no annotation.
type Decode struct{}

// Name returns "decode".
func (Decode) Name() string { return "decode" }

// DecodeOptions are the options of //kama:validate decode=<type>.
type DecodeOptions struct {
	// Type is the name of the type.
	Type string
}

// Parse decodes arg into the name of a type.
func (Decode) Parse(arg string, _ Resolver) (Rule, error) {
	if !token.IsIdentifier(arg) {
		return nil, argsError(fmt.Errorf("%q is no type name; want decode=<type>", arg))
	}

	return DecodeOptions{Type: arg}, nil
}

// Check accepts raw JSON text.
func (o DecodeOptions) Check(t types.Type) error {
	if !IsRawJson(t) {
		return fmt.Errorf("%s is no raw JSON text, which decode decodes into %s", t, o.Type)
	}

	return nil
}

// Emit calls rawjson.Decode of the type.
func (o DecodeOptions) Emit(e Emitter, _ types.Type, v Gen) (Gen, error) {
	return lang.Expr("%s.Decode[%s](%s)", e.Import(RawJsonPath), o.Type, v), nil
}
