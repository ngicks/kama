package jsonschema

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/generator/validator/ext/basic"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// The functions of this file turn the validation keywords of a schema into
// the args of //kama:validate. A keyword applies to the values of one kind
// of JSON value only, as minLength to strings, so the args depend on the
// kind of the Go value the directive validates. The kinds are those of
// placeKind.

// ErrUnenforced is the warning of a validation keyword the generated code
// does not check, such as minLength next to a oneOf or a pattern RE2 does
// not compile. The warning names the JSON pointer of the keyword.
var ErrUnenforced = errors.New("jsonschema: keyword not enforced")

// placeKind returns the kind of the Go value of the schema at ptr, as
// validators see it: string, integer, number, boolean, array (a slice), map,
// struct (a struct or a product) or raw (raw JSON text, as raw tells). It
// follows references, an allOf of one member and the value of a nullable
// union. It is empty for a union, which no validator checks.
func placeKind(doc *jsonschema.Schema, ptr string) string {
	r, s, err := resolvedValue(doc, ptr)
	if err != nil || union(s) && !nullableType(s) {
		return ""
	}

	switch {
	case structKind(s) || product(doc, r, s):
		return "struct"
	case raw(doc, r):
		return "raw"
	case objectKind(s) && mapOnly(s):
		return "map"
	}

	k, err := kinds(doc, r)
	if err != nil {
		return ""
	}

	nonNull := opiter.SlicesValues(k).
		Filter(func(k string) bool { return k != "null" }).
		CollectSlice()

	if len(nonNull) != 1 || nonNull[0] == "object" {
		return ""
	}

	return nonNull[0]
}

// valueArgs returns the args of //kama:validate checking the validation
// keywords of the schema at ptr itself on a Go value of kind, and the
// keywords that apply to kind and that no arg checks: those kindArgs
// gives, then json(...) for the keywords the Go type leaves to the
// validators of raw JSON text, as jsonArgs tells.
func valueArgs(doc *jsonschema.Schema, opts Options, ptr, kind string) ([]expr.Arg, []string) {
	args, unchecked := kindArgs(doc, opts, ptr, kind)
	if kind == "" || kind == "raw" {
		return args, unchecked
	}

	bridge, _ := jsonArgs(doc, opts, ptr)

	return append(args, bridge...), unchecked
}

// kindArgs returns the args of //kama:validate checking the validation
// keywords of the schema at ptr itself on a Go value of kind, and the
// keywords that apply to kind and that no arg checks. A keyword applying
// to another kind of value holds for a value of kind, so it gives neither.
// The args of a struct are those of the struct as a whole, which typeArgs
// gives. The args of raw JSON text are those rawRules gives, which check
// every keyword but a pattern RE2 does not compile.
func kindArgs(doc *jsonschema.Schema, opts Options, ptr, kind string) ([]expr.Arg, []string) {
	s, err := at(doc, ptr)
	if err != nil {
		return nil, nil
	}

	if kind == "raw" {
		args, _ := rawRules(doc, opts, ptr)
		return args, rawUnchecked(s)
	}

	if s.Bool.IsSome() {
		return nil, nil
	}

	var (
		args      []expr.Arg
		unchecked []string
	)

	switch kind {
	case "integer", "number":
		if c, ok := bounds(
			s.Minimum,
			s.ExclusiveMinimum,
			s.Maximum,
			s.ExclusiveMaximum,
			numberText,
		); ok {
			args = appendRule(args, basic.CmpArgs(c))
		}

		if s.MultipleOf != nil {
			args = appendRule(
				args,
				basic.MultipleOfArgs{N: jsontext.Value(numberText(s.MultipleOf))},
			)
		}
	case "string":
		if c, ok := lengths(s.MinLength, s.MaxLength); ok {
			args = appendRule(args, basic.RunesArgs(c))
		}

		if f := assertedFormat(opts, s); f != "" {
			args = appendRule(args, basic.FormatArgs{Name: f})
		}

		if s.Pattern.IsSome() {
			if _, err := regexp.Compile(s.Pattern.Value()); err != nil {
				unchecked = append(unchecked, "pattern")
			} else {
				args = appendRule(args, basic.RegexpArgs{Pattern: s.Pattern.Value()})
			}
		}
	case "array":
		if c, ok := lengths(s.MinItems, s.MaxItems); ok {
			args = appendRule(args, basic.LengthArgs(c))
		}

		if s.UniqueItems.Value() && !bridged(doc, ptr, s, "uniqueItems") {
			args = appendRule(args, basic.UniqueArgs{})
		}
	case "map":
		if c, ok := lengths(s.MinProperties, s.MaxProperties); ok {
			args = appendRule(args, basic.LengthArgs(c))
		}
	case "":
		unchecked = append(unchecked, presentValidation(s)...)
	}

	if allowed, err := allowedValues(doc, ptr); err == nil && allowed != nil {
		switch kind {
		case "string", "integer", "number", "boolean":
			args = appendRule(args, basic.ConstArgs{Values: literals(allowed)})
		}
	}

	return args, slices.Compact(slices.Sorted(slices.Values(unchecked)))
}

// rawUnchecked returns pattern when s holds a pattern RE2 does not compile,
// which the args of raw JSON text leave out.
func rawUnchecked(s *jsonschema.Schema) []string {
	if s.Bool.IsNone() && s.Pattern.IsSome() {
		if _, err := regexp.Compile(s.Pattern.Value()); err != nil {
			return []string{"pattern"}
		}
	}

	return nil
}

// rawDependents returns the arg checking the dependentRequired of s on raw
// JSON text: for each key, required(<members>) if required(<key>) else
// always, all joined by and when there are several. It returns false when
// no key requires a member.
func rawDependents(s *jsonschema.Schema) (expr.Arg, bool) {
	var conds []expr.Arg

	for key, deps := range opiter.MapsSorted(s.DependentRequired) {
		if len(deps) > 0 {
			conds = append(conds, expr.Arg{Value: basic.Cond(
				node(basic.RequiredArgs{Names: []string{key}}),
				node(basic.RequiredArgs{Names: deps}),
				nil,
			)})
		}
	}

	if len(conds) == 0 {
		return expr.Arg{}, false
	}

	return single(conds), true
}

// rawValues returns the values of const on raw JSON text: values but nil
// and those no literal spells. check rejects a document holding a value no
// literal spells before any rule is written.
func rawValues(values []jsontext.Value) []jsontext.Value {
	return opiter.SlicesValues(values).
		Filter(func(v jsontext.Value) bool {
			_, err := expr.FromJson(v)
			return v != nil && err == nil
		}).
		CollectSlice()
}

// literals returns lits, Go literals of a string, a number or a bool as
// goLiteral spells them, as the JSON values of const.
func literals(lits []string) []jsontext.Value {
	return opiter.SlicesValues(lits).
		Map(func(lit string) jsontext.Value {
			if v, err := strconv.Unquote(lit); err == nil && strings.HasPrefix(lit, `"`) {
				text, _ := jsonv2.Marshal(v)
				return text
			}

			return jsontext.Value(lit)
		}).
		CollectSlice()
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

	return opiter.MapsSorted(has).
		Filter(func(_ string, ok bool) bool { return ok }).
		OmitL().
		CollectSlice()
}

// typeArgs returns the args of //kama:validate on the type the schema at
// ptr declares, and the keywords that apply to its value and that no arg
// checks. A struct or a product takes hook=False, its hooks being unwritten,
// and the validators of the object as a whole. A union takes none, its
// holder having a Validate of its own, and its wrappers take the
// validators of its keywords, as unionArgs tells.
func typeArgs(doc *jsonschema.Schema, opts Options, ptr string) ([]expr.Arg, []string) {
	s, err := at(doc, ptr)
	if err != nil {
		return nil, nil
	}

	if union(s) && !nullable(doc, ptr) {
		return nil, rawUnchecked(s)
	}

	if !structKind(s) && !product(doc, ptr, s) {
		if ptr == "" && nullable(doc, ptr) && !raw(doc, ptr) {
			// The root is an opt.Option, a struct of another package, which
			// takes no validator of its value.
			_, unchecked := valueArgs(doc, opts, ptr, "")
			return nil, unchecked
		}

		return valueArgs(doc, opts, ptr, placeKind(doc, ptr))
	}

	args := []expr.Arg{ext.Hook(false)}

	if closed(s) && !bridged(doc, ptr, s, "additionalProperties") {
		args = appendRule(args, basic.ClosedArgs{})
	}

	if c, ok := lengths(
		s.MinProperties,
		s.MaxProperties,
	); ok &&
		!bridged(doc, ptr, s, "minProperties") {
		args = appendRule(args, basic.MembersArgs(c))
	}

	bridge, _ := jsonArgs(doc, opts, ptr)

	return append(args, bridge...), nil
}

// unionArgs returns the args of //kama:validate on each wrapper of the
// union at ptr: hook=False, its hooks being unwritten, and the validators
// of the keywords of the union, which check the value of every member.
func unionArgs(doc *jsonschema.Schema, opts Options, ptr string) []expr.Arg {
	bridge, _ := jsonArgs(doc, opts, ptr)
	return append([]expr.Arg{ext.Hook(false)}, bridge...)
}

// declArgs returns the args of //kama:validate on the type the schema at
// ptr declares.
func declArgs(doc *jsonschema.Schema, opts Options, ptr string) []expr.Arg {
	args, _ := typeArgs(doc, opts, ptr)
	return args
}

// fieldArgs returns the args of //kama:validate on the field of the
// property name of the struct or product at ptr: the validation keywords
// of the property schema itself, unless it declares a type of its own, and
// what the dependentRequired of the object asks of the field. The keywords
// of a schema the property refers to are those of the type it declares.
func fieldArgs(doc *jsonschema.Schema, opts Options, ptr, name string) ([]expr.Arg, error) {
	pp := child(ptr, "properties", name)

	var args []expr.Arg

	if kind := placeKind(doc, pp); !declares(doc, pp) && kind != "struct" && !named(doc, pp) {
		args, _ = valueArgs(doc, opts, pp, kind)
	}

	deps := dependentArgs(doc, ptr, name)

	// A validator named by both would be given twice. The args of the value
	// in one and apply to the value the field holds, as each of them does.
	clash := opiter.SlicesValues(deps).
		Map(argName).
		Any(func(n string) bool {
			return slices.ContainsFunc(args, func(a expr.Arg) bool { return argName(a) == n })
		})
	if clash {
		args = []expr.Arg{single(args)}
	}

	return append(args, deps...), nil
}

// named reports whether the schema at ptr is a $ref alone to a schema
// whose Go type is named: a type it declares, or a root of raw JSON text.
// The Validate of that type checks the value, so the place names no
// validator of the target again.
func named(doc *jsonschema.Schema, ptr string) bool {
	s, err := at(doc, ptr)
	if err != nil || !refAloneAt(doc, ptr, s) {
		return false
	}

	r, _, err := resolved(doc, ptr)

	return err == nil && (declares(doc, r) || r == "" && raw(doc, r))
}

// dependentArgs returns the args the dependentRequired of the object at
// ptr gives the field of its property name. A required member is always
// present after decoding, so requires names only the optional members a
// present optional member depends on. The members a required member
// depends on are required themselves. A key or a member without a
// property is no field; typeArgs reports it.
func dependentArgs(doc *jsonschema.Schema, ptr, name string) []expr.Arg {
	s, err := at(doc, ptr)
	if err != nil || bridged(doc, ptr, s, "dependentRequired") {
		return nil
	}

	declared := func(n string) bool { _, ok := s.Properties.Get(n); return ok }
	optional := func(n string) bool { return !slices.Contains(s.Required, n) }

	var names []string

	if deps, ok := s.DependentRequired[name]; ok && optional(name) && declared(name) &&
		!slices.ContainsFunc(deps, func(n string) bool { return !declared(n) }) {
		names = opiter.SlicesValues(deps).
			Filter(optional).
			CollectSlice()
	}

	requiredBy := opiter.SlicesValues(slices.Sorted(maps.Keys(s.DependentRequired))).
		Filter(func(k string) bool { return !optional(k) && declared(k) }).
		Any(func(k string) bool { return slices.Contains(s.DependentRequired[k], name) })

	var args []expr.Arg

	if len(names) > 0 {
		args = appendRule(args, basic.RequiresArgs{Names: names})
	}

	if requiredBy && optional(name) {
		args = appendRule(args, basic.RequiredArgs{})
	}

	return args
}

// unenforcedKeywords returns a warning for each validation keyword of the
// schemas of doc that the generated code does not check.
func unenforcedKeywords(doc *jsonschema.Schema, opts Options) []error {
	var warnings []error

	for ptr := range schemas(doc) {
		for _, kw := range uncheckedAt(doc, opts, ptr) {
			warnings = append(warnings, fmt.Errorf("%w: #%s", ErrUnenforced, child(ptr, kw)))
		}
	}

	return warnings
}

// uncheckedAt returns the validation keywords of the schema at ptr that no
// directive checks: the keywords of a union, those a declared type or a
// field cannot take, those of a schema that is neither, a format opts
// asserts in a schema that is neither, and a pattern RE2 does not compile
// in the validators of raw JSON text.
func uncheckedAt(doc *jsonschema.Schema, opts Options, ptr string) []string {
	s, err := at(doc, ptr)

	switch {
	case err != nil:
		return nil
	case ruled(doc, ptr):
		return rawUnchecked(s)
	case declares(doc, ptr) || ptr == "":
		_, unchecked := typeArgs(doc, Options{}, ptr)
		return unchecked
	case isProperty(ptr):
		kind := placeKind(doc, ptr)
		if kind == "struct" {
			kind = ""
		}

		_, unchecked := valueArgs(doc, Options{}, ptr, kind)

		return unchecked
	}

	_, unchecked := valueArgs(doc, Options{}, ptr, "")
	if assertedFormat(opts, s) != "" {
		unchecked = append(unchecked, "format")
	}

	return unchecked
}

// valueType reports whether the schema at ptr declares a type defined over
// its built-in Go type, whose Validate checks its validation keywords: it
// is no object property, whose field takes them, it declares no type of
// another shape, takes no type from another schema and sits in the
// validators of no other schema, and a keyword applies to its value.
func valueType(doc *jsonschema.Schema, ptr string) bool {
	if ptr == "" || isProperty(ptr) {
		return false
	}

	s, err := at(doc, ptr)
	if err != nil || s.Bool.IsSome() || refAloneAt(doc, ptr, s) || transparentAllOf(doc, ptr, s) ||
		nullableUnion(doc, ptr) || unionMember(ptr) && nullMember(doc, ptr) || ruled(doc, ptr) {
		return false
	}

	// The args of raw JSON text may name this very type through ref, so a
	// keyword rather than an arg tells that it needs a Validate.
	if raw(doc, ptr) {
		return constrains(s)
	}

	if union(s) || product(doc, ptr, s) || stringEnum(s) || structKind(s) {
		return false
	}

	// The args of json(...) may name this very type through decode, so the
	// keywords it checks rather than its args tell.
	args, _ := kindArgs(doc, Options{}, ptr, placeKind(doc, ptr))

	return len(args) > 0 || len(bridging(doc, ptr, s)) > 0
}

// unionMember reports whether ptr is the schema of a member of a oneOf or
// an anyOf. A member admitting null only declares no type, its wrapper
// standing for it.
func unionMember(ptr string) bool {
	ends := stepEnds(ptr)
	if len(ends) == 0 {
		return false
	}

	start := 0
	if len(ends) > 1 {
		start = ends[len(ends)-2]
	}

	return strings.HasPrefix(ptr[start:], "/oneOf/") || strings.HasPrefix(ptr[start:], "/anyOf/")
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
// the lengths and the counts. It also reports a multipleOf the validator
// multipleof does not parse, a number below 1 too small to hold exactly.
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

	literal := func(kw string, v jsontext.Value) {
		if _, ok := numberLiteral(v); !ok {
			errs = append(errs, unsupported(child(ptr, kw),
				"%s has no number literal of //kama:validate: a float literal holds no value"+
					" beyond float64, and an integer literal holds at most %d digits",
				v, maxLiteralDigits))
		}
	}

	for kw, v := range map[string]jsontext.Value{
		"minimum": s.Minimum, "maximum": s.Maximum,
		"exclusiveMinimum": s.ExclusiveMinimum, "exclusiveMaximum": s.ExclusiveMaximum,
	} {
		if number(kw, v) != nil {
			literal(kw, v)
		}
	}

	for i, v := range slices.Concat([]jsontext.Value{s.Const}, s.Enum) {
		if _, err := expr.FromJson(v); v != nil && err != nil {
			at := child(ptr, "const")
			if s.Const == nil || i > 0 {
				at = child(ptr, "enum", strconv.Itoa(i-1))
			}

			errs = append(errs, unsupported(at, "%v", err))
		}
	}

	if r := number("multipleOf", s.MultipleOf); r != nil && r.Sign() > 0 {
		literal("multipleOf", s.MultipleOf)
	}

	if r := number("multipleOf", s.MultipleOf); r != nil && r.Sign() <= 0 {
		errs = append(
			errs,
			unsupported(child(ptr, "multipleOf"), "%s is not above zero", s.MultipleOf),
		)
	} else if r != nil {
		c, err := basic.MultipleOfArgs{N: jsontext.Value(numberText(s.MultipleOf))}.Call()
		if err == nil {
			_, err = (basic.MultipleOf{}).Parse(c, ext.NewRegistry())
		}

		if err != nil {
			errs = append(errs, unsupported(child(ptr, "multipleOf"), "%v", err))
		}
	}

	for kw, v := range map[string]jsontext.Value{
		"minLength": s.MinLength, "maxLength": s.MaxLength,
		"minItems": s.MinItems, "maxItems": s.MaxItems,
		"minProperties": s.MinProperties, "maxProperties": s.MaxProperties,
		"minContains": s.MinContains, "maxContains": s.MaxContains,
	} {
		if r := number(kw, v); r != nil && (!r.IsInt() || r.Sign() < 0) {
			errs = append(errs, unsupported(child(ptr, kw), "%s is no non-negative integer", v))
		}
	}

	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })

	return errs
}

// bounds returns the comparisons with the bounds ge, gt, le and lt, each
// number spelled by text, and whether a bound is not nil.
func bounds(
	ge, gt, le, lt jsontext.Value,
	text func(jsontext.Value) string,
) (basic.CompareArgs, bool) {
	spell := func(v jsontext.Value) jsontext.Value {
		if v == nil {
			return nil
		}

		return jsontext.Value(text(v))
	}

	c := basic.CompareArgs{Ge: spell(ge), Gt: spell(gt), Le: spell(le), Lt: spell(lt)}

	return c, ge != nil || gt != nil || le != nil || lt != nil
}

// lengths returns the comparisons with the bounds min and max of a length
// or a count, as bounds does.
func lengths(min, max jsontext.Value) (basic.CompareArgs, bool) {
	return bounds(min, nil, max, nil, integerText)
}

// numberText returns the text of the JSON number v as a number literal of
// //kama:validate, which numberLiteral tells, or the text of v as it is.
func numberText(v jsontext.Value) string {
	text, _ := numberLiteral(v)
	return text
}

// maxLiteralDigits bounds the digits of an integer literal spelling a JSON
// number that a float literal cannot, as 1e400. A longer literal would fill
// the comment with digits.
const maxLiteralDigits = 1024

// numberLiteral returns the JSON number v spelled as a number literal of
// //kama:validate: its text when a Starlark literal spells it, else the
// integer it is when that integer has at most maxLiteralDigits digits. It
// returns false for any other number, as 1e-400 or 1e100000.
func numberLiteral(v jsontext.Value) (string, bool) {
	if _, err := expr.FromJson(v); err == nil {
		return string(v), true
	}

	r, ok := new(big.Rat).SetString(string(v))
	if !ok || !r.IsInt() {
		return string(v), false
	}

	// The digits are counted before they are written out.
	if new(big.Int).Abs(r.Num()).BitLen() > maxLiteralDigits*3 {
		return string(v), false
	}

	text := r.Num().String()

	return text, len(strings.TrimPrefix(text, "-")) <= maxLiteralDigits
}

// integerText returns the JSON number v, an integer that check accepted,
// as a Go integer literal: 2.0 and 2e0 give 2. An integer above the
// greatest int64 gives 2^63, since the validators of a length or a count
// fold every such bound alike and 1e100000 would spell 100001 digits.
func integerText(v jsontext.Value) string {
	r, ok := new(big.Rat).SetString(string(v))
	if !ok || !r.IsInt() {
		return string(v)
	}

	beyond := new(big.Int).Lsh(big.NewInt(1), 63)
	if r.Num().Cmp(beyond) > 0 {
		return beyond.String()
	}

	return r.Num().String()
}
