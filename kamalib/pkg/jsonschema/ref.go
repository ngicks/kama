package jsonschema

import (
	"errors"
	"fmt"
	"iter"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/opt"
)

var (
	// ErrUnresolved reports a pointer or a fragment that names no schema of the document.
	ErrUnresolved = errors.New("jsonschema: unresolved reference")
	// ErrNonLocalRef reports a reference that does not start with "#".
	ErrNonLocalRef = errors.New("jsonschema: non-local reference")
)

// ResolveRef returns the schema of root that the same-document reference ref names.
//
// ref is "#", "#" followed by a JSON pointer, or "#" followed by a plain name that an $anchor or a
// $dynamicAnchor declares. The fragment is percent-decoded first. Anchor lookup does not descend
// into a subschema that declares $id, because the anchors there belong to another resource.
func ResolveRef(root *Schema, ref string) (*Schema, error) {
	frag, ok := strings.CutPrefix(ref, "#")
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNonLocalRef, ref)
	}

	frag, err := url.PathUnescape(frag)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrUnresolved, ref, err)
	}

	if frag == "" || strings.HasPrefix(frag, "/") {
		s, err := ResolvePointer(root, frag)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", ref, err)
		}

		return s, nil
	}

	if s := findAnchor(root, frag); s != nil {
		return s, nil
	}

	return nil, fmt.Errorf("%w: %q: no anchor %q", ErrUnresolved, ref, frag)
}

// ResolvePointer returns the schema of root at the RFC 6901 JSON pointer ptr.
//
// The pointer goes through schema-valued keywords only: a name under properties, $defs,
// patternProperties or dependentSchemas, an index under allOf, anyOf, oneOf or prefixItems, and
// the keywords that hold a single schema.
func ResolvePointer(root *Schema, ptr string) (*Schema, error) {
	if ptr == "" {
		return root, nil
	}

	rest, ok := strings.CutPrefix(ptr, "/")
	if !ok {
		return nil, fmt.Errorf("%w: pointer %q does not start with \"/\"", ErrUnresolved, ptr)
	}

	tokens := strings.Split(rest, "/")
	for i, tok := range tokens {
		t, err := unescapePointerToken(tok)
		if err != nil {
			return nil, fmt.Errorf("%w: pointer %q: %w", ErrUnresolved, ptr, err)
		}

		tokens[i] = t
	}

	cur := root
	for i := 0; i < len(tokens); {
		next, n := child(cur, tokens[i:])
		if next == nil {
			at := opiter.WrapSeq(slices.Values(tokens[:min(i+max(n, 1), len(tokens))])).
				Map(EscapePointerToken).
				CollectSlice()

			return nil, fmt.Errorf(
				"%w: pointer %q: no schema at %q", ErrUnresolved, ptr, "/"+strings.Join(at, "/"),
			)
		}

		cur, i = next, i+n
	}

	return cur, nil
}

// EscapePointerToken escapes s for use as one reference token of a JSON pointer.
func EscapePointerToken(s string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(s)
}

func unescapePointerToken(tok string) (string, error) {
	var b strings.Builder

	for i := 0; i < len(tok); i++ {
		if tok[i] != '~' {
			b.WriteByte(tok[i])
			continue
		}

		if i+1 == len(tok) || (tok[i+1] != '0' && tok[i+1] != '1') {
			return "", fmt.Errorf("invalid escape in token %q", tok)
		}

		if tok[i+1] == '0' {
			b.WriteByte('~')
		} else {
			b.WriteByte('/')
		}

		i++
	}

	return b.String(), nil
}

// child returns the subschema of s that the leading tokens name and the number of tokens the
// keyword takes. It returns nil when the tokens name no subschema.
func child(s *Schema, tokens []string) (*Schema, int) {
	if s == nil || s.Bool.IsSome() {
		return nil, 0
	}

	if m, ok := schemaMapOf(s, tokens[0]); ok {
		if len(tokens) < 2 {
			return nil, 2
		}

		c, _ := m.Get(tokens[1])

		return c, 2
	}

	if l, ok := schemaListOf(s, tokens[0]); ok {
		if len(tokens) < 2 {
			return nil, 2
		}

		i, ok := arrayIndex(tokens[1])
		if !ok || i >= len(l) {
			return nil, 2
		}

		return l[i], 2
	}

	return schemaOf(s, tokens[0]), 1
}

func schemaMapOf(s *Schema, keyword string) (SchemaMap, bool) {
	switch keyword {
	case "$defs":
		return s.Defs, true
	case "properties":
		return s.Properties, true
	case "patternProperties":
		return s.PatternProperties, true
	case "dependentSchemas":
		return s.DependentSchemas, true
	}

	return nil, false
}

func schemaListOf(s *Schema, keyword string) ([]*Schema, bool) {
	switch keyword {
	case "allOf":
		return s.AllOf, true
	case "anyOf":
		return s.AnyOf, true
	case "oneOf":
		return s.OneOf, true
	case "prefixItems":
		return s.PrefixItems, true
	}

	return nil, false
}

func schemaOf(s *Schema, keyword string) *Schema {
	switch keyword {
	case "not":
		return s.Not
	case "if":
		return s.If
	case "then":
		return s.Then
	case "else":
		return s.Else
	case "items":
		return s.Items
	case "contains":
		return s.Contains
	case "additionalProperties":
		return s.AdditionalProperties
	case "propertyNames":
		return s.PropertyNames
	case "unevaluatedItems":
		return s.UnevaluatedItems
	case "unevaluatedProperties":
		return s.UnevaluatedProperties
	case "contentSchema":
		return s.ContentSchema
	}

	return nil
}

// arrayIndex parses an RFC 6901 array index, which has no sign and no leading zero.
func arrayIndex(tok string) (int, bool) {
	if tok == "" || (len(tok) > 1 && tok[0] == '0') || tok[0] < '0' || tok[0] > '9' {
		return 0, false
	}

	i, err := strconv.Atoi(tok)

	return i, err == nil
}

func findAnchor(s *Schema, name string) *Schema {
	if s == nil {
		return nil
	}

	if s.Anchor == opt.Some(name) || s.DynamicAnchor == opt.Some(name) {
		return s
	}

	for c := range subschemas(s) {
		if c.Id.IsSome() {
			continue
		}

		if found := findAnchor(c, name); found != nil {
			return found
		}
	}

	return nil
}

// subschemas yields the direct subschemas of s in keyword order.
func subschemas(s *Schema) iter.Seq[*Schema] {
	return func(yield func(*Schema) bool) {
		for _, m := range []SchemaMap{s.Defs, s.DependentSchemas, s.Properties, s.PatternProperties} {
			for _, n := range m {
				if n.Schema != nil && !yield(n.Schema) {
					return
				}
			}
		}

		for _, l := range [][]*Schema{s.AllOf, s.AnyOf, s.OneOf, s.PrefixItems} {
			for _, c := range l {
				if c != nil && !yield(c) {
					return
				}
			}
		}

		single := []*Schema{
			s.Not, s.If, s.Then, s.Else, s.Items, s.Contains, s.AdditionalProperties,
			s.PropertyNames, s.UnevaluatedItems, s.UnevaluatedProperties, s.ContentSchema,
		}
		for _, c := range single {
			if c != nil && !yield(c) {
				return
			}
		}
	}
}
