package jsonschema

import (
	"encoding/json/jsontext"
	"iter"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext/basic"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// unionSteps returns the declarations of the union at ptr: the holder, the
// interface its members implement, a wrapper per member, then
// MarshalJSONTo and UnmarshalJSONFrom of the holder.
func unionSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	names, err := opiter.TryCollect(
		opiter.WrapSeq(opiter.Range(0, members(s))).
			Disassemble(func(i int) (string, error) { return memberName(doc, opts, ptr, i) }),
	)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	wrappers := opiter.WrapSeq(slices.Values(names)).
		Map(func(m string) string { return name + m }).
		CollectSlice()

	kind := name + "Kind"
	oneOf := unionKeyword(s) == "oneOf"

	lead := name + " is the first of " + joinComma(names) + " that the value matches."
	if oneOf {
		lead = name + " is exactly one of " + joinComma(names) + "."
	}

	steps := []genStep{
		lang.Line(
			docComment(typeDoc(doc, ptr, lead+" It is generated from the schema at #"+ptr+".")),
		),
		lang.Stmt("type %s struct {", name),
		lang.Stmt("Kind %s", kind),
		lang.Stmt("}"),
		lang.Line(docComment(kind + " is implemented by " + listText(wrappers) + " only.")),
		lang.Stmt("type %s interface {", kind),
		lang.Stmt("is%s()", kind),
		lang.Stmt("}"),
	}

	for i, m := range names {
		// The decoder of the holder yields a wrapper only after it passed
		// ValidateShallow, so a decoded wrapper is valid.
		steps = append(steps, lang.Line(validatedDoc(
			wrappers[i]+" holds the "+m+" member of "+name+".",
			decodedArgs(opts, unionArgs(doc, opts, ptr)),
		)))

		encode := lang.Stmt("return %s(enc, v.%s)", q(jsonPath, "MarshalEncode"), m)

		if isNullMember(doc, ptr, i) {
			steps = append(steps, lang.Stmt("type %s struct{}", wrappers[i]))
			encode = lang.Stmt("return enc.WriteToken(%s)", q(jsontextPath, "Null"))
		} else {
			steps = append(steps,
				lang.Stmt("type %s struct {", wrappers[i]),
				lang.Stmt("%s %s", m, unionMemberType(doc, opts, ptr, i)),
				lang.Stmt("}"),
			)
		}

		steps = append(steps,
			lang.Line(docComment("is"+kind+" marks "+wrappers[i]+" as a "+kind+".")),
			lang.Stmt("func (%s) is%s() {}", wrappers[i], kind),
			lang.DocFunc(
				docComment("MarshalJSONTo encodes the member v holds, which the validators of "+
					"the value encoded as JSON check."),
				lang.Expr(
					"func (v %s) MarshalJSONTo(enc *%s) error",
					wrappers[i],
					q(jsontextPath, "Encoder"),
				),
				encode,
			),
		)
	}

	unknown := name + "Unknown"

	steps = append(steps,
		lang.Line(docComment(unknown+" holds a value of "+name+" that no member decodes"+
			map[bool]string{true: ", or that two members decode"}[oneOf]+
			". UnmarshalJSONFrom yields it instead of failing, and Validate reports it with "+
			"the reasons decoding left in it. MarshalJSONTo writes Value as it is.")),
		lang.Stmt("type %s struct {", unknown),
		lang.Stmt("Value %s", q(jsontextPath, "Value")),
		lang.Stmt("err error"),
		lang.Stmt("}"),
		lang.Line(docComment("is"+kind+" marks "+unknown+" as a "+kind+".")),
		lang.Stmt("func (%s) is%s() {}", unknown, kind),
	)

	unknownErr := lang.Stmt(
		"return kamaJsonschemaUnknownMember(%s, k.err)", noMatchKind(s),
	)
	marshalCases := []genhelper.Case[genCtx]{
		lang.Case(
			lang.Expr("%s", strings.Join(wrappers, ", ")),
			lang.Stmt("return %s(enc, k)", q(jsonPath, "MarshalEncode")),
		),
		lang.Case(lang.Expr("%s", unknown), lang.Stmt("return enc.WriteValue(k.Value)")),
	}
	validateCases := append(
		opiter.WrapSeq(slices.Values(wrappers)).
			Map(func(w string) genhelper.Case[genCtx] {
				return lang.Case(
					lang.Expr("%s", w),
					lang.Stmt("return k.Validate(stopOnFirstError)"),
				)
			}).
			CollectSlice(),
		lang.Case(lang.Expr("%s", unknown), unknownErr),
	)

	nilKind := lang.Stmt(
		"return %s(%s, %s)",
		q(validPath, "New"),
		q(validPath, "ErrNil"),
		strconv.Quote(name+" holds no member"),
	)

	steps = append(steps,
		lang.DocFunc(
			docComment("MarshalJSONTo encodes the member v holds. A nil Kind is an error."),
			lang.Expr(
				"func (v %s) MarshalJSONTo(enc *%s) error", name, q(jsontextPath, "Encoder"),
			),
			lang.Switch(lang.Expr("k := v.Kind.(type)"), marshalCases...),
			nilKind,
		),
		lang.DocFunc(
			docComment("Validate reports why the member v holds is invalid, through the Validate "+
				"of its wrapper, which the validator generator writes. "+unknown+
				" and a nil Kind are errors."),
			lang.Expr("func (v %s) Validate(stopOnFirstError bool) error", name),
			lang.Switch(lang.Expr("k := v.Kind.(type)"), validateCases...),
			nilKind,
		),
	)

	for i, m := range names {
		steps = append(steps, probeSteps(doc, opts, ptr, i, wrappers[i], m)...)
	}

	if opts.Validate {
		steps = append(steps, lang.DocFunc(
			docComment("ValidateShallow is Validate for a decoded "+name+". The decoder yields "+
				"a member only after the member passed ValidateShallow of its wrapper, so "+
				"ValidateShallow reports "+unknown+" and a nil Kind only."),
			lang.Expr("func (v %s) ValidateShallow(stopOnFirstError bool) error", name),
			lang.Switch(
				lang.Expr("k := v.Kind.(type)"),
				lang.Case(lang.Expr("%s", strings.Join(wrappers, ", ")), lang.Stmt("return nil")),
				lang.Case(lang.Expr("%s", unknown), unknownErr),
			),
			nilKind,
		))
	}

	return append(steps, unionUnmarshal(doc, opts, ptr))
}

// unionUnmarshal returns UnmarshalJSONFrom of the union at ptr: the members
// the value may match decode it in schema order, and with Options.Validate
// a member decoding it must pass the Validate of its wrapper too. An anyOf
// stops at the first that succeeds. A oneOf stops at the second, which
// makes the value unknown, as does a value no member decodes. Decoding
// every member would make a value nested in unions cost time exponential
// in its depth even when the first member accepts every level. A value no
// member accepts still costs that much, since every member of every level
// fails.
func unionUnmarshal(doc *jsonschema.Schema, opts Options, ptr string) genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return lang.Fail(err)
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return lang.Fail(err)
	}

	oneOf := unionKeyword(s) == "oneOf"
	// raw is valid until dec reads again. The members decode it before that,
	// and the unknown member keeps a copy.
	setUnknown := func(reason genExpr) genStep {
		return lang.Stmt(
			"*v = %s{Kind: %sUnknown{Value: raw.Clone(), err: %s}}", name, name, reason,
		)
	}
	body := []genStep{
		lang.Stmt("raw, err := dec.ReadValue()"),
		lang.If(lang.Expr("err != nil"), lang.Stmt("return err"), nil),
	}

	if oneOf {
		body = append(body,
			lang.Stmt("var found %sKind", name),
			lang.Stmt("var name string"),
		)
	}

	body = append(body, lang.Stmt("var errs []error"))

	for i := range members(s) {
		m, err := memberName(doc, opts, ptr, i)
		if err != nil {
			return lang.Fail(err)
		}

		kind := lang.Expr("%s{%s: m}", name+m, m)
		if isNullMember(doc, ptr, i) {
			kind = lang.Expr("%s{}", name+m)
		}

		found := lang.Steps(
			lang.Stmt("*v = %s{Kind: %s}", name, kind),
			lang.Stmt("return nil"),
		)
		if oneOf {
			found = lang.Steps(
				lang.If(
					lang.Expr("found != nil"),
					lang.Steps(
						setUnknown(lang.Expr(
							"%s(%s, %s, name)",
							q(validPath, "New"), q(validPath, "ErrOneOf"),
							strconv.Quote("value matches %s, "+m),
						)),
						lang.Stmt("return nil"),
					),
					nil,
				),
				lang.Stmt("found, name = %s, %s", kind, strconv.Quote(m)),
			)
		}

		var then genStep = found

		if !isNullMember(doc, ptr, i) {
			failed := lang.Stmt(
				"errs = append(errs, %s(%s, err))",
				q("fmt", "Errorf"), strconv.Quote("member "+m+": %w"),
			)

			var decoded genStep = found
			if opts.Validate {
				// A composite literal in the header of an if needs the
				// parentheses. Its address reaches ValidateShallow on the
				// pointer receiver, as the validator generator writes it for a
				// struct.
				decoded = lang.If(
					lang.Expr("err := (&%s).ValidateShallow(false); err != nil", kind),
					failed,
					found,
				)
			}

			then = converted(
				unionMemberConv(doc, opts, ptr, i),
				func(conv genExpr) genStep {
					return lang.Steps(
						lang.Stmt("var w %s", unionMemberWire(doc, opts, ptr, i)),
						lang.If(
							lang.Expr("err := kamaJsonschemaUnmarshal(dec, raw, &w); err != nil"),
							failed,
							lang.If(
								lang.Expr("m, err := %s(w); err != nil", conv),
								failed,
								decoded,
							),
						),
					)
				},
				func(genExpr) genStep {
					return lang.Steps(
						lang.Stmt("var m %s", unionMemberType(doc, opts, ptr, i)),
						lang.If(
							lang.Expr("err := kamaJsonschemaUnmarshal(dec, raw, &m); err != nil"),
							failed,
							decoded,
						),
					)
				},
			)

			if probeArgs(doc, opts, ptr, i) != nil {
				then = lang.If(
					lang.Expr("err := %s(raw).Validate(false); err != nil", probeName(name+m)),
					failed,
					then,
				)
			}
		}

		body = append(body, lang.If(memberCondition(doc, ptr, i), then, nil))
	}

	noMatch := setUnknown(lang.Expr("kamaJsonschemaNoMatch(%s, errs)", noMatchKind(s)))
	if opts.Validate {
		noMatch = lang.Steps(
			noMatch,
			lang.If(
				lang.Expr("err := kamaJsonschemaStubbed(errs); err != nil"),
				lang.Stmt("return err"),
				nil,
			),
		)
	}

	text := "UnmarshalJSONFrom decodes v from the first member the value matches."

	if oneOf {
		text = "UnmarshalJSONFrom decodes v from the only member the value matches."

		body = append(body,
			lang.If(
				lang.Expr("found == nil"),
				lang.Steps(noMatch, lang.Stmt("return nil")),
				nil,
			),
			lang.Stmt("*v = %s{Kind: found}", name),
			lang.Stmt("return nil"),
		)
	} else {
		body = append(body, noMatch, lang.Stmt("return nil"))
	}

	if opts.Validate {
		text += " A member matches when it decodes the value and its wrapper passes " +
			"ValidateShallow."
	}

	both := ""
	if oneOf {
		both = ", or two members match,"
	}

	text += " A value no member matches" + both + " gives " + name +
		"Unknown, which Validate reports."

	if opts.Validate {
		text += " A member failing on a stub of the validator generator gives the error of " +
			"the stub, since the stub tells nothing of the value."
	}

	return lang.DocFunc(
		docComment(text),
		lang.Expr(
			"func (v *%s) UnmarshalJSONFrom(dec *%s) error",
			name,
			q(jsontextPath, "Decoder"),
		),
		body...,
	)
}

// probeSteps returns the declaration of the probe of the member i of the
// union at ptr, which the wrapper named wrapper holds under the field m, or
// none when probeArgs gives no args.
func probeSteps(
	doc *jsonschema.Schema,
	opts Options,
	ptr string,
	i int,
	wrapper, m string,
) []genStep {
	args := probeArgs(doc, opts, ptr, i)
	if args == nil {
		return nil
	}

	probe := probeName(wrapper)

	return []genStep{
		lang.Line(validatedDoc(
			probe+" is the raw value the union at #"+ptr+" tests before decoding it as its "+
				m+" member. Its validators are the keywords of the member that need no decoding "+
				"of a nested value, so a value they reject costs no decoding.",
			args,
		)),
		lang.Stmt("type %s %s", probe, q(jsontextPath, "Value")),
		lang.DocFunc(
			docComment("RawJson returns v as raw JSON text, which tells the validators to check "+
				"v as JSON Schema does."),
			lang.Expr("func (v %s) RawJson() %s", probe, q(jsontextPath, "Value")),
			lang.Stmt("return %s(v)", q(jsontextPath, "Value")),
		),
	}
}

// probeArgs returns the args of //kama:validate on the probe of the member
// i of the union at ptr, or nil when the member gets none. Deciding between
// members by decoding each one costs a decoding of the whole value per
// member, and nested unions multiply that per level. A probe checks on the
// raw value, before decoding, the keywords of the member that need no
// decoding of a nested value: those of the member itself that hold no
// subschema, and the properties whose schemas reach no reference. Each of
// them is one conjunct of the member, so the probe rejects only values the
// member rejects. A member reaching no reference decodes no nested type,
// so it gets no probe.
func probeArgs(doc *jsonschema.Schema, opts Options, ptr string, i int) []expr.Arg {
	s, err := at(doc, ptr)
	if err != nil || !opts.Validate || unionKeyword(s) == "" {
		return nil
	}

	mp := child(ptr, unionKeyword(s), strconv.Itoa(i))

	r, rs, err := resolved(doc, mp)
	if err != nil || rs.Bool.IsSome() || refFree(doc, mp) {
		return nil
	}

	args := leafRules(opts, rs, func(kw string) bool { return kw != "type" && kw != "required" })

	var fields []expr.Node

	for _, n := range rs.Properties {
		pp := child(r, "properties", n.Name)
		if !refFree(doc, pp) {
			continue
		}

		ops, err := rulesOf(doc, opts, pp, false, everyKeyword, lexicalScope(doc, pp), nil)
		if err != nil || len(ops) == 0 {
			continue
		}

		fields = append(fields, node(basic.FieldArgs{Name: &n.Name, Operands: values(ops)}))
	}

	if len(fields) > 0 {
		args = appendRule(args, basic.PropsArgs{Entries: fields})
	}

	if len(args) == 0 {
		return nil
	}

	return args
}

// refFree reports whether no schema at or under ptr, those under $defs
// included, holds a $ref or a $dynamicRef.
func refFree(doc *jsonschema.Schema, ptr string) bool {
	s, err := at(doc, ptr)
	if err != nil {
		return false
	}

	under := func(yield func(string, *jsonschema.Schema) bool) { visit(ptr, s, yield) }

	return !opiter.WrapSeq2(under).
		Any(func(_ string, x *jsonschema.Schema) bool {
			return x != nil && (x.Ref.IsSome() || x.DynamicRef.IsSome())
		})
}

// memberCondition returns the condition of the member i of the union at
// ptr possibly matching raw: the kinds it admits and, for an object, the
// members whose value it fixes, the members it requires and, when it
// rejects other members, the members it declares.
func memberCondition(doc *jsonschema.Schema, ptr string, i int) genExpr {
	k, err := memberKinds(doc, ptr, i)
	if err != nil {
		return lang.FailGen(err)
	}

	conds := []genExpr{kindCondition(k)}

	s, _ := at(doc, ptr)
	if memberKeyword(s) == "" || !slices.Equal(k, []string{"object"}) {
		return lang.Join(" && ", conds...)
	}

	r, rs, err := resolved(doc, child(ptr, unionKeyword(s), strconv.Itoa(i)))
	if err != nil {
		return lang.FailGen(err)
	}

	if !structKind(rs) {
		return lang.Join(" && ", conds...)
	}

	for _, name := range rs.Required {
		if v, ok := fixedValue(doc, child(r, "properties", name)); ok {
			conds = append(conds, lang.Expr(
				"kamaJsonschemaMemberEquals(raw, %s, %s)",
				strconv.Quote(name), strconv.Quote(string(v)),
			))
		}
	}

	if len(rs.Required) > 0 {
		conds = append(conds, lang.Expr(
			"kamaJsonschemaHasMembers(raw, %s)", joinComma(quoteAll(rs.Required)),
		))
	}

	if closed(rs) && !bridged(doc, r, rs, "additionalProperties") {
		props := opiter.WrapSeq(slices.Values(rs.Properties)).
			Map(func(n jsonschema.NamedSchema) string { return n.Name }).
			CollectSlice()

		conds = append(conds, lang.Expr(
			"kamaJsonschemaOnlyMembers(raw, %s)", joinComma(quoteAll(props)),
		))
	}

	return lang.Join(" && ", conds...)
}

// kindCondition returns the condition of raw being of one of kinds, the
// JSON Schema types. An integer is any number, since decoding into the Go
// type rejects a fraction.
func kindCondition(kinds []string) genExpr {
	consts := opiter.WrapSeq(slices.Values(kinds)).
		FlatMap(func(k string) iter.Seq[string] {
			switch k {
			case "null":
				return opiter.Once("KindNull")
			case "boolean":
				return slices.Values([]string{"KindTrue", "KindFalse"})
			case "string":
				return opiter.Once("KindString")
			case "integer", "number":
				return opiter.Once("KindNumber")
			case "object":
				return opiter.Once("KindBeginObject")
			case "array":
				return opiter.Once("KindBeginArray")
			}

			return opiter.Once("KindInvalid")
		}).
		UniqueFunc(func(c string) string { return c }).
		Map(func(c string) genExpr { return lang.Expr("raw.Kind() == %s", q(jsontextPath, c)) }).
		CollectSlice()

	switch len(consts) {
	case 0:
		return lang.Expr("false")
	case 1:
		return consts[0]
	}

	return lang.Expr("(%s)", lang.Join(" || ", consts...))
}

// noMatchKind spells the sentinel error of a value no member of the union
// s matches: validator.ErrOneOf for a oneOf, as of two members matching,
// and validator.ErrOr otherwise, as the validators or and oneof report.
func noMatchKind(s *jsonschema.Schema) genExpr {
	if unionKeyword(s) == "oneOf" {
		return q(validPath, "ErrOneOf")
	}

	return q(validPath, "ErrOr")
}

// fixedValue returns the only value the schema at ptr admits: its const or
// the value of an enum of one.
func fixedValue(doc *jsonschema.Schema, ptr string) (jsontext.Value, bool) {
	_, s, err := resolved(doc, ptr)
	switch {
	case err != nil:
		return nil, false
	case s.Const != nil:
		return s.Const, true
	case len(s.Enum) == 1:
		return s.Enum[0], true
	}

	return nil, false
}

// memberKinds returns the kinds the member i of the union at ptr admits.
func memberKinds(doc *jsonschema.Schema, ptr string, i int) ([]string, error) {
	s, err := at(doc, ptr)
	if err != nil {
		return nil, err
	}

	if kw := unionKeyword(s); kw != "" {
		return kinds(doc, child(ptr, kw, strconv.Itoa(i)))
	}

	return []string{s.Type.Names[i]}, nil
}

// isNullMember reports whether the member i of the union at ptr admits
// null only, which its wrapper stands for without a field.
func isNullMember(doc *jsonschema.Schema, ptr string, i int) bool {
	s, err := at(doc, ptr)
	if err != nil {
		return false
	}

	if kw := unionKeyword(s); kw != "" {
		return nullMember(doc, child(ptr, kw, strconv.Itoa(i)))
	}

	return s.Type.Names[i] == "null"
}

// unionMemberType spells the Go type of the member i of the union at ptr.
func unionMemberType(doc *jsonschema.Schema, opts Options, ptr string, i int) genExpr {
	s, err := at(doc, ptr)
	if err != nil {
		return lang.FailGen(err)
	}

	if kw := unionKeyword(s); kw != "" {
		return elemType(doc, opts, child(ptr, kw, strconv.Itoa(i)))
	}

	switch s.Type.Names[i] {
	case "string":
		return lang.Expr("string")
	case "integer":
		return lang.Expr("int64")
	case "number":
		return lang.Expr("float64")
	case "boolean":
		return lang.Expr("bool")
	}

	return lang.FailGen(
		unsupported(child(ptr, "type"), "kind %q in a union of kinds", s.Type.Names[i]),
	)
}

// unionMemberWire is unionMemberType for the wire type the member i of the
// union at ptr decodes into.
func unionMemberWire(doc *jsonschema.Schema, opts Options, ptr string, i int) genExpr {
	s, err := at(doc, ptr)
	if err != nil {
		return lang.FailGen(err)
	}

	if kw := unionKeyword(s); kw != "" {
		return elemWireType(doc, opts, child(ptr, kw, strconv.Itoa(i)), slotMember)
	}

	if s.Type.Names[i] == "integer" {
		return q(wirePath, "Int64")
	}

	return unionMemberType(doc, opts, ptr, i)
}

// unionMemberConv is wireConv for unionMemberWire.
func unionMemberConv(doc *jsonschema.Schema, opts Options, ptr string, i int) genExpr {
	s, err := at(doc, ptr)
	if err != nil {
		return lang.FailGen(err)
	}

	if kw := unionKeyword(s); kw != "" {
		return elemWireConv(doc, opts, child(ptr, kw, strconv.Itoa(i)), slotMember)
	}

	if s.Type.Names[i] == "integer" {
		return lang.Expr("%s.Value", q(wirePath, "Int64"))
	}

	return lang.Expr("")
}

func quoteAll(names []string) []string {
	return opiter.WrapSeq(slices.Values(names)).
		Map(strconv.Quote).
		CollectSlice()
}

func joinComma(names []string) string { return strings.Join(names, ", ") }
