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
	return typeNameAt(doc, opts, ptr, stepEnds(ptr), seen)
}

// typeNameAt is typeNameSeen for ptr whose steps end at the offsets ends.
// The name of ptr takes the names of its parents, which reuse ends rather
// than parse their steps again, so a name costs the length of ptr once.
func typeNameAt(
	doc *jsonschema.Schema,
	opts Options,
	ptr string,
	ends []int,
	seen []string,
) (string, error) {
	if slices.Contains(seen, ptr) {
		return "", unsupported(ptr, "names itself")
	}

	seen = append(seen, ptr)

	if ptr == "" {
		return rootName(doc, opts)
	}

	up := ends[:len(ends)-1]

	parent := ""
	if len(up) > 0 {
		parent = ptr[:up[len(up)-1]]
	}

	kw, key, _ := strings.Cut(ptr[len(parent)+1:], "/")
	kw, key = unescape(kw), unescape(key)

	switch kw {
	case "$defs":
		return definitionName(ptr, ends, key)
	case "properties":
		return suffixed(doc, opts, parent, up, seen, ptr, "property", key)
	case "oneOf", "anyOf", "allOf":
		return memberTypeName(doc, opts, parent, up, kw, key, seen)
	case "items":
		return suffixed(doc, opts, parent, up, seen, ptr, "items", "Item")
	case "additionalProperties":
		return suffixed(doc, opts, parent, up, seen, ptr, "additionalProperties", "Value")
	}

	return "", unsupported(ptr, "no Go name for a schema at this keyword")
}

// definitionName names the schema at ptr, whose steps end at ends, under
// $defs by its name key there. A document bundle embeds takes the name
// documentName gives it, and a schema under $defs inside such a document
// takes that name followed by its own, so two documents may each define a
// name.
func definitionName(ptr string, ends []int, key string) (string, error) {
	if uri := embeddedDocument(ptr); uri != "" {
		return documentName(ptr, uri)
	}

	name, err := identifier(ptr, "definition", key)
	if err != nil || len(ends) < 2 {
		return name, err
	}

	uri := embeddedDocument(ptr[:ends[0]])
	if uri == "" {
		return name, nil
	}

	prefix, err := documentName(ptr[:ends[0]], uri)

	return prefix + name, err
}

// twoTokenKeywords hold a map or a list of schemas, so that a step through
// one takes the name or the index after it.
var twoTokenKeywords = []string{
	"$defs", "properties", "oneOf", "anyOf", "allOf", "patternProperties", "dependentSchemas",
	"prefixItems",
}

// stepEnds returns the offsets in ptr, a pointer schemas yields, where each
// of its steps ends. A step is a keyword and, where the keyword holds a map
// or a list, the name or index under it. It parses from the front, since a
// name such as "items" cannot be told from a keyword from the back.
func stepEnds(ptr string) []int {
	if ptr == "" {
		return nil
	}

	toks := strings.Split(strings.TrimPrefix(ptr, "/"), "/")

	var ends []int

	end := 0

	for i := 0; i < len(toks); {
		kw := unescape(toks[i])
		end += 1 + len(toks[i])
		i++

		if slices.Contains(twoTokenKeywords, kw) &&
			i < len(toks) {
			end += 1 + len(toks[i])
			i++
		}

		ends = append(ends, end)
	}

	return ends
}

func memberTypeName(
	doc *jsonschema.Schema,
	opts Options,
	parent string,
	ends []int,
	kw, index string,
	seen []string,
) (string, error) {
	ps, err := at(doc, parent)
	if err != nil {
		return "", err
	}

	p, err := typeNameAt(doc, opts, parent, ends, seen)
	if err != nil {
		return "", err
	}

	// The value of a nullable union and the member of an allOf of one member
	// take the name of the schema holding them, which declares no type. The
	// root declares a type under that name anyway, so its value takes Value.
	if nullableUnion(doc, parent) || transparentAllOf(doc, parent, ps) {
		if parent == "" {
			return p + "Value", nil
		}

		return p, nil
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
	ends []int,
	seen []string,
	ptr, what, name string,
) (string, error) {
	p, err := typeNameAt(doc, opts, parent, ends, seen)
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

func unescape(tok string) string { return pointerUnescaper.Replace(tok) }

// pointerUnescaper is built once, since building a Replacer costs far more
// than a replacement.
var pointerUnescaper = strings.NewReplacer("~1", "/", "~0", "~")

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
