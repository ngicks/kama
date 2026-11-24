package jsonschema

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/opt"
)

// Schema is a JSON Schema draft 2020-12 schema.
//
// A some Bool makes the schema the boolean schema true or false, and the other fields are then
// ignored on encoding.
//
// A nil jsontext.Value, a nil slice, a nil map and a none Option mark an absent keyword. A
// keyword written as null, [] or {} decodes into a non-nil value and encodes back.
type Schema struct {
	Bool opt.Option[bool] `json:"-"`

	// Core vocabulary.
	Schema        opt.Option[string] `json:"$schema,omitzero"`
	Id            opt.Option[string] `json:"$id,omitzero"`
	Vocabulary    map[string]bool    `json:"$vocabulary,omitzero"`
	Anchor        opt.Option[string] `json:"$anchor,omitzero"`
	DynamicAnchor opt.Option[string] `json:"$dynamicAnchor,omitzero"`
	Ref           opt.Option[string] `json:"$ref,omitzero"`
	DynamicRef    opt.Option[string] `json:"$dynamicRef,omitzero"`
	Defs          SchemaMap          `json:"$defs,omitzero"`
	Comment       opt.Option[string] `json:"$comment,omitzero"`

	// Applicator vocabulary.
	AllOf                []*Schema `json:"allOf,omitzero"`
	AnyOf                []*Schema `json:"anyOf,omitzero"`
	OneOf                []*Schema `json:"oneOf,omitzero"`
	Not                  *Schema   `json:"not,omitzero"`
	If                   *Schema   `json:"if,omitzero"`
	Then                 *Schema   `json:"then,omitzero"`
	Else                 *Schema   `json:"else,omitzero"`
	DependentSchemas     SchemaMap `json:"dependentSchemas,omitzero"`
	PrefixItems          []*Schema `json:"prefixItems,omitzero"`
	Items                *Schema   `json:"items,omitzero"`
	Contains             *Schema   `json:"contains,omitzero"`
	Properties           SchemaMap `json:"properties,omitzero"`
	PatternProperties    SchemaMap `json:"patternProperties,omitzero"`
	AdditionalProperties *Schema   `json:"additionalProperties,omitzero"`
	PropertyNames        *Schema   `json:"propertyNames,omitzero"`

	// Unevaluated vocabulary.
	UnevaluatedItems      *Schema `json:"unevaluatedItems,omitzero"`
	UnevaluatedProperties *Schema `json:"unevaluatedProperties,omitzero"`

	// Validation vocabulary. Numeric keywords keep their source text.
	Type              Types               `json:"type,omitzero"`
	Enum              []jsontext.Value    `json:"enum,omitzero"`
	Const             jsontext.Value      `json:"const,omitzero"`
	MultipleOf        jsontext.Value      `json:"multipleOf,omitzero"`
	Maximum           jsontext.Value      `json:"maximum,omitzero"`
	ExclusiveMaximum  jsontext.Value      `json:"exclusiveMaximum,omitzero"`
	Minimum           jsontext.Value      `json:"minimum,omitzero"`
	ExclusiveMinimum  jsontext.Value      `json:"exclusiveMinimum,omitzero"`
	MaxLength         jsontext.Value      `json:"maxLength,omitzero"`
	MinLength         jsontext.Value      `json:"minLength,omitzero"`
	Pattern           opt.Option[string]  `json:"pattern,omitzero"`
	MaxItems          jsontext.Value      `json:"maxItems,omitzero"`
	MinItems          jsontext.Value      `json:"minItems,omitzero"`
	UniqueItems       opt.Option[bool]    `json:"uniqueItems,omitzero"`
	MaxContains       jsontext.Value      `json:"maxContains,omitzero"`
	MinContains       jsontext.Value      `json:"minContains,omitzero"`
	MaxProperties     jsontext.Value      `json:"maxProperties,omitzero"`
	MinProperties     jsontext.Value      `json:"minProperties,omitzero"`
	Required          []string            `json:"required,omitzero"`
	DependentRequired map[string][]string `json:"dependentRequired,omitzero"`

	// Format annotation vocabulary.
	Format opt.Option[string] `json:"format,omitzero"`

	// Content vocabulary.
	ContentEncoding  opt.Option[string] `json:"contentEncoding,omitzero"`
	ContentMediaType opt.Option[string] `json:"contentMediaType,omitzero"`
	ContentSchema    *Schema            `json:"contentSchema,omitzero"`

	// Meta-data vocabulary.
	Title       opt.Option[string] `json:"title,omitzero"`
	Description opt.Option[string] `json:"description,omitzero"`
	Default     jsontext.Value     `json:"default,omitzero"`
	Deprecated  opt.Option[bool]   `json:"deprecated,omitzero"`
	ReadOnly    opt.Option[bool]   `json:"readOnly,omitzero"`
	WriteOnly   opt.Option[bool]   `json:"writeOnly,omitzero"`
	Examples    []jsontext.Value   `json:"examples,omitzero"`

	// Unknown holds the members that name no keyword above.
	Unknown map[string]jsontext.Value `json:",embed"`
}

// schemaObject drops the methods of Schema so that encoding it does not recurse.
type schemaObject Schema

// MarshalJSONTo implements [encoding/json/v2.MarshalerTo].
func (s Schema) MarshalJSONTo(enc *jsontext.Encoder) error {
	if s.Bool.IsSome() {
		return enc.WriteToken(jsontext.Bool(s.Bool.Value()))
	}

	return jsonv2.MarshalEncode(enc, schemaObject(s))
}

// UnmarshalJSONFrom implements [encoding/json/v2.UnmarshalerFrom].
func (s *Schema) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	switch k := dec.PeekKind(); k {
	case jsontext.KindTrue, jsontext.KindFalse:
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}

		*s = Schema{Bool: opt.Some(tok.Bool())}

		return nil
	case jsontext.KindBeginObject:
		var o schemaObject
		if err := jsonv2.UnmarshalDecode(dec, &o); err != nil {
			return err
		}

		*s = Schema(o)

		return nil
	default:
		if err := dec.SkipValue(); err != nil {
			return err
		}

		return fmt.Errorf("jsonschema: a schema must be an object or a boolean, got %v", k)
	}
}

// Types is the value of the type keyword.
//
// List reports whether the source wrote an array. A single name written as a string has List
// false.
type Types struct {
	Names []string
	List  bool
}

// IsZero reports whether the type keyword is absent.
func (t Types) IsZero() bool {
	return t.Names == nil && !t.List
}

// MarshalJSONTo implements [encoding/json/v2.MarshalerTo].
func (t Types) MarshalJSONTo(enc *jsontext.Encoder) error {
	if !t.List && len(t.Names) == 1 {
		return enc.WriteToken(jsontext.String(t.Names[0]))
	}

	names := t.Names
	if names == nil {
		names = []string{}
	}

	return jsonv2.MarshalEncode(enc, names)
}

// UnmarshalJSONFrom implements [encoding/json/v2.UnmarshalerFrom].
func (t *Types) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	switch k := dec.PeekKind(); k {
	case jsontext.KindString:
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}

		*t = Types{Names: []string{tok.String()}}

		return nil
	case jsontext.KindBeginArray:
		var names []string
		if err := jsonv2.UnmarshalDecode(dec, &names); err != nil {
			return err
		}

		*t = Types{Names: names, List: true}

		return nil
	default:
		if err := dec.SkipValue(); err != nil {
			return err
		}

		return fmt.Errorf("jsonschema: type must be a string or an array, got %v", k)
	}
}

// SchemaMap is the value of a keyword that maps names to schemas, such as properties and $defs.
// It keeps the members in source order.
type SchemaMap []NamedSchema

// NamedSchema is a member of a [SchemaMap].
type NamedSchema struct {
	Name   string
	Schema *Schema
}

// Get returns the schema of the first member named name.
func (m SchemaMap) Get(name string) (*Schema, bool) {
	n := opiter.WrapSeq(slices.Values(m)).
		Filter(func(n NamedSchema) bool { return n.Name == name }).
		First()

	return n.Value().Schema, n.IsSome()
}

// MarshalJSONTo implements [encoding/json/v2.MarshalerTo].
func (m SchemaMap) MarshalJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}

	for _, n := range m {
		if err := enc.WriteToken(jsontext.String(n.Name)); err != nil {
			return err
		}

		if err := jsonv2.MarshalEncode(enc, n.Schema); err != nil {
			return err
		}
	}

	return enc.WriteToken(jsontext.EndObject)
}

// UnmarshalJSONFrom implements [encoding/json/v2.UnmarshalerFrom].
func (m *SchemaMap) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if k := dec.PeekKind(); k != jsontext.KindBeginObject {
		if err := dec.SkipValue(); err != nil {
			return err
		}

		return fmt.Errorf("jsonschema: want an object of schemas, got %v", k)
	}

	if _, err := dec.ReadToken(); err != nil {
		return err
	}

	out := SchemaMap{}

	for dec.PeekKind() != jsontext.KindEndObject {
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}

		// The decoder voids tok on its next call.
		name := tok.String()

		s := new(Schema)
		if err := jsonv2.UnmarshalDecode(dec, s); err != nil {
			return err
		}

		out = append(out, NamedSchema{Name: name, Schema: s})
	}

	if _, err := dec.ReadToken(); err != nil {
		return err
	}

	*m = out

	return nil
}
