package basic

import "github.com/ngicks/kama/kamalib/generator/validator/ext"

// UnevalItems is the validator //kama:validate unevalitems(<scope>,
// <validator>, ...): unevalprops for the elements of an array, as JSON
// Schema unevaluatedItems tests.
type UnevalItems struct{}

// Name returns "unevalitems".
func (UnevalItems) Name() string { return "unevalitems" }

// Parse decodes arg into the scope and operands through r.
func (UnevalItems) Parse(arg string, r ext.Resolver) (ext.Rule, error) {
	return parseUnevaluated(arg, r, true)
}
