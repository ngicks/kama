package jsonschema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

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
		steps = append(steps, lang.Line(docComment(
			wrappers[i]+" holds the "+m+" member of "+name+".",
		)))

		if isNullMember(doc, ptr, i) {
			steps = append(steps, lang.Stmt("type %s struct{}", wrappers[i]))
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
		)
	}

	var cases []genhelper.Case[genCtx]

	// A union of null members only reads no field of the member it holds.
	tag := lang.Expr("v.Kind.(type)")

	for i, m := range names {
		body := lang.Stmt("return %s(enc, k.%s)", q(jsonPath, "MarshalEncode"), m)
		if isNullMember(doc, ptr, i) {
			body = lang.Stmt("return enc.WriteToken(%s)", q(jsontextPath, "Null"))
		}

		if !isNullMember(doc, ptr, i) {
			tag = lang.Expr("k := v.Kind.(type)")
		}

		cases = append(cases, lang.Case(lang.Expr("%s", wrappers[i]), body))
	}

	steps = append(steps, lang.DocFunc(
		docComment("MarshalJSONTo encodes the member v holds. A nil Kind is an error."),
		lang.Expr("func (v %s) MarshalJSONTo(enc *%s) error", name, q(jsontextPath, "Encoder")),
		lang.Switch(tag, cases...),
		lang.Stmt(
			"return %s(%s, %s)",
			q(
				validPath,
				"New",
			),
			q(runtimePath, "ErrNilKind"),
			strconv.Quote(name+" holds no member"),
		),
	))

	return append(steps, unionUnmarshal(doc, opts, ptr, name, names))
}

// unionUnmarshal returns UnmarshalJSONFrom of the union at ptr: every
// member the value may match decodes it, and OneOf or AnyOf picks the
// result.
func unionUnmarshal(
	doc *jsonschema.Schema,
	opts Options,
	ptr, name string,
	names []string,
) genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return lang.Fail(err)
	}

	oneOf := unionKeyword(s) == "oneOf"

	body := []genStep{
		lang.Stmt("raw, err := %s(dec)", q(runtimePath, "ReadValue")),
		lang.If(lang.Expr("err != nil"), lang.Stmt("return err"), nil),
		lang.Stmt("var found []%sKind", name),
	}

	if oneOf {
		body = append(body, lang.Stmt("var names []string"))
	}

	body = append(body, lang.Stmt("var errs []error"))

	for i, m := range names {
		found := []genStep{lang.Stmt("found = append(found, %s{%s: m})", name+m, m)}
		if isNullMember(doc, ptr, i) {
			found = []genStep{lang.Stmt("found = append(found, %s{})", name+m)}
		}

		if oneOf {
			found = append(found, lang.Stmt("names = append(names, %s)", strconv.Quote(m)))
		}

		then := lang.Steps(found...)
		if !isNullMember(doc, ptr, i) {
			then = lang.Steps(
				lang.Stmt("var m %s", unionMemberType(doc, opts, ptr, i)),
				lang.If(
					lang.Expr("err := %s(raw, &m); err != nil", q(runtimePath, "Unmarshal")),
					lang.Stmt(
						"errs = append(errs, %s(%s, err))",
						q(runtimePath, "MemberError"), strconv.Quote(m),
					),
					lang.Steps(found...),
				),
			)
		}

		body = append(body, lang.If(memberCondition(doc, ptr, i), then, nil))
	}

	pick := lang.Stmt("kind, err := %s(found, errs)", q(runtimePath, "AnyOf"))
	text := "UnmarshalJSONFrom decodes v from the first member the value matches."

	if oneOf {
		pick = lang.Stmt("kind, err := %s(found, names, errs)", q(runtimePath, "OneOf"))
		text = "UnmarshalJSONFrom decodes v from the only member the value matches."
	}

	body = append(body,
		pick,
		lang.If(lang.Expr("err != nil"), lang.Stmt("return err"), nil),
		lang.Stmt("*v = %s{Kind: kind}", name),
		lang.Stmt("return nil"),
	)

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

// memberCondition returns the condition of the member i of the union at
// ptr possibly matching raw: the kinds it admits and, for an object, the
// members whose value it fixes, the members it requires and, when it
// rejects other members, the members it declares.
func memberCondition(doc *jsonschema.Schema, ptr string, i int) genExpr {
	k, err := memberKinds(doc, ptr, i)
	if err != nil {
		return lang.FailGen(err)
	}

	conds := []genExpr{lang.Expr(
		"%s(raw, %s)", q(runtimePath, "IsKind"), joinComma(quoteAll(k)),
	)}

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
				"%s(raw, %s, %s)",
				q(runtimePath, "MemberEquals"), strconv.Quote(name), strconv.Quote(string(v)),
			))
		}
	}

	if len(rs.Required) > 0 {
		conds = append(conds, lang.Expr(
			"%s(raw, %s)", q(runtimePath, "HasMembers"), joinComma(quoteAll(rs.Required)),
		))
	}

	if closed(rs) {
		props := opiter.WrapSeq(slices.Values(rs.Properties)).
			Map(func(n jsonschema.NamedSchema) string { return n.Name }).
			CollectSlice()

		conds = append(conds, lang.Expr(
			"%s(raw, %s)", q(runtimePath, "OnlyMembers"), joinComma(quoteAll(props)),
		))
	}

	return lang.Join(" && ", conds...)
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

func quoteAll(names []string) []string {
	return opiter.WrapSeq(slices.Values(names)).
		Map(strconv.Quote).
		CollectSlice()
}

func joinComma(names []string) string { return strings.Join(names, ", ") }
