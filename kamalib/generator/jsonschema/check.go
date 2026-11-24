package jsonschema

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// metaSchema is the URI of the meta-schema of draft 2020-12.
const metaSchema = "https://json-schema.org/draft/2020-12/schema"

// countKeywords count the members of an object.
var countKeywords = []string{"minProperties", "maxProperties"}

// scalarKinds are the kinds a Go scalar type holds.
var scalarKinds = []string{"string", "integer", "number", "boolean"}

// methodNames are the methods a generated struct declares, which no field
// of it may be named. Validate is the method the validator generator
// declares on it.
var methodNames = []string{
	"MarshalJSONTo", "UnmarshalJSONFrom", "AdditionalProperties", "Validate",
}

// check reports every schema of doc the generator cannot express, then
// every pair of Go names that collide.
func check(doc *jsonschema.Schema, opts Options) error {
	// The other checks read every schema they reach, so a nil one is
	// reported before any of them runs.
	nils := opiter.WrapSeq2(schemas(doc)).
		Filter(func(_ string, s *jsonschema.Schema) bool { return s == nil }).
		OmitL().
		Map(func(ptr string) error { return unsupported(ptr, "a nil schema") }).
		CollectSlice()
	if err := errors.Join(nils...); err != nil {
		return err
	}

	var errs []error

	for ptr, s := range schemas(doc) {
		errs = append(errs, checkSchema(doc, ptr, s)...)
	}

	if err := errors.Join(errs...); err != nil {
		return err
	}

	// A $ref alone takes the type of its target, whose own check covers it.
	for ptr, s := range schemas(doc) {
		if s.Bool.IsSome() || refAloneAt(doc, ptr, s) || ruled(doc, ptr) {
			continue
		}

		rules := jsonArgs
		if rawFrom(doc, ptr, s) {
			rules = rawRules
		}

		if _, err := rules(doc, opts, ptr); err != nil {
			errs = append(errs, err)
		}
	}

	if _, err := typeName(doc, opts, ""); err != nil {
		errs = append(errs, err)
	}

	if err := errors.Join(errs...); err != nil {
		return err
	}

	if err := checkTypes(doc, opts); err != nil {
		return err
	}

	return checkNames(doc, opts)
}

// checkTypes spells every Go type the declarations of doc spell, so that a
// schema with no Go type is reported before rendering starts.
func checkTypes(doc *jsonschema.Schema, opts Options) error {
	imports, err := newImports(opts.Package)
	if err != nil {
		return err
	}

	c := genhelper.NewBaseContext(genhelper.NewScope(), &genhelper.Package{Imports: imports})

	var errs []error

	spell := func(t genExpr) {
		if _, err := t(c); err != nil {
			errs = append(errs, err)
		}
	}

	if !declares(doc, "") {
		spell(definedType(doc, opts, ""))
	}

	// A type of raw JSON text spells jsontext.Value whatever its keywords.
	for ptr, s := range schemas(doc) {
		if !declares(doc, ptr) || rawFrom(doc, ptr, s) {
			continue
		}

		switch {
		case union(s):
			for i := range members(s) {
				if !isNullMember(doc, ptr, i) {
					spell(unionMemberType(doc, opts, ptr, i))
				}
			}
		case stringEnum(s):
		default:
			for i := range productOf(doc, ptr, s) {
				spell(goType(doc, opts, child(ptr, "allOf", strconv.Itoa(i))))
			}

			for _, n := range s.Properties {
				spell(fieldType(doc, opts, ptr, n.Name))
			}

			if extraMembers(s) && !bridged(doc, ptr, s, "additionalProperties") {
				spell(elemType(doc, opts, child(ptr, "additionalProperties")))
			}
		}
	}

	return errors.Join(errs...)
}

// checkSchema reports what of the schema s at ptr the generator cannot
// express. The shapes of a Go type are checked only where the schema has
// one: raw JSON text and a schema in the validators of another take every
// keyword as a validator.
func checkSchema(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema) []error {
	if s.Bool.IsSome() {
		return nil
	}

	var errs []error

	// Another meta-schema may turn vocabularies off or give keywords the
	// meaning of another draft. withVocabularies reads that of the root
	// only, and the root of an embedded document is none.
	if ptr != "" && s.Schema.IsSome() && strings.TrimSuffix(s.Schema.Value(), "#") != metaSchema {
		errs = append(errs, unsupported(child(ptr, "$schema"),
			"meta-schema %q is not that of draft 2020-12", s.Schema.Value()))
	}

	if !s.Type.IsZero() {
		errs = append(errs, checkType(ptr, s)...)
	}

	errs = append(errs, checkKeywords(ptr, s)...)

	for _, kw := range present(s, []string{"$ref", "$dynamicRef"}) {
		if _, err := refTargetOf(doc, ptr, kw); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 || refAloneAt(doc, ptr, s) || ruled(doc, ptr) || rawFrom(doc, ptr, s) {
		return errs
	}

	switch {
	case union(s) && unionKeyword(s) != "":
		errs = append(errs, checkUnion(doc, ptr, s)...)
	case product(doc, ptr, s):
		errs = append(errs, checkProduct(doc, ptr, s)...)
	case s.Enum != nil || s.Const != nil:
		errs = append(errs, checkEnum(doc, ptr, s)...)
	}

	if structKind(s) || product(doc, ptr, s) {
		errs = append(errs, checkProperties(ptr, s)...)
	}

	return errs
}

func checkType(ptr string, s *jsonschema.Schema) []error {
	kw := child(ptr, "type")

	if len(s.Type.Names) == 0 {
		return []error{unsupported(kw, "type lists no kind")}
	}

	for _, k := range s.Type.Names {
		if !slices.Contains(allKinds, k) && k != "integer" {
			return []error{unsupported(kw, "unknown type %q", k)}
		}
	}

	return nil
}

func checkUnion(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema) []error {
	kw := unionKeyword(s)

	if len(listOf(s, kw)) == 0 {
		return []error{unsupported(child(ptr, kw), "%s has no member", kw)}
	}

	if kw == "oneOf" {
		return checkDistinct(doc, ptr, s)
	}

	return nil
}

// checkDistinct reports the members of the oneOf at ptr that the generated
// code cannot tell apart, since it does not check every validation keyword:
// two members equal but for keywords one of them leaves unchecked, which a
// value then matches both or neither of, and a member holding no other
// keyword, which every value matches. The schema may still tell them apart,
// so the generated code would reject values the schema accepts. A member
// whose validation keywords are all checked keeps them, since selecting a
// member runs its Validate.
func checkDistinct(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema) []error {
	kw := "oneOf"
	n := len(listOf(s, kw))

	if n < 2 {
		return nil
	}

	var errs []error

	checked := func(i int) (string, string, bool) {
		r, rs, err := resolved(doc, child(ptr, kw, strconv.Itoa(i)))
		if err != nil {
			return "", "", false
		}

		a, okA := schemaText(rs, withoutAnnotations)

		drop := withoutAnnotations
		if !allChecked(doc, r) {
			drop = withoutUnchecked
		}

		c, okC := schemaText(rs, drop)

		return a, c, okA && okC
	}

	for i := range n {
		a, c, ok := checked(i)
		if ok && c == "{}" && a != "{}" {
			errs = append(errs, unsupported(
				child(ptr, kw, strconv.Itoa(i)),
				"oneOf member holds only keywords the generated code does not check, "+
					"so it matches every value",
			))
		}

		for j := i + 1; j < n; j++ {
			b, d, okJ := checked(j)
			if ok && okJ && c == d && a != b {
				errs = append(errs, unsupported(
					child(ptr, kw),
					"members %d and %d differ only in keywords the generated code does not check",
					i, j,
				))
			}
		}
	}

	return errs
}

// allChecked reports whether the generated code checks every validation
// keyword of the schema at ptr and of the schemas under it.
func allChecked(doc *jsonschema.Schema, ptr string) bool {
	return !opiter.WrapSeq2(schemas(doc)).
		Filter(func(p string, _ *jsonschema.Schema) bool {
			return p == ptr || strings.HasPrefix(p, ptr+"/")
		}).
		Any(func(p string, _ *jsonschema.Schema) bool { return len(uncheckedAt(doc, p)) > 0 })
}

// checkProduct reports a member of the product at ptr that holds the
// product itself by value, which Go cannot lay out.
func checkProduct(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema) []error {
	return opiter.WrapSeq(opiter.Range(0, len(s.AllOf))).
		Map(func(i int) string { return child(ptr, "allOf", strconv.Itoa(i)) }).
		Filter(func(mp string) bool { return onCycle(doc, ptr, mp) }).
		Map(func(mp string) error { return unsupported(mp, "allOf member holds the product itself") }).
		CollectSlice()
}

// sameSchema reports whether the schemas at a and b admit the same values
// and give the same Go type: they encode to the same JSON text once the
// annotations are gone from them and from every schema under them.
func sameSchema(doc *jsonschema.Schema, a, b string) bool {
	sa, errA := at(doc, a)
	sb, errB := at(doc, b)

	if errA != nil || errB != nil {
		return false
	}

	ja, okA := schemaText(sa, withoutAnnotations)
	jb, okB := schemaText(sb, withoutAnnotations)

	return okA && okB && ja == jb
}

func checkEnum(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema) []error {
	if s.Enum != nil && s.Const != nil {
		return []error{unsupported(child(ptr, "const"), "const next to enum")}
	}

	if other := shapesOtherThan(s, "enum", "const", "type"); len(other) > 0 {
		return []error{unsupported(child(ptr, other[0]), "keyword next to enum or const")}
	}

	if s.Enum != nil && len(s.Enum) == 0 {
		return []error{unsupported(child(ptr, "enum"), "enum lists no value")}
	}

	k, err := kinds(doc, ptr)
	if err != nil {
		return []error{err}
	}

	if raw(doc, ptr) {
		return nil
	}

	if stringEnum(s) {
		if !slices.Equal(k, []string{"string"}) {
			return []error{unsupported(child(ptr, "type"), "enum of strings next to type %v", k)}
		}

		return nil
	}

	_, err = allowedValues(doc, ptr)

	return []error{err}
}

// checkProperties checks the properties of a struct or a product give Go
// names and json tags.
func checkProperties(ptr string, s *jsonschema.Schema) []error {
	var errs []error

	for _, n := range s.Properties {
		pp := child(ptr, "properties", n.Name)

		if _, err := identifier(pp, "property", n.Name); err != nil {
			errs = append(errs, err)
		}

		if strings.ContainsFunc(n.Name, func(r rune) bool {
			return strings.ContainsRune(",\\'\"`", r) || unicode.IsControl(r)
		}) {
			errs = append(errs, unsupported(pp, "property name %q does not fit a json tag", n.Name))
		}
	}

	return errs
}

// checkNames reports the Go names that collide: two declarations of the
// package, two fields of a struct or of its wire struct, or a field and a
// method.
func checkNames(doc *jsonschema.Schema, opts Options) error {
	var errs []error

	declared := map[string]string{}
	declare := func(name, at string) {
		if first, ok := declared[name]; ok {
			errs = append(errs, fmt.Errorf(
				"%w: #%s and #%s both give the Go name %s", ErrConflict, first, at, name,
			))

			return
		}

		declared[name] = at
	}

	if !declares(doc, "") {
		if name, err := typeName(doc, opts, ""); err == nil {
			declare(name, "")
		}
	}

	for ptr, s := range schemas(doc) {
		if !declares(doc, ptr) {
			continue
		}

		name, err := typeName(doc, opts, ptr)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		declare(name, ptr)

		switch {
		case union(s):
			declare(name+"Kind", ptr)
			declare(name+"Unknown", ptr)

			for i := range members(s) {
				m, err := memberName(doc, opts, ptr, i)
				if err != nil {
					errs = append(errs, err)
					continue
				}

				declare(name+m, ptr)
			}
		case stringEnum(s):
			for i := range s.Enum {
				c, err := enumConst(doc, opts, ptr, i)
				if err != nil {
					errs = append(errs, err)
					continue
				}

				declare(c, child(ptr, "enum", strconv.Itoa(i)))
			}
		case rawFrom(doc, ptr, s):
		default:
			declare(wireName(name), ptr)
			errs = append(errs, fieldNames(doc, opts, ptr, s)...)
		}
	}

	return errors.Join(errs...)
}

// fieldNames reports the fields of the struct or product at ptr, or of its
// wire struct, that share a Go name with another field or a method.
func fieldNames(doc *jsonschema.Schema, opts Options, ptr string, s *jsonschema.Schema) []error {
	var errs []error

	fields := map[string]string{}
	wire := map[string]string{}

	for _, m := range methodNames {
		fields[m] = "a method"
	}

	add := func(scope map[string]string, name, what string) {
		if first, ok := scope[name]; ok {
			errs = append(errs, fmt.Errorf(
				"%w: #%s: %s and %s both give the Go field name %s",
				ErrConflict,
				ptr,
				first,
				what,
				name,
			))

			return
		}

		scope[name] = what
	}

	for i := range productOf(doc, ptr, s) {
		m, err := memberName(doc, opts, ptr, i)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		add(fields, m, "allOf member "+strconv.Itoa(i))
	}

	for _, n := range s.Properties {
		f, err := identifier(child(ptr, "properties", n.Name), "property", n.Name)
		if err != nil {
			continue
		}

		add(fields, f, "property "+strconv.Quote(n.Name))
		add(wire, f, "property "+strconv.Quote(n.Name))
	}

	add(wire, "AdditionalProperties", "the field of the members no property declares")

	return errs
}
