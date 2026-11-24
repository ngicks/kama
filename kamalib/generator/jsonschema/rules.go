package jsonschema

import (
	"encoding/json/jsontext"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// The functions of this file turn a schema whose value is raw JSON text
// into the args of //kama:validate. Every keyword becomes a validator of
// the value, and a subschema becomes the validators of the part of the
// value it applies to: the operands of a combinator, or those of a
// validator reaching into the members or the elements of the value.

// rawRules returns the args of //kama:validate checking the schema at ptr
// on raw JSON text, as JSON Schema applies it to a value of any kind.
func rawRules(doc *jsonschema.Schema, opts Options, ptr string) ([]string, error) {
	return rulesOf(doc, opts, ptr, false, everyKeyword, lexicalScope(doc, ptr), nil)
}

// rulesOf returns the args checking the keywords of the schema at ptr that
// keep keeps. The subschemas take every keyword. annotated tells that an
// unevaluatedProperties or an unevaluatedItems applies to the same value,
// which then needs what the validators evaluate: a reference is written
// out rather than named, since ref reports no evaluation. scope holds the
// schema resources evaluation entered, the dynamic scope a $dynamicRef
// resolves in. stack holds the schemas whose args are being built, which a
// reference reaches again on a cycle.
func rulesOf(
	doc *jsonschema.Schema,
	opts Options,
	ptr string,
	annotated bool,
	keep func(kw string) bool,
	scope, stack []string,
) ([]string, error) {
	s, err := at(doc, ptr)
	if err != nil {
		return nil, err
	}

	if s.Bool.IsSome() {
		if s.Bool.Value() {
			return nil, nil
		}

		return []string{noArgs("or")}, nil
	}

	scope = enter(doc, scope, ptr)
	stack = append(stack, ptr)
	inPlace := annotated || s.UnevaluatedProperties != nil || s.UnevaluatedItems != nil

	// sub returns the args of the subschema at the keys under ptr applying
	// to a member or an element of the value.
	sub := func(keys ...string) ([]string, error) {
		return rulesOf(doc, opts, child(ptr, keys...), false, everyKeyword, scope, stack)
	}
	// same returns the args of the subschema at the keys under ptr
	// applying to the value itself, as one arg.
	same := func(keys ...string) (string, error) {
		args, err := rulesOf(doc, opts, child(ptr, keys...), inPlace, everyKeyword, scope, stack)
		if err != nil {
			return "", err
		}

		return single(args)
	}

	args := leafRules(opts, s, keep)

	steps := []func() ([]string, error){
		func() ([]string, error) { return propsRules(ptr, s, sub, inPlace, keep) },
		func() ([]string, error) { return itemsRules(s, sub, inPlace, keep) },
		func() ([]string, error) { return containsRules(s, sub, keep) },
		func() ([]string, error) { return keysRules(s, sub, keep) },
		func() ([]string, error) { return dependentRules(s, same, keep) },
		func() ([]string, error) { return listRules(s, same, keep) },
		func() ([]string, error) { return notRules(s, same, keep) },
		func() ([]string, error) { return ifRules(s, same, inPlace, keep) },
		func() ([]string, error) {
			return refRules(doc, opts, ptr, "$ref", inPlace, keep, scope, stack)
		},
		func() ([]string, error) {
			return refRules(doc, opts, ptr, "$dynamicRef", inPlace, keep, scope, stack)
		},
	}

	for _, step := range steps {
		more, err := step()
		if err != nil {
			return nil, err
		}

		args = append(args, more...)
	}

	for _, kw := range []string{"unevaluatedProperties", "unevaluatedItems"} {
		if !slices.Contains(present(s, []string{kw}), kw) || !keep(kw) {
			continue
		}

		rest, err := sub(kw)
		if err != nil {
			return nil, err
		}

		name := "unevalprops"
		if kw == "unevaluatedItems" {
			name = "unevalitems"
		}

		inner, err := single(args)
		if err != nil {
			return nil, err
		}

		arg, err := callArg(name, append([]string{inner}, rest...)...)
		if err != nil {
			return nil, err
		}

		args = []string{arg}
	}

	return distinct(args)
}

// distinct returns args with every validator named once, as a type or a
// field takes it: the args of a validator named twice move into one and,
// in the place of the first, along with the operands of an and among them.
func distinct(args []string) ([]string, error) {
	names := opiter.WrapSeq(slices.Values(args)).
		Map(argName).
		CollectSlice()

	twice := func(name string) bool {
		return opiter.WrapSeq(slices.Values(names)).
			Filter(func(n string) bool { return n == name }).
			Count() > 1
	}

	repeated := opiter.WrapSeq(slices.Values(names)).
		Any(twice)
	if !repeated {
		return args, nil
	}

	var (
		out, joined []string
		at          = -1
	)

	for i, a := range args {
		switch {
		case names[i] == "and":
			kv, err := dircmnt.ParseArgsKeyValue(a)
			if err != nil {
				return nil, err
			}

			joined = append(joined, opiter.WrapSeq(slices.Values(kv[0].Args)).
				Map(dircmnt.KeyValue.String).
				CollectSlice()...)
		case twice(names[i]):
			joined = append(joined, a)
		default:
			out = append(out, a)
			continue
		}

		if at < 0 {
			at = len(out)
		}
	}

	and, err := callArg("and", joined...)
	if err != nil {
		return nil, err
	}

	return slices.Insert(out, at, and), nil
}

// argName returns the name of the validator the arg a names.
func argName(a string) string {
	kv, err := dircmnt.ParseArgsKeyValue(a)
	if err != nil || len(kv) == 0 {
		return a
	}

	return kv[0].Key
}

// leafRules returns the args of the keywords of s holding no subschema that
// keep keeps.
func leafRules(opts Options, s *jsonschema.Schema, keep func(kw string) bool) []string {
	var args []string

	add := func(name, arg string, keywords ...string) {
		if arg != "" && slices.ContainsFunc(keywords, keep) {
			args = append(args, name+"="+quoteArg(arg))
		}
	}
	lengths := func(min, max jsontext.Value) string {
		return comparisons(map[string]jsontext.Value{">=": min, "<=": max}, integerText)
	}

	if !s.Type.IsZero() {
		add("kind", strings.Join(slices.Compact(slices.Clone(s.Type.Names)), " "), "type")
	}

	// A const next to an enum applies too, so each gives a const of its own.
	if s.Const != nil {
		add("const", rawValues([]jsontext.Value{s.Const}), "const")
	}

	if s.Enum != nil && keep("enum") {
		if len(s.Enum) == 0 {
			args = append(args, noArgs("or"))
		} else {
			add("const", rawValues(s.Enum), "enum")
		}
	}

	add("cmp", comparisons(map[string]jsontext.Value{
		">=": s.Minimum, "<=": s.Maximum, ">": s.ExclusiveMinimum, "<": s.ExclusiveMaximum,
	}, numberText), "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum")

	if s.MultipleOf != nil {
		add("multipleof", string(s.MultipleOf), "multipleOf")
	}

	add("runes", lengths(s.MinLength, s.MaxLength), "minLength", "maxLength")
	add("format", assertedFormat(opts, s), "format")

	if s.Pattern.IsSome() {
		if _, err := regexp.Compile(s.Pattern.Value()); err == nil {
			add("regexp", s.Pattern.Value(), "pattern")
		}
	}

	add("length", lengths(s.MinItems, s.MaxItems), "minItems", "maxItems")

	if s.UniqueItems.Value() && keep("uniqueItems") {
		args = append(args, "unique")
	}

	add("members", lengths(s.MinProperties, s.MaxProperties), "minProperties", "maxProperties")

	if len(s.Required) > 0 {
		add("required", spellNames(s.Required), "required")
	}

	if dep := rawDependents(s); dep != "" && keep("dependentRequired") {
		args = append(args, dep)
	}

	return args
}

// propsRules returns props for the properties, the patternProperties and
// the additionalProperties of s that keep keeps. An entry without
// validators, as a property keep leaves out gives, stays only when rest
// needs to know the member is no other one, or when annotated needs what
// the entry evaluates. ptr is the pointer of s.
func propsRules(
	ptr string,
	s *jsonschema.Schema,
	sub func(keys ...string) ([]string, error),
	annotated bool,
	keep func(kw string) bool,
) ([]string, error) {
	var (
		entries []string
		checks  bool
	)

	rest := s.AdditionalProperties != nil && keep("additionalProperties")

	// entry adds the entry kind of the subschema at keys, which names the
	// member name or the pattern name, but for rest.
	entry := func(kind, name string, keys ...string) error {
		var ops []string

		if keep(keys[0]) {
			var err error

			ops, err = sub(keys...)
			if err != nil {
				return err
			}
		}

		checks = checks || len(ops) > 0

		if len(ops) == 0 && kind != "rest" && !rest && !annotated {
			return nil
		}

		if kind != "rest" {
			ops = append([]string{quoteName(name)}, ops...)
		}

		arg, err := callArg(kind, ops...)
		if err != nil {
			return err
		}

		entries = append(entries, arg)

		return nil
	}

	for _, n := range s.Properties {
		if err := entry("field", n.Name, "properties", n.Name); err != nil {
			return nil, err
		}
	}

	for _, n := range s.PatternProperties {
		if !keep("patternProperties") {
			break
		}

		if _, err := regexp.Compile(n.Name); err != nil {
			return nil, unsupported(
				child(ptr, "patternProperties", n.Name), "pattern does not compile as RE2: %v", err,
			)
		}

		if err := entry("match", n.Name, "patternProperties", n.Name); err != nil {
			return nil, err
		}
	}

	if rest {
		if err := entry("rest", "", "additionalProperties"); err != nil {
			return nil, err
		}

		checks = checks || s.AdditionalProperties.Bool.IsNone()
	}

	if len(entries) == 0 || !checks && !annotated {
		return nil, nil
	}

	return oneCall("props", entries...)
}

// itemsRules returns prefix for the prefixItems of s, with the items of s
// as its rest, or items for the items of s alone, as keep keeps them.
func itemsRules(
	s *jsonschema.Schema,
	sub func(keys ...string) ([]string, error),
	annotated bool,
	keep func(kw string) bool,
) ([]string, error) {
	var rest []string

	if s.Items != nil && keep("items") {
		var err error

		rest, err = sub("items")
		if err != nil {
			return nil, err
		}
	}

	if s.PrefixItems == nil || !keep("prefixItems") {
		if s.Items == nil || !keep("items") || len(rest) == 0 && !annotated {
			return nil, nil
		}

		return oneCall("items", rest...)
	}

	var ops []string

	for i := range s.PrefixItems {
		args, err := sub("prefixItems", strconv.Itoa(i))
		if err != nil {
			return nil, err
		}

		op, err := single(args)
		if err != nil {
			return nil, err
		}

		ops = append(ops, op)
	}

	if s.Items != nil && keep("items") {
		op, err := callArg("rest", rest...)
		if err != nil {
			return nil, err
		}

		ops = append(ops, op)
	}

	return oneCall("prefix", ops...)
}

// containsRules returns contains for the contains of s, counted by its
// minContains, 1 by default, and its maxContains.
func containsRules(
	s *jsonschema.Schema,
	sub func(keys ...string) ([]string, error),
	keep func(kw string) bool,
) ([]string, error) {
	if s.Contains == nil || !keep("contains") {
		return nil, nil
	}

	ops, err := sub("contains")
	if err != nil {
		return nil, err
	}

	counts := []string{">=1"}
	if s.MinContains != nil {
		counts = []string{">=" + integerText(s.MinContains)}
	}

	if s.MaxContains != nil {
		counts = append(counts, "<="+integerText(s.MaxContains))
	}

	return oneCall("contains", append(counts, ops...)...)
}

// keysRules returns keys for the propertyNames of s.
func keysRules(
	s *jsonschema.Schema,
	sub func(keys ...string) ([]string, error),
	keep func(kw string) bool,
) ([]string, error) {
	if s.PropertyNames == nil || !keep("propertyNames") {
		return nil, nil
	}

	ops, err := sub("propertyNames")
	if err != nil || len(ops) == 0 {
		return nil, err
	}

	return oneCall("keys", ops...)
}

// dependentRules returns, for each key of the dependentSchemas of s,
// if(required=<key>, <schema>).
func dependentRules(
	s *jsonschema.Schema,
	same func(keys ...string) (string, error),
	keep func(kw string) bool,
) ([]string, error) {
	if !keep("dependentSchemas") {
		return nil, nil
	}

	var args []string

	for _, n := range s.DependentSchemas {
		then, err := same("dependentSchemas", n.Name)
		if err != nil {
			return nil, err
		}

		arg, err := callArg("if", "required="+quoteArg(spellNames([]string{n.Name})), then)
		if err != nil {
			return nil, err
		}

		args = append(args, arg)
	}

	return args, nil
}

// listRules returns and, or and oneof for the allOf, anyOf and oneOf of
// s, each member one operand.
func listRules(
	s *jsonschema.Schema,
	same func(keys ...string) (string, error),
	keep func(kw string) bool,
) ([]string, error) {
	var args []string

	for _, kw := range []string{"allOf", "anyOf", "oneOf"} {
		members := listOf(s, kw)
		if members == nil || !keep(kw) {
			continue
		}

		ops := make([]string, 0, len(members))

		for i := range members {
			op, err := same(kw, strconv.Itoa(i))
			if err != nil {
				return nil, err
			}

			ops = append(ops, op)
		}

		arg, err := callArg(
			map[string]string{"allOf": "and", "anyOf": "or", "oneOf": "oneof"}[kw],
			ops...)
		if err != nil {
			return nil, err
		}

		args = append(args, arg)
	}

	return args, nil
}

// notRules returns not for the not of s.
func notRules(
	s *jsonschema.Schema,
	same func(keys ...string) (string, error),
	keep func(kw string) bool,
) ([]string, error) {
	if s.Not == nil || !keep("not") {
		return nil, nil
	}

	op, err := same("not")
	if err != nil {
		return nil, err
	}

	return oneCall("not", op)
}

// ifRules returns if for the if, then and else of s. An if without then
// and else checks nothing, but it evaluates when it holds, which annotated
// needs.
func ifRules(
	s *jsonschema.Schema,
	same func(keys ...string) (string, error),
	annotated bool,
	keep func(kw string) bool,
) ([]string, error) {
	if s.If == nil || !keep("if") || s.Then == nil && s.Else == nil && !annotated {
		return nil, nil
	}

	ops := make([]string, 0, 3)

	for _, kw := range []string{"if", "then", "else"} {
		present := map[string]bool{"if": true, "then": s.Then != nil, "else": s.Else != nil}[kw]

		switch {
		case present:
			op, err := same(kw)
			if err != nil {
				return nil, err
			}

			ops = append(ops, op)
		case kw == "then":
			ops = append(ops, noArgs("and"))
		}
	}

	return oneCall("if", ops...)
}

// refRules returns the args the keyword kw, $ref or $dynamicRef, of the
// schema at ptr applies: ref or decode naming the type of its target, else
// the args of the target written out. A $dynamicRef resolves in scope, as
// dynamicTarget tells. The target is written out when annotated asks for
// what it evaluates, off the cycle of stack, since ref reports no
// evaluation, and when the dynamic scope would resolve the $dynamicRef
// under the target otherwise than the type of the target does, as
// scopeKeeps tells.
func refRules(
	doc *jsonschema.Schema,
	opts Options,
	ptr, kw string,
	annotated bool,
	keep func(kw string) bool,
	scope, stack []string,
) ([]string, error) {
	s, err := at(doc, ptr)
	if err != nil || !keep(kw) {
		return nil, err
	}

	if _, ok := refOf(s, kw); !ok {
		return nil, nil
	}

	target, err := refTargetOf(doc, ptr, kw)
	if err == nil && kw == "$dynamicRef" {
		target, err = dynamicTarget(doc, ptr, target, scope)
	}

	if err != nil {
		return nil, err
	}

	onCycle := slices.Contains(stack, target)
	keeps := scopeKeeps(doc, scope, target)

	if name := namedBy(doc, target); name != "" && keeps && (onCycle || !annotated) {
		t, err := typeName(doc, opts, target)
		if err != nil {
			return nil, err
		}

		return []string{name + "=" + t}, nil
	}

	if onCycle {
		return nil, unsupported(child(ptr, kw),
			"reference closes a cycle through a union, which validates on no decoding,"+
				" a schema declaring no type, or a type the dynamic scope resolves otherwise")
	}

	return rulesOf(doc, opts, target, annotated, everyKeyword, scope, stack)
}

// namedBy returns the validator applying the type the schema at ptr
// declares to raw JSON text, or "" for none: ref for a type of raw JSON
// text, whose Validate it calls, and decode for a type whose decoder
// validates the value. The root declares a type, and its decoder validates
// a union it holds. A union elsewhere validates on no decoding.
func namedBy(doc *jsonschema.Schema, ptr string) string {
	switch {
	case ptr != "" && !declares(doc, ptr):
		return ""
	case raw(doc, ptr):
		return "ref"
	}

	s, err := at(doc, ptr)
	if err != nil || ptr != "" && union(s) {
		return ""
	}

	return "decode"
}

// single returns args as one arg: the only arg, else and(...) of them.
func single(args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}

	return callArg("and", args...)
}

// oneCall returns the call name(args...) as the only arg.
func oneCall(name string, args ...string) ([]string, error) {
	arg, err := callArg(name, args...)
	if err != nil {
		return nil, err
	}

	return []string{arg}, nil
}

// callArg spells the call name(args...), each of args the text of one arg
// of //kama:validate. An arg dircmnt does not read back is an error.
func callArg(name string, args ...string) (string, error) {
	kvs, err := opiter.TryCollect(
		opiter.WrapSeq(slices.Values(args)).
			Disassemble(dircmnt.ParseArgsKeyValue),
	)
	if err != nil {
		return "", err
	}

	return dircmnt.KeyValue{Key: name, Call: true, Args: slices.Concat(kvs...)}.String(), nil
}

// noArgs spells the call name() without args.
func noArgs(name string) string { return dircmnt.KeyValue{Key: name, Call: true}.String() }

// spellNames spells names as the arg of required on raw JSON text, each
// quoted, so that no name reads back as a comparison or as nothing.
func spellNames(names []string) string {
	return dircmnt.SpellArgs(
		opiter.WrapSeq(slices.Values(names)).
			Map(func(n string) dircmnt.KeyValue { return dircmnt.KeyValue{Key: n, KeyQuoted: true} }).
			CollectSlice(),
	)
}

// quoteName spells the member name or the pattern n as the first arg of
// field or match, quoted so that no name reads as a validator.
func quoteName(n string) string {
	return dircmnt.KeyValue{Key: n, KeyQuoted: true}.String()
}
