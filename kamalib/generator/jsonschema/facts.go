package jsonschema

import (
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/opt"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// The functions of this file answer questions about the schema at a JSON
// pointer of the document. A pointer is RFC 6901 text without the leading
// "#", so the root is "". Every answer walks the document again.

// allKinds are the JSON types a schema without a type restriction admits,
// in the order of the JSON Schema specification. "integer" is a subset of
// "number" and stays out.
var allKinds = []string{"null", "boolean", "object", "array", "number", "string"}

// shapeKeywords change the Go type of a schema. Every other keyword the
// generator accepts annotates or validates without changing it.
var shapeKeywords = []string{
	"$ref", "type", "enum", "const", "properties", "required", "additionalProperties",
	"items", "allOf", "anyOf", "oneOf",
}

func unsupported(ptr, format string, args ...any) error {
	return fmt.Errorf("%w: #%s: %s", ErrUnsupported, ptr, fmt.Sprintf(format, args...))
}

// at returns the schema at ptr.
func at(doc *jsonschema.Schema, ptr string) (*jsonschema.Schema, error) {
	s, err := jsonschema.ResolvePointer(doc, ptr)
	if err != nil {
		return nil, unsupported(ptr, "%v", err)
	}

	return s, nil
}

// child returns the pointer of the schema under ptr that keys name.
func child(ptr string, keys ...string) string {
	for _, k := range keys {
		ptr += "/" + jsonschema.EscapePointerToken(k)
	}

	return ptr
}

// schemas yields every schema of doc the generator reads with its pointer:
// the root and, depth first, the schemas under properties, the schema of
// additionalProperties, items, the members of oneOf, anyOf and allOf, then
// those under $defs. No reference is followed, so each schema comes once.
// A nil schema comes as it is, with nothing under it.
func schemas(doc *jsonschema.Schema) iter.Seq2[string, *jsonschema.Schema] {
	return func(yield func(string, *jsonschema.Schema) bool) {
		visit("", doc, yield)
	}
}

func visit(ptr string, s *jsonschema.Schema, yield func(string, *jsonschema.Schema) bool) bool {
	if !yield(ptr, s) {
		return false
	}

	if s == nil {
		return true
	}

	for _, n := range s.Properties {
		if !visit(child(ptr, "properties", n.Name), n.Schema, yield) {
			return false
		}
	}

	if s.AdditionalProperties != nil && s.AdditionalProperties.Bool.IsNone() {
		if !visit(child(ptr, "additionalProperties"), s.AdditionalProperties, yield) {
			return false
		}
	}

	if s.Items != nil && !visit(child(ptr, "items"), s.Items, yield) {
		return false
	}

	for _, kw := range []string{"oneOf", "anyOf", "allOf"} {
		for i, m := range listOf(s, kw) {
			if !visit(child(ptr, kw, strconv.Itoa(i)), m, yield) {
				return false
			}
		}
	}

	for _, n := range s.Defs {
		if !visit(child(ptr, "$defs", n.Name), n.Schema, yield) {
			return false
		}
	}

	return true
}

func listOf(s *jsonschema.Schema, keyword string) []*jsonschema.Schema {
	switch keyword {
	case "oneOf":
		return s.OneOf
	case "anyOf":
		return s.AnyOf
	case "allOf":
		return s.AllOf
	}

	return nil
}

// present returns the keywords of keywords that s holds.
func present(s *jsonschema.Schema, keywords []string) []string {
	has := map[string]bool{
		"$ref":                  s.Ref.IsSome(),
		"type":                  !s.Type.IsZero(),
		"enum":                  s.Enum != nil,
		"const":                 s.Const != nil,
		"properties":            s.Properties != nil,
		"required":              s.Required != nil,
		"additionalProperties":  s.AdditionalProperties != nil,
		"items":                 s.Items != nil,
		"allOf":                 s.AllOf != nil,
		"anyOf":                 s.AnyOf != nil,
		"oneOf":                 s.OneOf != nil,
		"if":                    s.If != nil,
		"then":                  s.Then != nil,
		"else":                  s.Else != nil,
		"not":                   s.Not != nil,
		"prefixItems":           s.PrefixItems != nil,
		"patternProperties":     s.PatternProperties != nil,
		"dependentSchemas":      s.DependentSchemas != nil,
		"unevaluatedItems":      s.UnevaluatedItems != nil,
		"unevaluatedProperties": s.UnevaluatedProperties != nil,
		"contains":              s.Contains != nil,
		"propertyNames":         s.PropertyNames != nil,
		"$dynamicRef":           s.DynamicRef.IsSome(),
	}

	return opiter.WrapSeq(slices.Values(keywords)).
		Filter(func(kw string) bool { return has[kw] }).
		CollectSlice()
}

// shapesOtherThan returns the shape keywords of s outside allowed.
func shapesOtherThan(s *jsonschema.Schema, allowed ...string) []string {
	return opiter.WrapSeq(slices.Values(present(s, shapeKeywords))).
		Filter(func(kw string) bool { return !slices.Contains(allowed, kw) }).
		CollectSlice()
}

// refTarget returns the pointer of the schema the $ref of the schema at
// ptr names.
func refTarget(doc *jsonschema.Schema, ptr string) (string, error) {
	s, err := at(doc, ptr)
	if err != nil {
		return "", err
	}

	ref := s.Ref.Value()
	kw := child(ptr, "$ref")

	frag, ok := strings.CutPrefix(ref, "#")
	if !ok {
		return "", unsupported(kw, "reference %q leaves the document", ref)
	}

	frag, err = url.PathUnescape(frag)
	if err != nil {
		return "", unsupported(kw, "reference %q: %v", ref, err)
	}

	if frag != "" && !strings.HasPrefix(frag, "/") {
		found := opiter.WrapSeq2(schemas(doc)).
			Filter(func(_ string, c *jsonschema.Schema) bool {
				return c.Anchor == opt.Some(frag) || c.DynamicAnchor == opt.Some(frag)
			}).
			OmitL().
			First()
		if found.IsNone() {
			return "", unsupported(kw, "reference %q names no anchor", ref)
		}

		return found.Value(), nil
	}

	target, err := normalPointer(frag)
	if err != nil {
		return "", unsupported(kw, "reference %q: %v", ref, err)
	}

	if !yielded(doc, target) {
		return "", unsupported(kw, "reference %q names no schema the generator reads", ref)
	}

	return target, nil
}

// yielded reports whether schemas yields ptr, a pointer spelled as child
// spells it. It walks the tokens of ptr through the keywords visit
// descends into rather than through every pointer of doc, which would make
// each reference cost as much as the document.
func yielded(doc *jsonschema.Schema, ptr string) bool {
	cur := doc
	toks := strings.Split(ptr, "/")[1:]

	for i := 0; i < len(toks); i++ {
		if cur == nil {
			return false
		}

		kw := unescape(toks[i])

		switch kw {
		case "additionalProperties":
			if !extraMembers(cur) {
				return false
			}

			cur = cur.AdditionalProperties

			continue
		case "items":
			if cur.Items == nil {
				return false
			}

			cur = cur.Items

			continue
		}

		if i+1 == len(toks) {
			return false
		}

		i++
		key := unescape(toks[i])

		switch kw {
		case "properties", "$defs":
			m := cur.Properties
			if kw == "$defs" {
				m = cur.Defs
			}

			c, ok := m.Get(key)
			if !ok {
				return false
			}

			cur = c
		case "oneOf", "anyOf", "allOf":
			l := listOf(cur, kw)

			n, err := strconv.Atoi(key)
			if err != nil || strconv.Itoa(n) != key || n < 0 || n >= len(l) {
				return false
			}

			cur = l[n]
		default:
			return false
		}
	}

	return true
}

// normalPointer escapes ptr the way child does, so that two spellings of
// one pointer compare equal.
func normalPointer(ptr string) (string, error) {
	if ptr == "" {
		return "", nil
	}

	var out string

	for tok := range strings.SplitSeq(strings.TrimPrefix(ptr, "/"), "/") {
		if strings.Contains(strings.ReplaceAll(strings.ReplaceAll(tok, "~0", ""), "~1", ""), "~") {
			return "", fmt.Errorf("invalid escape in %q", tok)
		}

		out = child(out, unescape(tok))
	}

	return out, nil
}

// resolved returns the pointer of the schema that gives the Go type of the
// schema at ptr, and that schema: it follows $ref and an allOf of one
// member.
func resolved(doc *jsonschema.Schema, ptr string) (string, *jsonschema.Schema, error) {
	s, err := at(doc, ptr)
	if err != nil {
		return "", nil, err
	}

	return resolvedFrom(doc, ptr, s)
}

// resolvedFrom is resolved for s, the schema at ptr, which a caller holding
// it passes to spare walking ptr again.
func resolvedFrom(
	doc *jsonschema.Schema,
	ptr string,
	s *jsonschema.Schema,
) (string, *jsonschema.Schema, error) {
	var seen []string

	for {
		if slices.Contains(seen, ptr) {
			return "", nil, unsupported(ptr, "references itself without a type in between")
		}

		seen = append(seen, ptr)

		var err error

		switch {
		case s.Ref.IsSome():
			ptr, err = refTarget(doc, ptr)
		case transparentAllOf(s):
			ptr = child(ptr, "allOf", "0")
		default:
			return ptr, s, nil
		}

		if err != nil {
			return "", nil, err
		}

		s, err = at(doc, ptr)
		if err != nil {
			return "", nil, err
		}
	}
}

// transparentAllOf reports whether s is an allOf of one member whose type
// it keeps.
func transparentAllOf(s *jsonschema.Schema) bool {
	return len(s.AllOf) == 1 && len(shapesOtherThan(s, "allOf", "type")) == 0 &&
		!slices.Contains(s.Type.Names, "null")
}

// unionKeyword returns "oneOf" or "anyOf", whichever s holds, or "".
func unionKeyword(s *jsonschema.Schema) string {
	switch {
	case s.OneOf != nil:
		return "oneOf"
	case s.AnyOf != nil:
		return "anyOf"
	}

	return ""
}

// typeKinds returns the names of the type keyword of s other than null.
func typeKinds(s *jsonschema.Schema) []string {
	return opiter.WrapSeq(slices.Values(s.Type.Names)).
		Filter(func(k string) bool { return k != "null" }).
		CollectSlice()
}

// multiKind reports whether the type keyword of s names two or more kinds
// other than null, which make a union of those kinds.
func multiKind(s *jsonschema.Schema) bool { return len(typeKinds(s)) >= 2 }

// nullableType reports whether the type keyword of s names null and one
// other kind.
func nullableType(s *jsonschema.Schema) bool {
	return slices.Contains(s.Type.Names, "null") && len(typeKinds(s)) == 1
}

// nullMember reports whether the schema at ptr admits null only.
func nullMember(doc *jsonschema.Schema, ptr string) bool {
	_, s, err := resolved(doc, ptr)

	return err == nil && slices.Equal(s.Type.Names, []string{"null"}) &&
		len(shapesOtherThan(s, "type")) == 0
}

// nullableUnion reports whether the schema at ptr is a oneOf or an anyOf of
// one schema and a schema admitting null only, which make a nullable value
// of that schema. In a oneOf the other schema must not admit null, or null
// would match both members.
func nullableUnion(doc *jsonschema.Schema, ptr string) bool {
	s, err := at(doc, ptr)
	if err != nil {
		return false
	}

	kw := unionKeyword(s)
	members := listOf(s, kw)

	if len(members) != 2 || len(shapesOtherThan(s, kw, "type")) > 0 {
		return false
	}

	nulls := opiter.WrapSeq(slices.Values([]int{0, 1})).
		Filter(func(i int) bool { return nullMember(doc, child(ptr, kw, strconv.Itoa(i))) }).
		CollectSlice()
	if len(nulls) != 1 {
		return false
	}

	if kw == "anyOf" {
		return true
	}

	k, err := kinds(doc, child(ptr, kw, strconv.Itoa(1-nulls[0])))

	return err == nil && !slices.Contains(k, "null")
}

// valueMember returns the pointer of the member of the nullable union at
// ptr that does not admit null only.
func valueMember(doc *jsonschema.Schema, ptr string) string {
	s, _ := at(doc, ptr)
	kw := unionKeyword(s)

	if nullMember(doc, child(ptr, kw, "0")) {
		return child(ptr, kw, "1")
	}

	return child(ptr, kw, "0")
}

// resolvedValue is resolved for a schema that is no nullable union. For a
// nullable union it gives the schema of its value instead, which holds the
// const or the enum the value is checked against.
func resolvedValue(doc *jsonschema.Schema, ptr string) (string, *jsonschema.Schema, error) {
	var seen []string

	for {
		r, s, err := resolved(doc, ptr)
		if err != nil || !nullableUnion(doc, r) {
			return r, s, err
		}

		if slices.Contains(seen, r) {
			return "", nil, unsupported(r, "is a nullable value of itself")
		}

		seen = append(seen, r)
		ptr = valueMember(doc, r)
	}
}

// nullable reports whether the schema at ptr is a nullable value of
// another schema: a type naming null and one other kind, or a nullable
// union.
func nullable(doc *jsonschema.Schema, ptr string) bool {
	r, s, err := resolved(doc, ptr)

	return err == nil && (nullableType(s) || nullableUnion(doc, r))
}

// acceptsNull reports whether the Go type of the schema at ptr holds null
// itself: a schema of any kind, or a union with a member admitting null.
func acceptsNull(doc *jsonschema.Schema, ptr string) bool {
	k, err := kinds(doc, ptr)

	return err == nil && !nullable(doc, ptr) && slices.Contains(k, "null")
}

// objectKind reports whether s describes an object by its type keyword or,
// without one, by the keywords it holds.
func objectKind(s *jsonschema.Schema) bool {
	if !s.Type.IsZero() {
		return slices.Equal(typeKinds(s), []string{"object"})
	}

	return s.Properties != nil || s.Required != nil || s.AdditionalProperties != nil
}

// mapOnly reports whether the object s describes is a map: it declares no
// property and gives additionalProperties a schema.
func mapOnly(s *jsonschema.Schema) bool {
	return s.Properties == nil && s.Required == nil && s.AdditionalProperties != nil &&
		s.AdditionalProperties.Bool.IsNone()
}

// stringEnum reports whether s is an enum of strings, which makes a named
// string type.
func stringEnum(s *jsonschema.Schema) bool {
	return len(s.Enum) > 0 && opiter.WrapSeq(slices.Values(s.Enum)).
		Every(func(v jsontext.Value) bool { return v.Kind() == '"' })
}

// declares reports whether the schema at ptr declares a Go type of its
// own: a struct, a union, a product of allOf or a string enum. A
// reference, an allOf of one member and a nullable union take the type of
// another schema, and the other schemas are spelled by built-in types.
func declares(doc *jsonschema.Schema, ptr string) bool {
	s, err := at(doc, ptr)
	if err != nil || s.Bool.IsSome() || s.Ref.IsSome() || transparentAllOf(s) ||
		nullableUnion(doc, ptr) {
		return false
	}

	return union(s) || s.AllOf != nil || stringEnum(s) || structKind(s)
}

// union reports whether s holds a oneOf, an anyOf or a type of two or more
// kinds. It does not tell a nullable union apart, which declares no union:
// callers that need the difference ask nullableUnion, as declares does.
func union(s *jsonschema.Schema) bool { return unionKeyword(s) != "" || multiKind(s) }

// product reports whether s is an allOf of members that each give a field.
func product(s *jsonschema.Schema) bool { return s.AllOf != nil && !transparentAllOf(s) }

// structKind reports whether s is an object that becomes a struct.
func structKind(s *jsonschema.Schema) bool {
	return s.Enum == nil && s.Const == nil && s.AllOf == nil && !union(s) && objectKind(s) &&
		!mapOnly(s)
}

// kinds returns the JSON types the schema at ptr admits, in the order its
// keywords give them. "integer" stands for itself.
func kinds(doc *jsonschema.Schema, ptr string) ([]string, error) {
	return kindsSeen(doc, ptr, nil, map[string]bool{})
}

// kindsSeen is kinds for a schema reached through the unions of onPath.
// done holds the unions the walk has finished, which it skips: their kinds
// came earlier in the walk, so the result stays the same, while walking
// them again for every path to them takes time exponential in the depth of
// unions sharing members.
func kindsSeen(
	doc *jsonschema.Schema,
	ptr string,
	onPath []string,
	done map[string]bool,
) ([]string, error) {
	r, s, err := resolved(doc, ptr)
	if err != nil {
		return nil, err
	}

	if slices.Contains(onPath, r) {
		return nil, unsupported(r, "is a member of itself")
	}

	if done[r] {
		return nil, nil
	}

	onPath = append(onPath, r)

	switch {
	case s.Bool.IsSome():
		if s.Bool.Value() {
			return allKinds, nil
		}

		return nil, nil
	case !s.Type.IsZero():
		return slices.Compact(slices.Clone(s.Type.Names)), nil
	case s.Const != nil:
		return []string{valueKind(s.Const)}, nil
	case s.Enum != nil:
		return slices.Collect(opiter.Unique(
			opiter.WrapSeq(slices.Values(s.Enum)).
				Map(valueKind),
		)), nil
	case unionKeyword(s) != "":
		kw := unionKeyword(s)

		var out []string

		for i := range listOf(s, kw) {
			k, err := kindsSeen(doc, child(r, kw, strconv.Itoa(i)), onPath, done)
			if err != nil {
				return nil, err
			}

			out = append(out, k...)
		}

		done[r] = true

		return slices.Collect(opiter.Unique(slices.Values(out))), nil
	case s.AllOf != nil || objectKind(s):
		return []string{"object"}, nil
	case s.Items != nil:
		return []string{"array"}, nil
	}

	return allKinds, nil
}

// valueKind returns the JSON type of v. A number without a fraction or an
// exponent is "integer".
func valueKind(v jsontext.Value) string {
	switch v.Kind() {
	case 'n':
		return "null"
	case 't', 'f':
		return "boolean"
	case '"':
		return "string"
	case '{':
		return "object"
	case '[':
		return "array"
	}

	if _, err := strconv.ParseInt(string(v), 10, 64); err == nil {
		return "integer"
	}

	return "number"
}

// required reports whether the object at ptr requires the member name.
func required(doc *jsonschema.Schema, ptr, name string) bool {
	s, err := at(doc, ptr)

	return err == nil && slices.Contains(s.Required, name)
}

// closed reports whether the object s rejects members it does not declare.
func closed(s *jsonschema.Schema) bool {
	return s.AdditionalProperties != nil && s.AdditionalProperties.Bool == opt.Some(false)
}

// extraMembers reports whether the object s keeps the members it does not
// declare in a map of the schema of additionalProperties.
func extraMembers(s *jsonschema.Schema) bool {
	return s.AdditionalProperties != nil && s.AdditionalProperties.Bool.IsNone()
}

// structOf returns the pointer of the struct or product the schema s at
// ptr holds by value, and that struct or product: itself, through
// references and nullable unions.
func structOf(
	doc *jsonschema.Schema,
	ptr string,
	s *jsonschema.Schema,
) (string, *jsonschema.Schema, bool) {
	var seen []string

	for !slices.Contains(seen, ptr) {
		seen = append(seen, ptr)

		r, rs, err := resolvedFrom(doc, ptr, s)
		switch {
		case err != nil:
			return "", nil, false
		case structKind(rs) || product(rs):
			return r, rs, true
		// nullableUnion walks r again, which only a union needs.
		case unionKeyword(rs) == "" || !nullableUnion(doc, r):
			return "", nil, false
		}

		ptr = valueMember(doc, r)

		s, err = at(doc, ptr)
		if err != nil {
			return "", nil, false
		}
	}

	return "", nil, false
}

// byValue yields the structs and products the struct or product s at ptr
// holds by value, with their pointers, in the order of its fields.
func byValue(
	doc *jsonschema.Schema,
	ptr string,
	s *jsonschema.Schema,
) iter.Seq2[string, *jsonschema.Schema] {
	return func(yield func(string, *jsonschema.Schema) bool) {
		for i, m := range s.AllOf {
			if m == nil {
				continue
			}

			t, ts, ok := structOf(doc, child(ptr, "allOf", strconv.Itoa(i)), m)
			if ok && !yield(t, ts) {
				return
			}
		}

		for _, n := range s.Properties {
			if n.Schema == nil {
				continue
			}

			t, ts, ok := structOf(doc, child(ptr, "properties", n.Name), n.Schema)
			if ok && !yield(t, ts) {
				return
			}
		}
	}
}

// onCycle reports whether the field at ptr of the struct at owner closes a
// cycle of structs held by value, which Go cannot lay out: following the
// fields held by value from the type of the field reaches owner again.
// Every field on such a cycle becomes a pointer.
//
// The walk carries the schema of each pointer with it. Finding the schema
// of a pointer again costs the length of the pointer, which made the walk
// cubic in the depth of nested objects.
func onCycle(doc *jsonschema.Schema, owner, ptr string) bool {
	s, err := at(doc, ptr)
	if err != nil {
		return false
	}

	start, ss, ok := structOf(doc, ptr, s)
	if !ok {
		return false
	}

	seen := map[string]bool{}
	ptrs, nodes := []string{start}, []*jsonschema.Schema{ss}

	for len(ptrs) > 0 {
		cur, cs := ptrs[len(ptrs)-1], nodes[len(nodes)-1]
		ptrs, nodes = ptrs[:len(ptrs)-1], nodes[:len(nodes)-1]

		if cur == owner {
			return true
		}

		if seen[cur] {
			continue
		}

		seen[cur] = true

		for t, ts := range byValue(doc, cur, cs) {
			ptrs, nodes = append(ptrs, t), append(nodes, ts)
		}
	}

	return false
}
