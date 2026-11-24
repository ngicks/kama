package jsonschema

import (
	"errors"
	"fmt"
	"iter"
	"net/url"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/opt"
)

var (
	// ErrUnresolved reports a pointer or a fragment that names no schema of the document.
	ErrUnresolved = errors.New("jsonschema: unresolved reference")
	// ErrNonLocalRef reports a reference whose URI names no schema resource of the document.
	ErrNonLocalRef = errors.New("jsonschema: non-local reference")
)

// ResolveRef returns the pointer of the schema of root that ref, a URI reference written in the
// schema at ptr, names. uri is the retrieval URI of root, which may be empty.
//
// ref resolves against the base URI of the schema at ptr, as BaseUri tells, and the result
// names a schema as Locate tells.
func ResolveRef(root *Schema, uri, ptr, ref string) (string, error) {
	// A fragment alone names a schema of the resource holding ptr, which
	// the walk along ptr finds without resolving a URI or visiting every
	// resource of the document. A generator resolves references this way
	// for every question it asks.
	if frag, ok := strings.CutPrefix(ref, "#"); ok {
		res, _, err := enclosing(root, uri, ptr)
		if err != nil {
			return "", err
		}

		return locateFragment(root, res, ref, frag)
	}

	target, err := RefUri(root, uri, ptr, ref)
	if err != nil {
		return "", err
	}

	return Locate(root, uri, target)
}

// RefUri returns ref, a URI reference written in the schema at ptr of root, resolved against
// the base URI of that schema, as BaseUri tells. The result keeps the fragment of ref.
func RefUri(root *Schema, uri, ptr, ref string) (string, error) {
	base, err := BaseUri(root, uri, ptr)
	if err != nil {
		return "", err
	}

	return resolveUri(base, ref)
}

// BaseUri returns the base URI of the schema at ptr of root, whose retrieval URI is uri. Each
// $id from the root down to the schema resolves against the base URI of the schema above it,
// the first one against uri, as RFC 3986 resolves a reference. The result has no fragment. An
// empty uri leaves a document without an absolute $id with a relative base URI, against which
// references still resolve alike.
func BaseUri(root *Schema, uri, ptr string) (string, error) {
	_, base, err := enclosing(root, uri, ptr)

	return base, err
}

// Locate returns the pointer of the schema of root that target, a URI with an optional
// fragment, names. uri is the retrieval URI of root.
//
// The URI without its fragment names a schema resource of root, as Resources yields it. An
// empty fragment names the resource itself. A fragment starting with "/" is a JSON pointer from
// the resource, which may go into a resource embedded in it. Any other fragment is a plain name
// that an $anchor or a $dynamicAnchor of the resource declares, outside the resources embedded
// in it. The fragment is percent-decoded first. A URI naming no resource fails with
// [ErrNonLocalRef], and a fragment naming no schema with [ErrUnresolved].
func Locate(root *Schema, uri, target string) (string, error) {
	u, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrUnresolved, target, err)
	}

	frag := u.EscapedFragment()
	u.Fragment, u.RawFragment = "", ""

	res, ok := opiter.WrapSeq2(Resources(root, uri)).
		Filter(func(id, _ string) bool { return id == u.String() }).
		OmitF().
		First().
		Unpack()
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrNonLocalRef, target)
	}

	return locateFragment(root, res, target, frag)
}

// Resources yields the absolute URI and the pointer of every schema resource of root, whose
// retrieval URI is uri: the root, then each schema declaring $id, depth first. The URI of a
// resource is its base URI, as BaseUri tells. A keyword that holds no schema, such as enum or
// an unknown keyword, holds no resource, whatever $id its value spells.
func Resources(root *Schema, uri string) iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		base, err := resolveId(uri, root.Id.UnwrapOr(""))
		if err != nil {
			return
		}

		if !yield(base, "") {
			return
		}

		var visit func(s *Schema, ptr, base string) bool

		visit = func(s *Schema, ptr, base string) bool {
			for step, c := range children(s) {
				cp, cb := ptr+step, base

				if c.Id.IsSome() {
					if cb, err = resolveId(base, c.Id.Value()); err != nil {
						continue
					}

					if !yield(cb, cp) {
						return false
					}
				}

				if !visit(c, cp, cb) {
					return false
				}
			}

			return true
		}

		visit(root, "", base)
	}
}

// ResourcesAlong returns the pointers of the schema resources holding the schema at ptr of root,
// from the root down: the root and each schema declaring $id on the way, the schema at ptr
// included. The last one is the resource the schema at ptr belongs to.
func ResourcesAlong(root *Schema, ptr string) ([]string, error) {
	var along []string

	_, err := walkPointer(root, ptr, func(at string, s *Schema) error {
		if at == "" || s.Id.IsSome() {
			along = append(along, at)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return along, nil
}

// DynamicAnchor returns the pointer of the schema of the resource at res of root that declares
// the $dynamicAnchor name, outside the resources embedded in it. An $anchor of that name does
// not count, as JSON Schema resolves a $dynamicRef in the dynamic scope by $dynamicAnchor only.
func DynamicAnchor(root *Schema, res, name string) (string, bool) {
	s, err := ResolvePointer(root, res)
	if err != nil {
		return "", false
	}

	var find func(s *Schema, ptr string) (string, bool)

	find = func(s *Schema, ptr string) (string, bool) {
		if s.DynamicAnchor == opt.Some(name) {
			return ptr, true
		}

		for step, c := range children(s) {
			if c.Id.IsNone() {
				if found, ok := find(c, ptr+step); ok {
					return found, true
				}
			}
		}

		return "", false
	}

	return find(s, res)
}

// enclosing returns the pointer of the schema resource holding the schema at ptr of root and
// the base URI of that schema.
func enclosing(root *Schema, uri, ptr string) (string, string, error) {
	base, err := resolveId(uri, root.Id.UnwrapOr(""))
	if err != nil {
		return "", "", fmt.Errorf("%w: $id of the root: %w", ErrUnresolved, err)
	}

	res := ""

	_, err = walkPointer(root, ptr, func(at string, s *Schema) error {
		if at == "" || s.Id.IsNone() {
			return nil
		}

		b, err := resolveId(base, s.Id.Value())
		if err != nil {
			return fmt.Errorf("%w: $id at %q: %w", ErrUnresolved, at, err)
		}

		res, base = at, b

		return nil
	})
	if err != nil {
		return "", "", err
	}

	return res, base, nil
}

// locateFragment returns the pointer of the schema that frag, the escaped fragment of ref,
// names in the resource at res.
func locateFragment(root *Schema, res, ref, frag string) (string, error) {
	frag, err := url.PathUnescape(frag)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrUnresolved, ref, err)
	}

	if frag == "" || strings.HasPrefix(frag, "/") {
		if _, err := ResolvePointer(root, res+frag); err != nil {
			return "", fmt.Errorf("%q: %w", ref, err)
		}

		return res + frag, nil
	}

	resource, err := ResolvePointer(root, res)
	if err != nil {
		return "", fmt.Errorf("%q: %w", ref, err)
	}

	if ptr, ok := findAnchor(resource, res, frag); ok {
		return ptr, nil
	}

	return "", fmt.Errorf("%w: %q: no anchor %q", ErrUnresolved, ref, frag)
}

// resolveId resolves the $id id against base and drops its fragment, which the base URI of a
// resource does not hold.
func resolveId(base, id string) (string, error) {
	u, err := resolveUri(base, id)
	if err != nil {
		return "", err
	}

	u, _, _ = strings.Cut(u, "#")

	return u, nil
}

// resolveUri resolves ref against base as RFC 3986 tells.
func resolveUri(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}

	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}

	return b.ResolveReference(r).String(), nil
}

// ResolvePointer returns the schema of root at the RFC 6901 JSON pointer ptr.
//
// The pointer goes through schema-valued keywords only: a name under properties, $defs,
// patternProperties or dependentSchemas, an index under allOf, anyOf, oneOf or prefixItems, and
// the keywords that hold a single schema.
func ResolvePointer(root *Schema, ptr string) (*Schema, error) {
	return walkPointer(root, ptr, nil)
}

// walkPointer is ResolvePointer calling visit, unless nil, with the pointer and the schema of
// each step from the root down to the schema at ptr. An error of visit stops the walk.
func walkPointer(
	root *Schema,
	ptr string,
	visit func(at string, s *Schema) error,
) (*Schema, error) {
	if visit != nil {
		if err := visit("", root); err != nil {
			return nil, err
		}
	}

	if ptr == "" {
		return root, nil
	}

	rest, ok := strings.CutPrefix(ptr, "/")
	if !ok {
		return nil, fmt.Errorf("%w: pointer %q does not start with \"/\"", ErrUnresolved, ptr)
	}

	// A bad escape is reported before a missing step, wherever it is.
	for tok := range strings.SplitSeq(rest, "/") {
		if _, err := unescapePointerToken(tok); err != nil {
			return nil, fmt.Errorf("%w: pointer %q: %w", ErrUnresolved, ptr, err)
		}
	}

	// The tokens are taken one by one from rest rather than split up front:
	// resolving runs for every question a generator asks, and a slice of
	// the tokens of a deep pointer costs more than the walk.
	cur := root

	for {
		first, after, more := strings.Cut(rest, "/")
		second, _, _ := strings.Cut(after, "/")
		kw, _ := unescapePointerToken(first)
		key, _ := unescapePointerToken(second)

		next, n := child(cur, kw, key, more)

		taken := len(first)
		if n == 2 && more {
			taken += 1 + len(second)
		}

		at := ptr[:len(ptr)-len(rest)+taken]

		if next == nil {
			return nil, fmt.Errorf("%w: pointer %q: no schema at %q", ErrUnresolved, ptr, at)
		}

		cur = next

		if visit != nil {
			if err := visit(at, cur); err != nil {
				return nil, err
			}
		}

		rest = rest[taken:]
		if rest == "" {
			return cur, nil
		}

		rest = rest[1:]
	}
}

// EscapePointerToken escapes s for use as one reference token of a JSON pointer.
func EscapePointerToken(s string) string { return pointerEscaper.Replace(s) }

// pointerEscaper is built once, since building a Replacer costs far more than a replacement.
var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

func unescapePointerToken(tok string) (string, error) {
	if !strings.Contains(tok, "~") {
		return tok, nil
	}

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

// child returns the subschema of s that the keyword kw names, with key
// the token after it when hasKey holds, and the number of tokens the
// keyword takes. It returns nil when the tokens name no subschema.
func child(s *Schema, kw, key string, hasKey bool) (*Schema, int) {
	if s == nil || s.Bool.IsSome() {
		return nil, 0
	}

	if m, ok := schemaMapOf(s, kw); ok {
		if !hasKey {
			return nil, 2
		}

		c, _ := m.Get(key)

		return c, 2
	}

	if l, ok := schemaListOf(s, kw); ok {
		if !hasKey {
			return nil, 2
		}

		i, ok := arrayIndex(key)
		if !ok || i >= len(l) {
			return nil, 2
		}

		return l[i], 2
	}

	return schemaOf(s, kw), 1
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

// findAnchor returns the pointer of the schema under s, at ptr, that declares the anchor name,
// outside the schemas declaring $id, whose anchors belong to another resource.
func findAnchor(s *Schema, ptr, name string) (string, bool) {
	if s.Anchor == opt.Some(name) || s.DynamicAnchor == opt.Some(name) {
		return ptr, true
	}

	for step, c := range children(s) {
		if c.Id.IsSome() {
			continue
		}

		if found, ok := findAnchor(c, ptr+step, name); ok {
			return found, true
		}
	}

	return "", false
}

// children yields the direct subschemas of s in keyword order, each with the pointer that leads
// to it from s.
func children(s *Schema) iter.Seq2[string, *Schema] {
	return func(yield func(string, *Schema) bool) {
		if s == nil || s.Bool.IsSome() {
			return
		}

		for _, kw := range []string{"$defs", "dependentSchemas", "properties", "patternProperties"} {
			m, _ := schemaMapOf(s, kw)
			for _, n := range m {
				step := "/" + kw + "/" + EscapePointerToken(n.Name)
				if n.Schema != nil && !yield(step, n.Schema) {
					return
				}
			}
		}

		for _, kw := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
			l, _ := schemaListOf(s, kw)
			for i, c := range l {
				if c != nil && !yield("/"+kw+"/"+strconv.Itoa(i), c) {
					return
				}
			}
		}

		for _, kw := range []string{
			"not", "if", "then", "else", "items", "contains", "additionalProperties",
			"propertyNames", "unevaluatedItems", "unevaluatedProperties", "contentSchema",
		} {
			if c := schemaOf(s, kw); c != nil && !yield("/"+kw, c) {
				return
			}
		}
	}
}
