package basic

import (
	"errors"
	"fmt"
	"go/types"
	"slices"
	"strconv"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Kind is the validator //kama:validate kind("<kind>", ...): the value is of
// one of the JSON kinds listed, as JSON Schema type tests: null, boolean,
// object, array, number, string or integer, a number with a zero fraction
// such as 1.0.
//
// It applies to raw JSON text only, as IsRawJson tells. The Go type of
// another value decides its kind already.
type Kind struct{}

// Name returns "kind".
func (Kind) Name() string { return "kind" }

// KindOptions are the options of //kama:validate kind("<kind>", ...).
type KindOptions struct {
	// Kinds are the kinds of the arg, in order.
	Kinds []string
}

// jsonKinds are the kinds Kind takes.
var jsonKinds = []string{"null", "boolean", "object", "array", "number", "string", "integer"}

// Parse decodes the args of c, one or more JSON kinds.
func (Kind) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	var args struct {
		Kinds []string `json:"kinds"`
	}

	if _, err := (ext.Params{Variadic: "kinds"}).Bind(r, c, &args); err != nil {
		return nil, err
	}

	if len(args.Kinds) == 0 {
		return nil, argsError(errors.New(
			`want one or more JSON kinds, as in kind("string", "null")`,
		))
	}

	for _, k := range args.Kinds {
		if !slices.Contains(jsonKinds, k) {
			return nil, argsError(fmt.Errorf("%q is no JSON kind; want one of %v", k, jsonKinds))
		}
	}

	return KindOptions{Kinds: args.Kinds}, nil
}

// Check accepts raw JSON text.
func (KindOptions) Check(t types.Type) error {
	if !ext.IsRawJson(t) {
		return fmt.Errorf("%s is no raw JSON text, whose Go type tells its kind", t)
	}

	return nil
}

// Emit calls rawjson.Kind.
func (o KindOptions) Emit(e ext.Emitter, _ types.Type, v ext.Gen) (ext.Gen, error) {
	kinds := opiter.WrapSeq(slices.Values(o.Kinds)).
		Map(func(k string) ext.Gen { return lang.Expr("%s", strconv.Quote(k)) }).
		CollectSlice()

	return rawCall(e, "Kind", v, kinds...), nil
}
