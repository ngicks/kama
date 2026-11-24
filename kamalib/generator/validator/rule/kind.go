package rule

import (
	"errors"
	"fmt"
	"go/types"
	"slices"
	"strconv"

	"github.com/ngicks/opiter"
)

// Kind is the validator //kama:validate kind="<kind> ...": the value is of
// one of the JSON kinds listed, as JSON Schema type tests: null, boolean,
// object, array, number, string or integer, a number with a zero fraction
// such as 1.0.
//
// It applies to raw JSON text only, as IsRawJson tells. The Go type of
// another value decides its kind already.
type Kind struct{}

// Name returns "kind".
func (Kind) Name() string { return "kind" }

// KindOptions are the options of //kama:validate kind="<kind> ...".
type KindOptions struct {
	// Kinds are the kinds of the arg, in order.
	Kinds []string
}

// jsonKinds are the kinds Kind takes.
var jsonKinds = []string{"null", "boolean", "object", "array", "number", "string", "integer"}

// Parse decodes arg into one or more kinds separated by spaces.
func (Kind) Parse(arg string, _ Resolver) (Rule, error) {
	args, err := argTokens(arg)
	if err != nil {
		return nil, err
	}

	if len(args) == 0 {
		return nil, argsError(errors.New("want one or more JSON kinds, as in kind=\"string null\""))
	}

	kinds := make([]string, 0, len(args))

	for _, kv := range args {
		if kv.HasValue || !slices.Contains(jsonKinds, kv.Key) {
			return nil, argsError(
				fmt.Errorf("arg %q is no JSON kind; want one of %v", kv.String(), jsonKinds),
			)
		}

		kinds = append(kinds, kv.Key)
	}

	return KindOptions{Kinds: kinds}, nil
}

// Check accepts raw JSON text.
func (KindOptions) Check(t types.Type) error {
	if !IsRawJson(t) {
		return fmt.Errorf("%s is no raw JSON text, whose Go type tells its kind", t)
	}

	return nil
}

// Emit calls rawjson.Kind.
func (o KindOptions) Emit(e Emitter, _ types.Type, v Gen) (Gen, error) {
	kinds := opiter.WrapSeq(slices.Values(o.Kinds)).
		Map(func(k string) Gen { return lang.Expr("%s", strconv.Quote(k)) }).
		CollectSlice()

	return rawCall(e, "Kind", v, kinds...), nil
}
