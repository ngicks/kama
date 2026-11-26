package jsonschema

import (
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext/basic"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// The functions of this file turn a schema whose value is raw JSON text
// into the args of //kama:validate. Every keyword becomes a validator of
// the value, and a subschema becomes the validators of the part of the
// value it applies to: the operands of a combinator, or those of a
// validator reaching into the members or the elements of the value.

// rawRules returns the args of //kama:validate checking the schema at ptr
// on raw JSON text, as JSON Schema applies it to a value of any kind. When
// doc holds an unevaluatedProperties or an unevaluatedItems, ref may run
// the args through ValidateEvaluated of the type for one of them, so they
// keep the entries that only record what they evaluate.
func rawRules(doc *jsonschema.Schema, opts Options, ptr string) ([]expr.Arg, error) {
	return rulesOf(doc, opts, ptr, evaluates(doc), everyKeyword, lexicalScope(doc, ptr), nil)
}

// evaluates reports whether a schema of doc holds unevaluatedProperties or
// unevaluatedItems.
func evaluates(doc *jsonschema.Schema) bool {
	return opiter.WrapSeq2(schemas(doc)).
		Any(func(_ string, s *jsonschema.Schema) bool {
			return s != nil && (s.UnevaluatedProperties != nil || s.UnevaluatedItems != nil)
		})
}

// rulesOf returns the args checking the keywords of the schema at ptr that
// keep keeps. The subschemas take every keyword. annotated tells that an
// unevaluatedProperties or an unevaluatedItems applies to the same value,
// which then needs what the validators evaluate: a reference to a type
// that decode names is written out rather than named, since decode reports
// no evaluation. scope holds the
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
) ([]expr.Arg, error) {
	s, err := at(doc, ptr)
	if err != nil {
		return nil, err
	}

	if s.Bool.IsSome() {
		if s.Bool.Value() {
			return nil, nil
		}

		return appendRule(nil, basic.NeverArgs{}), nil
	}

	scope = enter(doc, scope, ptr)
	stack = append(stack, ptr)
	inPlace := annotated || s.UnevaluatedProperties != nil || s.UnevaluatedItems != nil

	// sub returns the args of the subschema at the keys under ptr applying
	// to a member or an element of the value.
	sub := func(keys ...string) ([]expr.Arg, error) {
		return rulesOf(doc, opts, child(ptr, keys...), false, everyKeyword, scope, stack)
	}
	// same returns the args of the subschema at the keys under ptr
	// applying to the value itself, as one arg.
	same := func(keys ...string) (expr.Arg, error) {
		args, err := rulesOf(doc, opts, child(ptr, keys...), inPlace, everyKeyword, scope, stack)
		if err != nil {
			return expr.Arg{}, err
		}

		return single(args), nil
	}

	args := leafRules(opts, s, keep)

	steps := []func() ([]expr.Arg, error){
		func() ([]expr.Arg, error) { return propsRules(ptr, s, sub, inPlace, keep) },
		func() ([]expr.Arg, error) { return itemsRules(s, sub, inPlace, keep) },
		func() ([]expr.Arg, error) { return containsRules(s, sub, keep) },
		func() ([]expr.Arg, error) { return keysRules(s, sub, keep) },
		func() ([]expr.Arg, error) { return dependentRules(s, same, keep) },
		func() ([]expr.Arg, error) { return listRules(s, same, keep) },
		func() ([]expr.Arg, error) { return notRules(s, same, keep) },
		func() ([]expr.Arg, error) { return ifRules(s, same, inPlace, keep) },
		func() ([]expr.Arg, error) {
			return refRules(doc, opts, ptr, "$ref", inPlace, keep, scope, stack)
		},
		func() ([]expr.Arg, error) {
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

		scope, ops := single(args).Value, values(rest)

		var uneval spelled = basic.UnevalPropsArgs{Scope: scope, Operands: ops}
		if kw == "unevaluatedItems" {
			uneval = basic.UnevalItemsArgs{Scope: scope, Operands: ops}
		}

		args = appendRule(nil, uneval)
	}

	return distinct(args), nil
}

// distinct returns args with every validator named once, as a type or a
// field takes it: the args of a validator named twice move into one and,
// in the place of the first, along with the operands of an and among them.
func distinct(args []expr.Arg) []expr.Arg {
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
		return args
	}

	var (
		out, joined []expr.Arg
		at          = -1
	)

	for i, a := range args {
		_, isAnd := a.Value.(expr.And)

		switch {
		case isAnd:
			joined = opiter.WrapSeq(slices.Values(a.Value.(expr.And).Operands)).
				Map(func(n expr.Node) expr.Arg { return expr.Arg{Value: n} }).
				AppendSeq(joined)
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

	return slices.Insert(out, at, single(joined))
}

// argName returns the name of the validator the arg a names: the name of a
// call, or the type of the node of an operator, which no name of a
// validator equals.
func argName(a expr.Arg) string {
	if c, ok := a.Value.(expr.Call); ok {
		return c.Name
	}

	return reflect.TypeOf(a.Value).String()
}

// leafRules returns the args of the keywords of s holding no subschema that
// keep keeps.
func leafRules(opts Options, s *jsonschema.Schema, keep func(kw string) bool) []expr.Arg {
	var args []expr.Arg

	add := func(a spelled, keywords ...string) {
		if slices.ContainsFunc(keywords, keep) {
			args = appendRule(args, a)
		}
	}

	if len(s.Type.Names) > 0 {
		add(basic.KindArgs{Kinds: slices.Compact(slices.Clone(s.Type.Names))}, "type")
	}

	// A const next to an enum applies too, so each gives a const of its own.
	if v := rawValues([]jsontext.Value{s.Const}); len(v) > 0 {
		add(basic.ConstArgs{Values: v}, "const")
	}

	if s.Enum != nil && keep("enum") {
		if len(s.Enum) == 0 {
			args = appendRule(args, basic.NeverArgs{})
		} else if v := rawValues(s.Enum); len(v) > 0 {
			add(basic.ConstArgs{Values: v}, "enum")
		}
	}

	if c, ok := bounds(
		s.Minimum,
		s.ExclusiveMinimum,
		s.Maximum,
		s.ExclusiveMaximum,
		numberText,
	); ok {
		add(basic.CmpArgs(c), "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum")
	}

	if s.MultipleOf != nil {
		add(basic.MultipleOfArgs{N: jsontext.Value(numberText(s.MultipleOf))}, "multipleOf")
	}

	if c, ok := lengths(s.MinLength, s.MaxLength); ok {
		add(basic.RunesArgs(c), "minLength", "maxLength")
	}

	if f := assertedFormat(opts, s); f != "" {
		add(basic.FormatArgs{Name: f}, "format")
	}

	if s.Pattern.IsSome() {
		if _, err := regexp.Compile(s.Pattern.Value()); err == nil {
			add(basic.RegexpArgs{Pattern: s.Pattern.Value()}, "pattern")
		}
	}

	if c, ok := lengths(s.MinItems, s.MaxItems); ok {
		add(basic.LengthArgs(c), "minItems", "maxItems")
	}

	if s.UniqueItems.Value() && keep("uniqueItems") {
		args = appendRule(args, basic.UniqueArgs{})
	}

	if c, ok := lengths(s.MinProperties, s.MaxProperties); ok {
		add(basic.MembersArgs(c), "minProperties", "maxProperties")
	}

	if len(s.Required) > 0 {
		add(basic.RequiredArgs{Names: s.Required}, "required")
	}

	if dep, ok := rawDependents(s); ok && keep("dependentRequired") {
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
	sub func(keys ...string) ([]expr.Arg, error),
	annotated bool,
	keep func(kw string) bool,
) ([]expr.Arg, error) {
	var (
		entries []expr.Node
		checks  bool
	)

	rest := s.AdditionalProperties != nil && keep("additionalProperties")

	// entry adds the entry spell gives for the validators of the subschema
	// at keys. isRest tells the entry rest from field and match.
	entry := func(spell func(ops []expr.Node) spelled, isRest bool, keys ...string) error {
		var ops []expr.Arg

		if keep(keys[0]) {
			var err error

			ops, err = sub(keys...)
			if err != nil {
				return err
			}
		}

		checks = checks || len(ops) > 0

		if len(ops) == 0 && !isRest && !rest && !annotated {
			return nil
		}

		entries = append(entries, node(spell(values(ops))))

		return nil
	}

	for _, n := range s.Properties {
		field := func(ops []expr.Node) spelled { return basic.FieldArgs{Name: &n.Name, Operands: ops} }
		if err := entry(field, false, "properties", n.Name); err != nil {
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

		match := func(ops []expr.Node) spelled { return basic.MatchArgs{Name: &n.Name, Operands: ops} }
		if err := entry(match, false, "patternProperties", n.Name); err != nil {
			return nil, err
		}
	}

	if rest {
		restOf := func(ops []expr.Node) spelled { return basic.RestArgs{Operands: ops} }
		if err := entry(restOf, true, "additionalProperties"); err != nil {
			return nil, err
		}

		checks = checks || s.AdditionalProperties.Bool.IsNone()
	}

	if len(entries) == 0 || !checks && !annotated {
		return nil, nil
	}

	return appendRule(nil, basic.PropsArgs{Entries: entries}), nil
}

// itemsRules returns prefix for the prefixItems of s, with the items of s
// as its rest, or items for the items of s alone, as keep keeps them.
func itemsRules(
	s *jsonschema.Schema,
	sub func(keys ...string) ([]expr.Arg, error),
	annotated bool,
	keep func(kw string) bool,
) ([]expr.Arg, error) {
	var rest []expr.Arg

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

		return appendRule(nil, basic.ItemsArgs{Operands: values(rest)}), nil
	}

	var ops []expr.Node

	for i := range s.PrefixItems {
		args, err := sub("prefixItems", strconv.Itoa(i))
		if err != nil {
			return nil, err
		}

		ops = append(ops, single(args).Value)
	}

	if s.Items != nil && keep("items") {
		ops = append(ops, node(basic.RestArgs{Operands: values(rest)}))
	}

	return appendRule(nil, basic.PrefixArgs{Operands: ops}), nil
}

// containsRules returns contains for the contains of s, counted by its
// minContains, 1 by default, and its maxContains.
func containsRules(
	s *jsonschema.Schema,
	sub func(keys ...string) ([]expr.Arg, error),
	keep func(kw string) bool,
) ([]expr.Arg, error) {
	if s.Contains == nil || !keep("contains") {
		return nil, nil
	}

	ops, err := sub("contains")
	if err != nil {
		return nil, err
	}

	minimum := jsontext.Value("1")
	if s.MinContains != nil {
		minimum = s.MinContains
	}

	counts, _ := lengths(minimum, s.MaxContains)

	return appendRule(
		nil,
		basic.ContainsArgs{Operands: values(ops), Ge: counts.Ge, Le: counts.Le},
	), nil
}

// keysRules returns keys for the propertyNames of s.
func keysRules(
	s *jsonschema.Schema,
	sub func(keys ...string) ([]expr.Arg, error),
	keep func(kw string) bool,
) ([]expr.Arg, error) {
	if s.PropertyNames == nil || !keep("propertyNames") {
		return nil, nil
	}

	ops, err := sub("propertyNames")
	if err != nil || len(ops) == 0 {
		return nil, err
	}

	return appendRule(nil, basic.KeysArgs{Operands: values(ops)}), nil
}

// dependentRules returns, for each key of the dependentSchemas of s,
// <schema> if required(<key>) else always.
func dependentRules(
	s *jsonschema.Schema,
	same func(keys ...string) (expr.Arg, error),
	keep func(kw string) bool,
) ([]expr.Arg, error) {
	if !keep("dependentSchemas") {
		return nil, nil
	}

	var args []expr.Arg

	for _, n := range s.DependentSchemas {
		then, err := same("dependentSchemas", n.Name)
		if err != nil {
			return nil, err
		}

		required := node(basic.RequiredArgs{Names: []string{n.Name}})
		args = append(args, expr.Arg{Value: basic.Cond(required, then.Value, nil)})
	}

	return args, nil
}

// listRules returns and, or and oneof for the allOf, anyOf and oneOf of
// s, each member one operand.
func listRules(
	s *jsonschema.Schema,
	same func(keys ...string) (expr.Arg, error),
	keep func(kw string) bool,
) ([]expr.Arg, error) {
	var args []expr.Arg

	for _, kw := range []string{"allOf", "anyOf", "oneOf"} {
		members := listOf(s, kw)
		if members == nil || !keep(kw) {
			continue
		}

		ops := make([]expr.Arg, 0, len(members))

		for i := range members {
			op, err := same(kw, strconv.Itoa(i))
			if err != nil {
				return nil, err
			}

			ops = append(ops, op)
		}

		switch kw {
		case "allOf":
			args = append(args, single(ops))
		case "anyOf":
			args = append(args, expr.Arg{Value: basic.Any(values(ops)...)})
		default:
			args = appendRule(args, basic.OneOfArgs{Operands: values(ops)})
		}
	}

	return args, nil
}

// notRules returns not for the not of s.
func notRules(
	s *jsonschema.Schema,
	same func(keys ...string) (expr.Arg, error),
	keep func(kw string) bool,
) ([]expr.Arg, error) {
	if s.Not == nil || !keep("not") {
		return nil, nil
	}

	op, err := same("not")
	if err != nil {
		return nil, err
	}

	return []expr.Arg{{Value: basic.Negate(op.Value)}}, nil
}

// ifRules returns if for the if, then and else of s. An if without then
// and else checks nothing, but it evaluates when it holds, which annotated
// needs.
func ifRules(
	s *jsonschema.Schema,
	same func(keys ...string) (expr.Arg, error),
	annotated bool,
	keep func(kw string) bool,
) ([]expr.Arg, error) {
	if s.If == nil || !keep("if") || s.Then == nil && s.Else == nil && !annotated {
		return nil, nil
	}

	ops := make([]expr.Node, 0, 3)

	for _, kw := range []string{"if", "then", "else"} {
		present := map[string]bool{"if": true, "then": s.Then != nil, "else": s.Else != nil}[kw]

		var op expr.Arg

		if present {
			var err error

			op, err = same(kw)
			if err != nil {
				return nil, err
			}
		}

		ops = append(ops, op.Value)
	}

	return []expr.Arg{{Value: basic.Cond(ops[0], ops[1], ops[2])}}, nil
}

// refRules returns the args the keyword kw, $ref or $dynamicRef, of the
// schema at ptr applies: ref or decode naming the type of its target, else
// the args of the target written out. A $dynamicRef resolves in scope, as
// dynamicTarget tells. ref reports what its type evaluated, and decode
// does not, so a target decode names is written out when annotated asks
// for what it evaluates, off the cycle of stack. A target is written out
// too when the dynamic scope would resolve the $dynamicRef under the
// target otherwise than the type of the target does, as scopeKeeps tells.
func refRules(
	doc *jsonschema.Schema,
	opts Options,
	ptr, kw string,
	annotated bool,
	keep func(kw string) bool,
	scope, stack []string,
) ([]expr.Arg, error) {
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

	if name := namedBy(doc, target); name != "" && keeps &&
		(onCycle || !annotated || name == (basic.Ref{}).Name()) {
		t, err := typeName(doc, opts, target)
		if err != nil {
			return nil, err
		}

		var named spelled = basic.RefArgs{Type: t}
		if name == (basic.Decode{}).Name() {
			named = basic.DecodeArgs{Type: t}
		}

		return appendRule(nil, named), nil
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
		return (basic.Ref{}).Name()
	}

	s, err := at(doc, ptr)
	if err != nil || ptr != "" && union(s) {
		return ""
	}

	return (basic.Decode{}).Name()
}

// single returns args as one arg, as basic.All joins them.
func single(args []expr.Arg) expr.Arg { return expr.Arg{Value: basic.All(values(args)...)} }

// spelled is the args of a validator, which Call spells as its call.
type spelled interface{ Call() (expr.Call, error) }

// appendRule appends to args the call a spells. It appends nothing when a
// holds a number that no literal spells, as 1e100000. check reports such a
// number, so no directive holding it is written, but checking one schema
// builds the args of others, as allChecked does.
func appendRule(args []expr.Arg, a spelled) []expr.Arg {
	c, err := a.Call()
	if err != nil {
		return args
	}

	return append(args, expr.Arg{Value: c})
}

// node returns the call a spells, for args of names and rules only. A
// name decoded from JSON is valid UTF-8, so its spelling never fails.
func node(a spelled) expr.Node {
	c, err := a.Call()
	if err != nil {
		panic(fmt.Errorf("jsonschema: %w", err))
	}

	return c
}

// values returns the values of args.
func values(args []expr.Arg) []expr.Node {
	return opiter.WrapSeq(slices.Values(args)).
		Map(func(a expr.Arg) expr.Node { return a.Value }).
		CollectSlice()
}
