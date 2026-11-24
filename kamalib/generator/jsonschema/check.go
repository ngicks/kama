package jsonschema

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/opt"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// unsupportedKeywords change the set of valid instances in a way the
// generated code cannot express.
var unsupportedKeywords = []string{
	"if", "then", "else", "not", "prefixItems", "patternProperties", "dependentSchemas",
	"unevaluatedItems", "unevaluatedProperties", "contains", "propertyNames", "$dynamicRef",
}

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

	for ptr, s := range schemas(doc) {
		if !declares(doc, ptr) {
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
			for i := range s.AllOf {
				spell(goType(doc, opts, child(ptr, "allOf", strconv.Itoa(i))))
			}

			for _, n := range s.Properties {
				spell(fieldType(doc, opts, ptr, n.Name))
			}

			if extraMembers(s) {
				spell(elemType(doc, opts, child(ptr, "additionalProperties")))
			}
		}
	}

	return errors.Join(errs...)
}

func checkSchema(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema) []error {
	if s.Bool.IsSome() {
		if !s.Bool.Value() {
			return []error{unsupported(ptr, "the false schema admits no value")}
		}

		return nil
	}

	errs := opiter.WrapSeq(slices.Values(present(s, unsupportedKeywords))).
		Map(func(kw string) error { return unsupported(child(ptr, kw), "keyword is not supported") }).
		CollectSlice()

	if ptr != "" && s.Id.IsSome() {
		errs = append(errs, unsupported(child(ptr, "$id"), "a schema resource other than the root"))
	}

	if !s.Type.IsZero() {
		errs = append(errs, checkType(ptr, s)...)
	}

	errs = append(errs, checkKeywords(ptr, s)...)

	switch {
	case s.Ref.IsSome():
		if other := shapesOtherThan(s, "$ref"); len(other) > 0 {
			errs = append(errs, unsupported(child(ptr, other[0]), "keyword next to $ref"))
		} else if _, err := refTarget(doc, ptr); err != nil {
			errs = append(errs, err)
		}
	case unionKeyword(s) != "":
		errs = append(errs, checkUnion(doc, ptr, s)...)
	case s.AllOf != nil:
		errs = append(errs, checkAllOf(doc, ptr, s)...)
	case s.Enum != nil || s.Const != nil:
		errs = append(errs, checkEnum(doc, ptr, s)...)
	default:
		errs = append(errs, checkContainer(ptr, s)...)
	}

	if structKind(s) || product(s) {
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

	if !multiKind(s) {
		return nil
	}

	if other := shapesOtherThan(s, "type"); len(other) > 0 {
		return []error{unsupported(child(ptr, other[0]), "keyword next to a type of several kinds")}
	}

	for _, k := range typeKinds(s) {
		if !slices.Contains(scalarKinds, k) {
			return []error{unsupported(kw, "kind %q in a type of several kinds", k)}
		}
	}

	return nil
}

func checkUnion(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema) []error {
	kw := unionKeyword(s)

	if other := shapesOtherThan(s, kw, "type"); len(other) > 0 {
		return []error{unsupported(child(ptr, other[0]), "keyword next to %s", kw)}
	}

	if len(listOf(s, kw)) == 0 {
		return []error{unsupported(child(ptr, kw), "%s has no member", kw)}
	}

	var errs []error
	if kw == "oneOf" {
		errs = checkDistinct(doc, ptr, s)
	}

	if s.Type.IsZero() {
		return errs
	}

	// A type next to the union narrows it unless every member already
	// admits only kinds the type lists.
	var admitted []string

	for i := range listOf(s, kw) {
		k, err := kinds(doc, child(ptr, kw, strconv.Itoa(i)))
		if err != nil {
			return append(errs, err)
		}

		admitted = append(admitted, k...)

		for _, m := range k {
			if !slices.Contains(s.Type.Names, m) &&
				(m != "integer" || !slices.Contains(s.Type.Names, "number")) {
				return append(errs,
					unsupported(child(ptr, "type"), "type narrows the members of %s", kw),
				)
			}
		}
	}

	// The type would make the holder nullable while no member decodes null.
	if slices.Contains(s.Type.Names, "null") && !slices.Contains(admitted, "null") {
		errs = append(errs, unsupported(
			child(ptr, "type"), "type admits null, which no member of %s does", kw,
		))
	}

	return errs
}

// checkDistinct reports the members of the oneOf at ptr that the generated
// code cannot tell apart, since it does not check the pure validation
// keywords: two members equal but for those keywords, which a value always
// matches both or neither of, and a member holding no other keyword, which
// every value matches. The schema may still tell them apart, so the
// generated code would reject values the schema accepts.
func checkDistinct(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema) []error {
	kw := "oneOf"
	n := len(listOf(s, kw))

	if n < 2 {
		return nil
	}

	var errs []error

	checked := func(i int) (string, string, bool) {
		_, rs, err := resolved(doc, child(ptr, kw, strconv.Itoa(i)))
		if err != nil {
			return "", "", false
		}

		a, okA := schemaText(rs, withoutAnnotations)
		c, okC := schemaText(rs, withoutUnchecked)

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

func checkAllOf(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema) []error {
	if len(s.AllOf) == 0 {
		return []error{unsupported(child(ptr, "allOf"), "allOf has no member")}
	}

	if transparentAllOf(s) {
		return checkTransparentType(doc, ptr, s)
	}

	other := shapesOtherThan(s, "allOf", "type", "properties", "required", "additionalProperties")
	if len(other) > 0 {
		return []error{unsupported(child(ptr, other[0]), "keyword next to allOf")}
	}

	if !s.Type.IsZero() && !slices.Equal(s.Type.Names, []string{"object"}) {
		return []error{
			unsupported(child(ptr, "type"), "allOf of more than one member that is no object"),
		}
	}

	if s.AdditionalProperties != nil && s.AdditionalProperties.Bool != opt.Some(true) {
		return []error{unsupported(
			child(ptr, "additionalProperties"),
			"additionalProperties next to allOf rejects the members of the other schemas",
		)}
	}

	var errs []error

	owner := map[string]string{}
	claim := func(name, at string) {
		first, ok := owner[name]
		if !ok {
			owner[name] = at
			return
		}

		if !sameSchema(doc, first, at) {
			errs = append(errs, fmt.Errorf(
				"%w: #%s and #%s declare the member %q differently", ErrConflict, first, at, name,
			))
		}
	}

	for i := range s.AllOf {
		mp := child(ptr, "allOf", strconv.Itoa(i))

		k, err := kinds(doc, mp)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if !slices.Equal(k, []string{"object"}) {
			errs = append(errs, unsupported(mp, "allOf member is no object"))
			continue
		}

		r, rs, err := resolved(doc, mp)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if closed(rs) {
			errs = append(
				errs,
				unsupported(mp, "allOf member rejects the members of the other schemas"),
			)
		}

		if onCycle(doc, ptr, mp) {
			errs = append(errs, unsupported(mp, "allOf member holds the product itself"))
		}

		for _, n := range rs.Properties {
			claim(n.Name, child(r, "properties", n.Name))
		}
	}

	for _, n := range s.Properties {
		claim(n.Name, child(ptr, "properties", n.Name))
	}

	return errs
}

// checkTransparentType reports a type next to the allOf of one member at
// ptr that admits other kinds than the member. The allOf takes the type of
// the member, which would drop the type.
func checkTransparentType(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema) []error {
	if s.Type.IsZero() {
		return nil
	}

	k, err := kinds(doc, child(ptr, "allOf", "0"))
	if err != nil {
		return []error{err}
	}

	if !slices.Equal(slices.Sorted(slices.Values(k)), slices.Sorted(slices.Values(s.Type.Names))) {
		return []error{unsupported(
			child(ptr, "type"), "type differs from the kinds %v of the only allOf member", k,
		)}
	}

	return nil
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

	if len(k) != 1 || !slices.Contains(scalarKinds, k[0]) {
		return []error{unsupported(ptr, "enum or const of other than one scalar kind")}
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

// checkContainer checks the keywords of an object or an array name the
// kind its type keyword gives.
func checkContainer(ptr string, s *jsonschema.Schema) []error {
	object := s.Properties != nil || s.Required != nil || s.AdditionalProperties != nil
	array := s.Items != nil

	switch {
	case object && array:
		return []error{unsupported(ptr, "keywords of both an object and an array")}
	case s.Type.IsZero():
		return nil
	case object && !slices.Equal(typeKinds(s), []string{"object"}):
		return []error{unsupported(ptr, "keywords of an object next to type %v", s.Type.Names)}
	case array && !slices.Equal(typeKinds(s), []string{"array"}):
		return []error{unsupported(ptr, "keywords of an array next to type %v", s.Type.Names)}
	}

	return nil
}

// checkProperties checks the properties of a struct or a product give Go
// names and json tags, and that every required member has a property.
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

	for _, name := range s.Required {
		if _, ok := s.Properties.Get(name); !ok {
			errs = append(errs, unsupported(
				child(ptr, "required"), "required member %q has no property", name,
			))
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

	for i := range s.AllOf {
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
