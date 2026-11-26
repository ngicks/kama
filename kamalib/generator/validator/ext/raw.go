package ext

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// RawJsonPath is the import path of the package whose functions the
// built-in validators call on raw JSON text.
const RawJsonPath = "github.com/ngicks/kama/kamaruntime/validator/rawjson"

// IsRawJson reports whether a value of t is raw JSON text, which a
// validator checks with the semantics of JSON Schema: t is jsontext.Value
// of encoding/json/jsontext, or a type of the underlying type []byte
// declaring the method RawJson() jsontext.Value, as a type defined over
// jsontext.Value does to tell itself apart from other byte slices.
//
// A validator accepting such a type in Rule.Check tests only the values of
// the JSON kinds it applies to and passes any other, as a JSON Schema
// keyword ignores a value of another kind: minimum passes a string. The
// built-in validators do so through the functions of the package at
// RawJsonPath.
func IsRawJson(t types.Type) bool {
	if typequery.IsNamed(t, jsontextPath, "Value") {
		return true
	}

	if !isBytes(t) {
		return false
	}

	sel := types.NewMethodSet(t).Lookup(nil, "RawJson")
	if sel == nil {
		return false
	}

	sig := sel.Type().(*types.Signature)

	return sig.Params().Len() == 0 && sig.Results().Len() == 1 &&
		typequery.IsNamed(sig.Results().At(0).Type(), jsontextPath, "Value")
}

const jsontextPath = "encoding/json/jsontext"

// isBytes reports whether the underlying type of t is []byte.
func isBytes(t types.Type) bool {
	return types.Identical(t.Underlying(), types.NewSlice(types.Typ[types.Byte]))
}
