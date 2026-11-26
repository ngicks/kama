package basic

import (
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// UnevalItems is the validator //kama:validate unevalitems(<scope>,
// <validator>, ...): unevalprops for the elements of an array, as JSON
// Schema unevaluatedItems tests.
type UnevalItems struct{}

// Name returns "unevalitems".
func (UnevalItems) Name() string { return "unevalitems" }

// Parse resolves the args of c, the scope and the operands, through r.
func (UnevalItems) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	return parseUnevaluated(c, r, true)
}
