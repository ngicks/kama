package jsonschema

import (
	"slices"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// The functions of this file tell which keywords of a schema with a Go type
// other than raw JSON text the Go type leaves to the validators of raw JSON
// text, and spell those validators as json(...), which applies them to the
// value encoded as JSON. The type keeps its shape whatever keywords it
// holds.

// jsonArgs returns the arg json(...) checking the keywords of the schema at
// ptr that bridged tells, or none when no such keyword checks anything.
// Under unevaluatedProperties or unevaluatedItems it checks every keyword,
// those the Go type checks too, since the annotations of every keyword
// decide what is unevaluated.
func jsonArgs(doc *jsonschema.Schema, opts Options, ptr string) ([]string, error) {
	s, err := at(doc, ptr)
	if err != nil || s.Bool.IsSome() || raw(doc, ptr) {
		return nil, err
	}

	keep := func(kw string) bool { return bridged(doc, ptr, s, kw) }
	if s.UnevaluatedProperties != nil || s.UnevaluatedItems != nil {
		keep = everyKeyword
	}

	args, err := rulesOf(doc, opts, ptr, false, keep, nil)
	if err != nil || len(args) == 0 {
		return nil, err
	}

	return []string{callArg("json", args...)}, nil
}

// bridged reports whether the Go type of the schema s at ptr, which is no
// raw JSON text, leaves the keyword kw to the validators of the value
// encoded as JSON:
//
//   - every keyword of a union but the oneOf or anyOf of its members, or
//     the type of a union of kinds;
//   - every keyword of a product but allOf, type, properties, required and
//     additionalProperties;
//   - the keywords of ruleKeywords, minContains and maxContains;
//   - an allOf that is no product;
//   - items next to prefixItems and additionalProperties next to
//     patternProperties, which narrow the values they apply to;
//   - a required naming a member without a property of its own, and a
//     dependentRequired of a map or of a product, or naming such a member.
func bridged(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema, kw string) bool {
	switch {
	case union(s) && !nullableUnion(doc, ptr):
		return kw != unionKeyword(s) && (kw != "type" || !multiKind(s))
	case slices.Contains(ruleKeywords, kw) || kw == "minContains" || kw == "maxContains":
		return true
	}

	switch kw {
	case "allOf", "items", "additionalProperties", "required", "dependentRequired":
	case "properties", "type", "$ref", "oneOf", "anyOf":
		return false
	default:
		return product(doc, ptr, s)
	}

	if product(doc, ptr, s) {
		return kw == "dependentRequired" || kw == "required" && requiresUndeclared(s)
	}

	switch kw {
	case "allOf":
		return !transparentAllOf(doc, ptr, s)
	case "items":
		return s.PrefixItems != nil
	case "additionalProperties":
		return s.PatternProperties != nil
	case "required":
		return structKind(s) && requiresUndeclared(s)
	}

	return mapOnly(s) || structKind(s) && undeclaredDependents(s)
}

// bridging returns the keywords of the schema s at ptr, which is no raw
// JSON text, that bridged tells.
func bridging(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema) []string {
	return opiter.WrapSeq(slices.Values(constraining(s))).
		Filter(func(kw string) bool { return bridged(doc, ptr, s, kw) }).
		CollectSlice()
}

// undeclaredDependents reports whether a key or a member of the
// dependentRequired of the object s has no property, which leaves no field
// to read or to test.
func undeclaredDependents(s *jsonschema.Schema) bool {
	undeclared := func(n string) bool { _, ok := s.Properties.Get(n); return !ok }

	return opiter.WrapSeq2(opiter.MapsSorted(s.DependentRequired)).
		Any(func(key string, deps []string) bool {
			return undeclared(key) || slices.ContainsFunc(deps, undeclared)
		})
}

// everyKeyword keeps every keyword, as the validators of raw JSON text do.
func everyKeyword(string) bool { return true }
