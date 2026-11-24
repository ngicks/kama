package jsonschema

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// The functions of this file turn the validation keywords of a schema into
// the args of //kama:validate. A keyword applies to the values of one kind
// of JSON value only, as minLength to strings, so the args depend on the
// kind of the Go value the directive validates. The kinds are those of
// placeKind.

// ErrUnenforced is the warning of a validation keyword the generated code
// does not check, such as minimum on a schema of no type, whose Go type is
// jsontext.Value. The warning names the JSON pointer of the keyword.
var ErrUnenforced = errors.New("jsonschema: keyword not enforced")

// placeKind returns the kind of the Go value of the schema at ptr, as
// validators see it: string, integer, number, boolean, array (a slice), map
// or struct (a struct or a product). It follows references, an allOf of one
// member and the value of a nullable union. It is empty for a union and
// for raw JSON text, which no validator checks.
func placeKind(doc *jsonschema.Schema, ptr string) string {
	r, s, err := resolvedValue(doc, ptr)
	if err != nil || s.Bool.IsSome() || union(s) && !nullableType(s) {
		return ""
	}

	switch {
	case structKind(s) || product(s):
		return "struct"
	case objectKind(s) && mapOnly(s):
		return "map"
	}

	k, err := kinds(doc, r)
	if err != nil {
		return ""
	}

	nonNull := opiter.WrapSeq(slices.Values(k)).
		Filter(func(k string) bool { return k != "null" }).
		CollectSlice()

	if len(nonNull) != 1 || nonNull[0] == "object" {
		return ""
	}

	return nonNull[0]
}

// valueArgs returns the args of //kama:validate checking the validation
// keywords of the schema at ptr itself on a Go value of kind, and the
// keywords that apply to kind and that no arg checks. A keyword applying
// to another kind of value holds for a value of kind, so it gives neither.
// The args of a struct are those of the struct as a whole, which typeArgs
// gives.
func valueArgs(doc *jsonschema.Schema, ptr, kind string) ([]string, []string) {
	s, err := at(doc, ptr)
	if err != nil || s.Bool.IsSome() {
		return nil, nil
	}

	var args, unchecked []string

	cmp := comparisons(map[string]jsontext.Value{
		">=": s.Minimum, "<=": s.Maximum, ">": s.ExclusiveMinimum, "<": s.ExclusiveMaximum,
	}, numberText)
	lengths := func(min, max jsontext.Value) string {
		return comparisons(map[string]jsontext.Value{">=": min, "<=": max}, integerText)
	}

	switch kind {
	case "integer", "number":
		if cmp != "" {
			args = append(args, "cmp="+quoteArg(cmp))
		}

		if s.MultipleOf != nil {
			args = append(args, "multipleof="+quoteArg(string(s.MultipleOf)))
		}
	case "string":
		if l := lengths(s.MinLength, s.MaxLength); l != "" {
			args = append(args, "runes="+quoteArg(l))
		}

		if s.Pattern.IsSome() {
			if _, err := regexp.Compile(s.Pattern.Value()); err != nil {
				unchecked = append(unchecked, "pattern")
			} else {
				args = append(args, "regexp="+quoteArg(s.Pattern.Value()))
			}
		}
	case "array":
		if l := lengths(s.MinItems, s.MaxItems); l != "" {
			args = append(args, "length="+quoteArg(l))
		}

		if s.UniqueItems.Value() {
			args = append(args, "unique")
		}
	case "map":
		if l := lengths(s.MinProperties, s.MaxProperties); l != "" {
			args = append(args, "length="+quoteArg(l))
		}

		if s.DependentRequired != nil {
			unchecked = append(unchecked, "dependentRequired")
		}
	case "":
		unchecked = append(unchecked, presentValidation(s)...)
	}

	if allowed, err := allowedValues(doc, ptr); err == nil && allowed != nil {
		switch kind {
		case "string", "integer", "number", "boolean":
			args = append(args, "const="+quoteArg(strings.Join(allowed, " ")))
		}
	}

	return args, slices.Compact(slices.Sorted(slices.Values(unchecked)))
}

// presentValidation returns the validation keywords of s that some kind
// of value is checked against, enum and const aside.
func presentValidation(s *jsonschema.Schema) []string {
	has := map[string]bool{
		"minimum":           s.Minimum != nil,
		"maximum":           s.Maximum != nil,
		"exclusiveMinimum":  s.ExclusiveMinimum != nil,
		"exclusiveMaximum":  s.ExclusiveMaximum != nil,
		"dependentRequired": s.DependentRequired != nil,
		"multipleOf":        s.MultipleOf != nil,
		"minLength":         s.MinLength != nil,
		"maxLength":         s.MaxLength != nil,
		"pattern":           s.Pattern.IsSome(),
		"minItems":          s.MinItems != nil,
		"maxItems":          s.MaxItems != nil,
		"uniqueItems":       s.UniqueItems.Value(),
		"minProperties":     s.MinProperties != nil,
		"maxProperties":     s.MaxProperties != nil,
	}

	return opiter.WrapSeq2(opiter.MapsSorted(has)).
		Filter(func(_ string, ok bool) bool { return ok }).
		OmitL().
		CollectSlice()
}

// typeArgs returns the args of //kama:validate on the type the schema at
// ptr declares, and the keywords that apply to its value and that no arg
// checks. A struct or a product takes hook=off, its hooks being unwritten,
// and the validators of the object as a whole. A union takes none, its
// holder having a Validate of its own.
func typeArgs(doc *jsonschema.Schema, ptr string) ([]string, []string) {
	s, err := at(doc, ptr)
	if err != nil {
		return nil, nil
	}

	if !structKind(s) && !product(s) {
		if ptr == "" && nullable(doc, ptr) {
			// The root is an opt.Option, a struct of another package, which
			// takes no validator of its value.
			_, unchecked := valueArgs(doc, ptr, "")
			return nil, unchecked
		}

		return valueArgs(doc, ptr, placeKind(doc, ptr))
	}

	args := []string{"hook=off"}

	if closed(s) {
		args = append(args, "closed")
	}

	members := comparisons(
		map[string]jsontext.Value{">=": s.MinProperties, "<=": s.MaxProperties}, integerText,
	)
	if members != "" {
		args = append(args, "members="+quoteArg(members))
	}

	// A member without a property has no field to read or to test.
	undeclared := func(n string) bool { _, ok := s.Properties.Get(n); return !ok }

	unfit := opiter.WrapSeq2(opiter.MapsSorted(s.DependentRequired)).
		Any(func(key string, deps []string) bool {
			return undeclared(key) || slices.ContainsFunc(deps, undeclared)
		})
	if unfit {
		return args, []string{"dependentRequired"}
	}

	return args, nil
}

// declArgs returns the args of //kama:validate on the type the schema at
// ptr declares.
func declArgs(doc *jsonschema.Schema, ptr string) []string {
	args, _ := typeArgs(doc, ptr)
	return args
}

// fieldArgs returns the args of //kama:validate on the field of the
// property name of the struct or product at ptr: the validation keywords
// of the property schema itself, unless it declares a type of its own, and
// what the dependentRequired of the object asks of the field. The keywords
// of a schema the property refers to are those of the type it declares.
func fieldArgs(doc *jsonschema.Schema, ptr, name string) []string {
	pp := child(ptr, "properties", name)

	var args []string

	if kind := placeKind(doc, pp); !declares(doc, pp) && kind != "struct" {
		args, _ = valueArgs(doc, pp, kind)
	}

	return append(args, dependentArgs(doc, ptr, name)...)
}

// dependentArgs returns the args the dependentRequired of the object at
// ptr gives the field of its property name. A required member is always
// present after decoding, so requires names only the optional members a
// present optional member depends on. The members a required member
// depends on are required themselves. A key or a member without a
// property is no field; typeArgs reports it.
func dependentArgs(doc *jsonschema.Schema, ptr, name string) []string {
	s, err := at(doc, ptr)
	if err != nil {
		return nil
	}

	declared := func(n string) bool { _, ok := s.Properties.Get(n); return ok }
	optional := func(n string) bool { return !slices.Contains(s.Required, n) }

	var names []string

	if deps, ok := s.DependentRequired[name]; ok && optional(name) && declared(name) &&
		!slices.ContainsFunc(deps, func(n string) bool { return !declared(n) }) {
		names = opiter.WrapSeq(slices.Values(deps)).
			Filter(optional).
			CollectSlice()
	}

	requiredBy := opiter.WrapSeq(slices.Values(slices.Sorted(maps.Keys(s.DependentRequired)))).
		Filter(func(k string) bool { return !optional(k) && declared(k) }).
		Any(func(k string) bool { return slices.Contains(s.DependentRequired[k], name) })

	var args []string

	if len(names) > 0 {
		args = append(args, "requires="+quoteArg(dircmnt.SpellArgs(
			opiter.WrapSeq(slices.Values(names)).
				Map(func(n string) dircmnt.KeyValue { return dircmnt.KeyValue{Key: n, KeyQuoted: true} }).
				CollectSlice(),
		)))
	}

	if requiredBy && optional(name) {
		args = append(args, "required")
	}

	return args
}

// unenforcedKeywords returns a warning for each validation keyword of the
// schemas of doc that the generated code does not check.
func unenforcedKeywords(doc *jsonschema.Schema) []error {
	var warnings []error

	for ptr := range schemas(doc) {
		for _, kw := range uncheckedAt(doc, ptr) {
			warnings = append(warnings, fmt.Errorf("%w: #%s", ErrUnenforced, child(ptr, kw)))
		}
	}

	return warnings
}

// uncheckedAt returns the validation keywords of the schema at ptr that no
// directive checks: the keywords of a raw value or a union, those a
// declared type or a field cannot take, and those of a schema that is
// neither, such as one next to a $ref outside an object property.
func uncheckedAt(doc *jsonschema.Schema, ptr string) []string {
	switch {
	case declares(doc, ptr) || ptr == "":
		_, unchecked := typeArgs(doc, ptr)
		return unchecked
	case isProperty(ptr):
		kind := placeKind(doc, ptr)
		if kind == "struct" {
			kind = ""
		}

		_, unchecked := valueArgs(doc, ptr, kind)

		return unchecked
	}

	_, unchecked := valueArgs(doc, ptr, "")

	return unchecked
}

// valueType reports whether the schema at ptr declares a type defined over
// its built-in Go type, whose Validate checks its validation keywords: it
// is no object property, whose field takes them, it declares no type of
// another shape and takes no type from another schema, and a keyword
// applies to its value.
func valueType(doc *jsonschema.Schema, ptr string) bool {
	if ptr == "" || isProperty(ptr) {
		return false
	}

	s, err := at(doc, ptr)
	if err != nil || s.Bool.IsSome() || s.Ref.IsSome() || transparentAllOf(s) || union(s) ||
		s.AllOf != nil || stringEnum(s) || structKind(s) || nullableUnion(doc, ptr) {
		return false
	}

	args, _ := valueArgs(doc, ptr, placeKind(doc, ptr))

	return len(args) > 0
}

// isProperty reports whether ptr is the schema of an object property.
func isProperty(ptr string) bool {
	ends := stepEnds(ptr)
	if len(ends) == 0 {
		return false
	}

	start := 0
	if len(ends) > 1 {
		start = ends[len(ends)-2]
	}

	return strings.HasPrefix(ptr[start:], "/properties/")
}

// checkKeywords reports a validation keyword of s at ptr whose value is
// none the keyword takes: a number for minimum, maximum and their exclusive
// forms, a number above zero for multipleOf, and a non-negative integer for
// the lengths and the counts.
func checkKeywords(ptr string, s *jsonschema.Schema) []error {
	var errs []error

	number := func(kw string, v jsontext.Value) *big.Rat {
		if v == nil {
			return nil
		}

		r, ok := new(big.Rat).SetString(string(v))
		if v.Kind() != '0' || !ok {
			errs = append(errs, unsupported(child(ptr, kw), "%s is no number", v))
			return nil
		}

		return r
	}

	for kw, v := range map[string]jsontext.Value{
		"minimum": s.Minimum, "maximum": s.Maximum,
		"exclusiveMinimum": s.ExclusiveMinimum, "exclusiveMaximum": s.ExclusiveMaximum,
	} {
		number(kw, v)
	}

	if r := number("multipleOf", s.MultipleOf); r != nil && r.Sign() <= 0 {
		errs = append(
			errs,
			unsupported(child(ptr, "multipleOf"), "%s is not above zero", s.MultipleOf),
		)
	}

	for kw, v := range map[string]jsontext.Value{
		"minLength": s.MinLength, "maxLength": s.MaxLength,
		"minItems": s.MinItems, "maxItems": s.MaxItems,
		"minProperties": s.MinProperties, "maxProperties": s.MaxProperties,
	} {
		if r := number(kw, v); r != nil && (!r.IsInt() || r.Sign() < 0) {
			errs = append(errs, unsupported(child(ptr, kw), "%s is no non-negative integer", v))
		}
	}

	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })

	return errs
}

// comparisons returns the comparisons of the bounds by operator, in the
// order >=, >, <=, <, each spelled by text, or "" when there is none.
func comparisons(bounds map[string]jsontext.Value, text func(jsontext.Value) string) string {
	return strings.Join(
		opiter.WrapSeq(slices.Values([]string{">=", ">", "<=", "<"})).
			Filter(func(op string) bool { return bounds[op] != nil }).
			Map(func(op string) string { return op + text(bounds[op]) }).
			CollectSlice(),
		" ",
	)
}

// numberText returns the text of the JSON number v, a Go literal as it is.
func numberText(v jsontext.Value) string { return string(v) }

// integerText returns the JSON number v, an integer that check accepted,
// as a Go integer literal: 2.0 and 2e0 give 2.
func integerText(v jsontext.Value) string {
	r, ok := new(big.Rat).SetString(string(v))
	if !ok || !r.IsInt() {
		return string(v)
	}

	return r.Num().String()
}

// quoteArg spells s as the value of an arg of //kama:validate, a Go string
// literal, so that no character of s ends the arg or the comment: a raw
// literal for printable text holding a double quote, which reads better,
// else an interpreted one.
func quoteArg(s string) string {
	if strings.Contains(s, `"`) && !strings.Contains(s, "`") &&
		!strings.ContainsFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) {
		return "`" + s + "`"
	}

	return strconv.Quote(s)
}
