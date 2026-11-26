package jsonschema

import (
	jsonv2 "encoding/json/v2"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/opt"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// schemaText returns the JSON text of s after drop has removed keywords
// from s and from every schema under it. Two schemas compare by it.
//
// The text is deterministic: Go maps such as dependentRequired and the
// unknown keywords encode in sorted order. Keywords are removed from the
// schemas rather than from the text, where a property named like a
// keyword could not be told from it.
func schemaText(
	s *jsonschema.Schema,
	drop func(jsonschema.Schema) jsonschema.Schema,
) (string, bool) {
	b, err := jsonv2.Marshal(without(s, drop), jsonv2.Deterministic(true))

	return string(b), err == nil
}

// withoutAnnotations drops the keywords that change neither the values s
// admits nor its Go type: the annotations and the unknown keywords.
func withoutAnnotations(s jsonschema.Schema) jsonschema.Schema {
	s.Title, s.Description, s.Comment = opt.None[string](), opt.None[string](), opt.None[string]()
	s.Deprecated, s.ReadOnly, s.WriteOnly = opt.None[bool](), opt.None[bool](), opt.None[bool]()
	s.Default, s.Examples, s.Unknown = nil, nil, nil

	return s
}

// withoutUnchecked is withoutAnnotations that also drops the validation
// keywords, which a schema whose keywords the generated code does not all
// check compares without.
func withoutUnchecked(s jsonschema.Schema) jsonschema.Schema {
	s = withoutAnnotations(s)

	s.MultipleOf, s.Maximum, s.ExclusiveMaximum = nil, nil, nil
	s.Minimum, s.ExclusiveMinimum = nil, nil
	s.MaxLength, s.MinLength, s.MaxItems, s.MinItems = nil, nil, nil, nil
	s.MaxContains, s.MinContains, s.MaxProperties, s.MinProperties = nil, nil, nil, nil
	s.Pattern, s.Format = opt.None[string](), opt.None[string]()
	s.ContentEncoding, s.ContentMediaType = opt.None[string](), opt.None[string]()
	s.UniqueItems, s.DependentRequired, s.ContentSchema = opt.None[bool](), nil, nil

	return s
}

// without returns a copy of s and of every schema under it that drop has
// removed keywords from. s is left as it is.
func without(
	s *jsonschema.Schema,
	drop func(jsonschema.Schema) jsonschema.Schema,
) *jsonschema.Schema {
	if s == nil {
		return nil
	}

	one := func(c *jsonschema.Schema) *jsonschema.Schema { return without(c, drop) }
	list := func(l []*jsonschema.Schema) []*jsonschema.Schema {
		if l == nil {
			return nil
		}

		return opiter.SlicesValues(l).
			Map(one).
			CollectSlice()
	}
	named := func(m jsonschema.SchemaMap) jsonschema.SchemaMap {
		if m == nil {
			return nil
		}

		return opiter.SlicesValues(m).
			Map(func(n jsonschema.NamedSchema) jsonschema.NamedSchema {
				return jsonschema.NamedSchema{Name: n.Name, Schema: one(n.Schema)}
			}).
			CollectSlice()
	}

	c := drop(*s)

	c.Defs, c.DependentSchemas = named(c.Defs), named(c.DependentSchemas)
	c.Properties, c.PatternProperties = named(c.Properties), named(c.PatternProperties)
	c.AllOf, c.AnyOf, c.OneOf, c.PrefixItems = list(c.AllOf), list(c.AnyOf), list(c.OneOf),
		list(c.PrefixItems)
	c.Not, c.If, c.Then, c.Else = one(c.Not), one(c.If), one(c.Then), one(c.Else)
	c.Items, c.Contains, c.AdditionalProperties = one(c.Items), one(c.Contains),
		one(c.AdditionalProperties)
	c.PropertyNames, c.ContentSchema = one(c.PropertyNames), one(c.ContentSchema)
	c.UnevaluatedItems, c.UnevaluatedProperties = one(c.UnevaluatedItems),
		one(c.UnevaluatedProperties)

	return &c
}
