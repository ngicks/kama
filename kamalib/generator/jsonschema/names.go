package jsonschema

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// goName upper-camel-cases s on the characters an identifier cannot hold,
// as foo_bar, foo-bar and foo bar give FooBar. ok is false for a result
// that is empty, starts with a digit or does not start with an upper-case
// letter.
func goName(s string) (string, bool) {
	var b strings.Builder

	upper := true

	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			upper = true
			continue
		}

		if upper {
			r = unicode.ToUpper(r)
			upper = false
		}

		b.WriteRune(r)
	}

	out := b.String()
	first := []rune(out + " ")[0]

	return out, out != "" && unicode.IsUpper(first)
}

// typeName returns the name of the Go type the schema at ptr declares or
// would declare:
//
//   - the root takes Options.TypeName, else its title;
//   - a schema under $defs takes its name there;
//   - a property takes the name of its object followed by its own name, an
//     items schema "Item" and an additionalProperties schema "Value";
//   - a member of a union takes the name of its wrapper followed by
//     "Value", one of an allOf the name of its field;
//   - the value of a nullable union and the member of an allOf of one
//     member take the name of the schema holding them.
func typeName(doc *jsonschema.Schema, opts Options, ptr string) (string, error) {
	return typeNameSeen(doc, opts, ptr, nil)
}

func typeNameSeen(doc *jsonschema.Schema, opts Options, ptr string, seen []string) (string, error) {
	if slices.Contains(seen, ptr) {
		return "", unsupported(ptr, "names itself")
	}

	seen = append(seen, ptr)

	if ptr == "" {
		return rootName(doc, opts)
	}

	parent, kw, key := lastStep(ptr)

	switch kw {
	case "$defs":
		return identifier(ptr, "definition", key)
	case "properties":
		return suffixed(doc, opts, parent, seen, ptr, "property", key)
	case "oneOf", "anyOf", "allOf":
		return memberTypeName(doc, opts, parent, kw, key, seen)
	case "items":
		return suffixed(doc, opts, parent, seen, ptr, "items", "Item")
	case "additionalProperties":
		return suffixed(doc, opts, parent, seen, ptr, "additionalProperties", "Value")
	}

	return "", unsupported(ptr, "no Go name for a schema at this keyword")
}

// lastStep splits ptr, a pointer schemas yields, into the pointer of the
// schema holding it, the keyword and the name or index under the keyword.
// The keyword takes the key only where it holds a map or a list.
func lastStep(ptr string) (parent, kw, key string) {
	toks := strings.Split(strings.TrimPrefix(ptr, "/"), "/")

	for i := 0; i < len(toks); {
		parent, kw, key = strings.Join(toks[:i], "/"), unescape(toks[i]), ""
		if i > 0 {
			parent = "/" + parent
		}

		i++

		if slices.Contains([]string{"$defs", "properties", "oneOf", "anyOf", "allOf"}, kw) &&
			i < len(toks) {
			key = unescape(toks[i])
			i++
		}
	}

	return parent, kw, key
}

func memberTypeName(
	doc *jsonschema.Schema,
	opts Options,
	parent, kw, index string,
	seen []string,
) (string, error) {
	ps, err := at(doc, parent)
	if err != nil {
		return "", err
	}

	p, err := typeNameSeen(doc, opts, parent, seen)
	if err != nil || nullableUnion(doc, parent) || transparentAllOf(ps) {
		return p, err
	}

	i, _ := strconv.Atoi(index)

	m, err := memberNameSeen(doc, opts, parent, i, seen)
	if err != nil || kw == "allOf" {
		return p + m, err
	}

	return p + m + "Value", nil
}

func suffixed(
	doc *jsonschema.Schema,
	opts Options,
	parent string,
	seen []string,
	ptr, what, name string,
) (string, error) {
	p, err := typeNameSeen(doc, opts, parent, seen)
	if err != nil {
		return "", err
	}

	n, err := identifier(ptr, what, name)

	return p + n, err
}

func rootName(doc *jsonschema.Schema, opts Options) (string, error) {
	// Generate has checked that the type name is an identifier.
	if opts.TypeName != "" {
		return opts.TypeName, nil
	}

	if doc.Title.IsNone() {
		return "", unsupported("", "the root schema has no title and no type name is given")
	}

	return identifier("/title", "title", doc.Title.Value())
}

// identifier returns goName of name, or an error naming ptr and what name
// is.
func identifier(ptr, what, name string) (string, error) {
	n, ok := goName(name)
	if !ok {
		return "", unsupported(ptr, "%s %q gives no exported Go identifier", what, name)
	}

	return n, nil
}

func unescape(tok string) string { return strings.NewReplacer("~1", "/", "~0", "~").Replace(tok) }

// members returns the number of members of the union or allOf s. The
// members of a type of several kinds are its kinds.
func members(s *jsonschema.Schema) int {
	if kw := memberKeyword(s); kw != "" {
		return len(listOf(s, kw))
	}

	return len(s.Type.Names)
}

// memberKeyword returns the keyword holding the members of s, or "" for a
// union of the kinds of its type keyword.
func memberKeyword(s *jsonschema.Schema) string {
	if kw := unionKeyword(s); kw != "" {
		return kw
	}

	if s.AllOf != nil {
		return "allOf"
	}

	return ""
}

// memberName names the member i of the union or allOf at ptr: the name of
// the schema its $ref names, else its title, else its kind when no other
// member without a $ref or a title has that kind, else Option<i+1>.
func memberName(doc *jsonschema.Schema, opts Options, ptr string, i int) (string, error) {
	return memberNameSeen(doc, opts, ptr, i, nil)
}

func memberNameSeen(
	doc *jsonschema.Schema,
	opts Options,
	ptr string,
	i int,
	seen []string,
) (string, error) {
	s, err := at(doc, ptr)
	if err != nil {
		return "", err
	}

	kw := memberKeyword(s)
	if kw == "" {
		return kindName(s.Type.Names[i]), nil
	}

	mp := child(ptr, kw, strconv.Itoa(i))

	m, err := at(doc, mp)
	if err != nil {
		return "", err
	}

	switch {
	case m.Ref.IsSome():
		target, err := refTarget(doc, mp)
		if err != nil {
			return "", err
		}

		return typeNameSeen(doc, opts, target, seen)
	case m.Title.IsSome():
		return identifier(child(mp, "title"), "title", m.Title.Value())
	}

	k := soleKind(doc, mp)

	same := 0

	for j, o := range listOf(s, kw) {
		if o.Ref.IsNone() && o.Title.IsNone() && k != "" &&
			soleKind(doc, child(ptr, kw, strconv.Itoa(j))) == k {
			same++
		}
	}

	if same == 1 {
		return kindName(k), nil
	}

	return "Option" + strconv.Itoa(i+1), nil
}

// soleKind returns the only kind the schema at ptr admits, or "".
func soleKind(doc *jsonschema.Schema, ptr string) string {
	k, err := kinds(doc, ptr)
	if err != nil || len(k) != 1 {
		return ""
	}

	return k[0]
}

// kindName spells the JSON type k as a Go name, as integer gives Integer.
func kindName(k string) string {
	n, _ := goName(k)
	return n
}

// wireName names the struct the type name decodes through.
func wireName(name string) string {
	r := []rune(name)
	return string(unicode.ToLower(r[0])) + string(r[1:]) + "Wire"
}
