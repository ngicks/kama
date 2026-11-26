package jsonschema

import (
	"encoding/json/jsontext"
	"regexp"
	"slices"
	"strconv"

	"github.com/ngicks/opiter"

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

		return []expr.Arg{call("never")}, nil
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

		name := "unevalprops"
		if kw == "unevaluatedItems" {
			name = "unevalitems"
		}

		args = []expr.Arg{call(name, append([]expr.Arg{single(args)}, rest...)...)}
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
		switch {
		case names[i] == "and":
			joined = append(joined, pos(a.Value.(expr.And).Operands...)...)
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
// call, or the word of an operator.
func argName(a expr.Arg) string {
	switch x := a.Value.(type) {
	case expr.Call:
		return x.Name
	case expr.And:
		return "and"
	case expr.Or:
		return "or"
	case expr.Not:
		return "not"
	case expr.Cond:
		return "if"
	}

	return a.String()
}

// leafRules returns the args of the keywords of s holding no subschema that
// keep keeps.
func leafRules(opts Options, s *jsonschema.Schema, keep func(kw string) bool) []expr.Arg {
	var args []expr.Arg

	add := func(name string, params []expr.Arg, keywords ...string) {
		if len(params) > 0 && slices.ContainsFunc(keywords, keep) {
			args = append(args, call(name, params...))
		}
	}
	lengths := func(min, max jsontext.Value) []expr.Arg {
		return comparisons(map[string]jsontext.Value{">=": min, "<=": max}, integerText)
	}

	if !s.Type.IsZero() {
		add("kind", strs(slices.Compact(slices.Clone(s.Type.Names))), "type")
	}

	// A const next to an enum applies too, so each gives a const of its own.
	if s.Const != nil {
		add("const", rawValues([]jsontext.Value{s.Const}), "const")
	}

	if s.Enum != nil && keep("enum") {
		if len(s.Enum) == 0 {
			args = append(args, call("never"))
		} else {
			add("const", rawValues(s.Enum), "enum")
		}
	}

	add("cmp", comparisons(map[string]jsontext.Value{
		">=": s.Minimum, "<=": s.Maximum, ">": s.ExclusiveMinimum, "<": s.ExclusiveMaximum,
	}, numberText), "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum")

	if s.MultipleOf != nil {
		add("multipleof", pos(expr.Number{Text: numberText(s.MultipleOf)}), "multipleOf")
	}

	add("runes", lengths(s.MinLength, s.MaxLength), "minLength", "maxLength")

	if f := assertedFormat(opts, s); f != "" {
		add("format", strs([]string{f}), "format")
	}

	if s.Pattern.IsSome() {
		if _, err := regexp.Compile(s.Pattern.Value()); err == nil {
			add("regexp", strs([]string{s.Pattern.Value()}), "pattern")
		}
	}

	add("length", lengths(s.MinItems, s.MaxItems), "minItems", "maxItems")

	if s.UniqueItems.Value() && keep("uniqueItems") {
		args = append(args, call("unique"))
	}

	add("members", lengths(s.MinProperties, s.MaxProperties), "minProperties", "maxProperties")

	if len(s.Required) > 0 {
		add("required", strs(s.Required), "required")
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
		entries []expr.Arg
		checks  bool
	)

	rest := s.AdditionalProperties != nil && keep("additionalProperties")

	// entry adds the entry kind of the subschema at keys, which names the
	// member name or the pattern name, but for rest.
	entry := func(kind, name string, keys ...string) error {
		var ops []expr.Arg

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
			ops = append(strs([]string{name}), ops...)
		}

		entries = append(entries, call(kind, ops...))

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

	return []expr.Arg{call("props", entries...)}, nil
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

		return []expr.Arg{call("items", rest...)}, nil
	}

	var ops []expr.Arg

	for i := range s.PrefixItems {
		args, err := sub("prefixItems", strconv.Itoa(i))
		if err != nil {
			return nil, err
		}

		ops = append(ops, single(args))
	}

	if s.Items != nil && keep("items") {
		ops = append(ops, call("rest", rest...))
	}

	return []expr.Arg{call("prefix", ops...)}, nil
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

	counts := comparisons(
		map[string]jsontext.Value{">=": minimum, "<=": s.MaxContains}, integerText,
	)

	return []expr.Arg{call("contains", append(ops, counts...)...)}, nil
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

	return []expr.Arg{call("keys", ops...)}, nil
}

// dependentRules returns, for each key of the dependentSchemas of s,
// if(required=<key>, <schema>).
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

		args = append(args, cond(call("required", strs([]string{n.Name})...), then, always()))
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
			args = append(args, anyOf(ops))
		default:
			args = append(args, call("oneof", ops...))
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

	return []expr.Arg{{Value: expr.Not{Operand: op.Value}}}, nil
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

	ops := make([]expr.Arg, 0, 3)

	for _, kw := range []string{"if", "then", "else"} {
		present := map[string]bool{"if": true, "then": s.Then != nil, "else": s.Else != nil}[kw]

		switch {
		case present:
			op, err := same(kw)
			if err != nil {
				return nil, err
			}

			ops = append(ops, op)
		default:
			ops = append(ops, always())
		}
	}

	return []expr.Arg{cond(ops[0], ops[1], ops[2])}, nil
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
		(onCycle || !annotated || name == "ref") {
		t, err := typeName(doc, opts, target)
		if err != nil {
			return nil, err
		}

		return []expr.Arg{call(name, strs([]string{t})...)}, nil
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

// single returns args as one arg: always for none, the only arg, else
// the and of them.
func single(args []expr.Arg) expr.Arg {
	switch len(args) {
	case 0:
		return always()
	case 1:
		return args[0]
	}

	return expr.Arg{Value: expr.And{Operands: values(args)}}
}

// anyOf returns the or of args, never for none. One arg stands for itself,
// as an or of one operand holds when the operand does.
func anyOf(args []expr.Arg) expr.Arg {
	switch len(args) {
	case 0:
		return call("never")
	case 1:
		return args[0]
	}

	return expr.Arg{Value: expr.Or{Operands: values(args)}}
}

// cond returns the conditional then if c else otherwise.
func cond(c, then, otherwise expr.Arg) expr.Arg {
	return expr.Arg{Value: expr.Cond{If: c.Value, Then: then.Value, Else: otherwise.Value}}
}

// always returns the rule always holding.
func always() expr.Arg { return call("always") }

// call returns the call name(args...), the bare name without args.
func call(name string, args ...expr.Arg) expr.Arg {
	return expr.Arg{Value: expr.Call{Name: name, Args: args}}
}

// pos returns ns as positional args.
func pos(ns ...expr.Node) []expr.Arg {
	return opiter.WrapSeq(slices.Values(ns)).
		Map(func(n expr.Node) expr.Arg { return expr.Arg{Value: n} }).
		CollectSlice()
}

// strs returns names as positional string args.
func strs(names []string) []expr.Arg {
	return opiter.WrapSeq(slices.Values(names)).
		Map(func(n string) expr.Arg { return expr.Arg{Value: expr.String{Value: n}} }).
		CollectSlice()
}

// values returns the values of args.
func values(args []expr.Arg) []expr.Node {
	return opiter.WrapSeq(slices.Values(args)).
		Map(func(a expr.Arg) expr.Node { return a.Value }).
		CollectSlice()
}
