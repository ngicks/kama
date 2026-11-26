package jsonschema

import (
	"maps"
	"slices"
	"strings"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/opt"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// vocabularyPrefix starts the URI of every vocabulary of draft 2020-12.
const vocabularyPrefix = "https://json-schema.org/draft/2020-12/vocab/"

// vocabularies are the vocabularies of draft 2020-12 the generator
// implements. format-assertion is left out, since the generated code
// asserts no format.
var vocabularies = []string{
	"core", "applicator", "unevaluated", "validation", "meta-data", "format-annotation",
	"content",
}

// withVocabularies returns doc as the meta-schema its root names by
// $schema reads it. A meta-schema other than that of draft 2020-12 is
// loaded through Options.Load and must declare $vocabulary. A vocabulary
// it requires must be one of draft 2020-12 the generator implements, and
// an optional vocabulary the generator does not know is ignored. The
// keywords of a vocabulary of draft 2020-12 the meta-schema leaves out are
// unknown keywords, which assert nothing, so the result holds none of
// them. The annotation vocabularies assert nothing anyway.
//
// The result is a document of its own, not a copy of facts derived from
// doc: the generator reads it as its only source, as it reads the result
// of bundle.
func withVocabularies(doc *jsonschema.Schema, opts Options) (*jsonschema.Schema, error) {
	// check reports a nil document.
	if doc == nil {
		return doc, nil
	}

	uri, ok := doc.Schema.Unpack()
	if !ok || doc.Bool.IsSome() || strings.TrimSuffix(uri, "#") == metaSchema {
		return doc, nil
	}

	kw := child("", "$schema")

	if opts.Load == nil {
		return nil, unsupported(kw, "meta-schema %q is not that of draft 2020-12", uri)
	}

	meta, err := opts.Load(strings.TrimSuffix(uri, "#"))
	if err != nil {
		return nil, unsupported(kw, "meta-schema %q: %v", uri, err)
	}

	if meta.Vocabulary == nil {
		return nil, unsupported(kw, "meta-schema %q declares no $vocabulary", uri)
	}

	for _, v := range slices.Sorted(maps.Keys(meta.Vocabulary)) {
		name, std := strings.CutPrefix(v, vocabularyPrefix)
		if meta.Vocabulary[v] && (!std || !slices.Contains(vocabularies, name)) {
			return nil, unsupported(kw, "meta-schema %q requires vocabulary %q", uri, v)
		}
	}

	dropped := opiter.SlicesValues([]string{"applicator", "unevaluated", "validation"}).
		Filter(func(name string) bool {
			_, ok := meta.Vocabulary[vocabularyPrefix+name]
			return !ok
		}).
		CollectSlice()
	if len(dropped) == 0 {
		return doc, nil
	}

	return withoutVocabularies(doc, dropped), nil
}

// withoutVocabularies returns a copy of s and of its subschemas holding no
// keyword of the vocabularies dropped names: applicator, unevaluated or
// validation.
func withoutVocabularies(s *jsonschema.Schema, dropped []string) *jsonschema.Schema {
	if s == nil {
		return nil
	}

	c := *s

	if slices.Contains(dropped, "validation") {
		c.Type, c.Enum, c.Const = jsonschema.Types{}, nil, nil
		c.MultipleOf, c.Maximum, c.ExclusiveMaximum = nil, nil, nil
		c.Minimum, c.ExclusiveMinimum = nil, nil
		c.MaxLength, c.MinLength, c.Pattern = nil, nil, opt.None[string]()
		c.MaxItems, c.MinItems, c.UniqueItems = nil, nil, opt.None[bool]()
		c.MaxContains, c.MinContains, c.MaxProperties, c.MinProperties = nil, nil, nil, nil
		c.Required, c.DependentRequired = nil, nil
	}

	if slices.Contains(dropped, "applicator") {
		c.AllOf, c.AnyOf, c.OneOf, c.Not, c.If, c.Then, c.Else = nil, nil, nil, nil, nil, nil, nil
		c.DependentSchemas, c.PrefixItems, c.Items, c.Contains = nil, nil, nil, nil
		c.Properties, c.PatternProperties, c.AdditionalProperties, c.PropertyNames = nil, nil, nil, nil
	}

	if slices.Contains(dropped, "unevaluated") {
		c.UnevaluatedItems, c.UnevaluatedProperties = nil, nil
	}

	without := func(c *jsonschema.Schema) *jsonschema.Schema {
		return withoutVocabularies(c, dropped)
	}

	for _, m := range []*jsonschema.SchemaMap{
		&c.Defs, &c.DependentSchemas, &c.Properties, &c.PatternProperties,
	} {
		if *m != nil {
			*m = opiter.SlicesValues(*m).
				Map(func(n jsonschema.NamedSchema) jsonschema.NamedSchema {
					return jsonschema.NamedSchema{Name: n.Name, Schema: without(n.Schema)}
				}).
				CollectSlice()
		}
	}

	for _, l := range []*[]*jsonschema.Schema{&c.AllOf, &c.AnyOf, &c.OneOf, &c.PrefixItems} {
		if *l != nil {
			*l = opiter.SlicesValues(*l).
				Map(without).
				CollectSlice()
		}
	}

	for _, p := range []**jsonschema.Schema{
		&c.Not, &c.If, &c.Then, &c.Else, &c.Items, &c.Contains, &c.AdditionalProperties,
		&c.PropertyNames, &c.UnevaluatedItems, &c.UnevaluatedProperties, &c.ContentSchema,
	} {
		*p = without(*p)
	}

	return &c
}

// assertedFormats are the formats of draft 2020-12 the default format
// helpers of the validator generator check. Their names are the same in
// both.
var assertedFormats = []string{"date", "date-time", "ipv4", "ipv6", "uuid"}

// assertedFormat returns the format of s that opts asserts, or "".
func assertedFormat(opts Options, s *jsonschema.Schema) string {
	if f := s.Format.UnwrapOr(""); opts.AssertFormats && slices.Contains(assertedFormats, f) {
		return f
	}

	return ""
}
