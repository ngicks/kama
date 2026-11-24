package jsonschema

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// objectSteps returns the declarations of the struct of the object at ptr,
// or of the product of its allOf: the struct, its MarshalJSONTo where the
// default encoding falls short, the wire struct and UnmarshalJSONFrom.
func objectSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	parts, err := productParts(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	lead := name + " is generated from the schema at #" + ptr + "."
	if product(doc, ptr, s) {
		lead = name + " is all of " + parts + ". It is generated from the schema at #" + ptr + "."
	}

	steps := []genStep{
		lang.Line(
			validatedDoc(typeDoc(doc, ptr, lead), decodedArgs(opts, declArgs(doc, opts, ptr))),
		),
		lang.Stmt("type %s struct {", name),
	}

	for i := range productOf(doc, ptr, s) {
		steps = append(steps, lang.Stmt(
			"%s %s `json:\"-\"`",
			memberField(doc, opts, ptr, i),
			goType(doc, opts, child(ptr, "allOf", strconv.Itoa(i))),
		))
	}

	for _, n := range s.Properties {
		pp := child(ptr, "properties", n.Name)

		var comment []genExpr

		if a := annotations(n.Schema); len(a) > 0 {
			comment = append(comment, lang.Expr("%s", commentLines(strings.Join(a, "\n\n"))))
		}

		args, err := fieldArgs(doc, opts, ptr, n.Name)
		if err != nil {
			return []genStep{lang.Fail(err)}
		}

		if len(args) > 0 {
			comment = append(comment, lang.Expr("%s", directiveLines(args)))
		}

		if comment != nil {
			steps = append(steps, lang.Line(lang.Comment(comment...)))
		}

		steps = append(steps, lang.Stmt(
			"%s %s `json:%s`",
			fieldName(pp, n.Name), fieldType(doc, opts, ptr, n.Name), tagFor(doc, ptr, n.Name),
		))
	}

	steps = append(steps,
		lang.Stmt(
			"%s map[string]%s `json:\",embed\"`",
			fallbackField(doc, opts, ptr),
			extraType(doc, opts, ptr),
		),
		lang.Stmt("}"),
	)
	steps = append(steps, marshalSteps(doc, opts, ptr)...)
	steps = append(steps, wireSteps(doc, opts, ptr)...)

	return append(steps, unmarshalSteps(doc, opts, ptr)...)
}

// extraType spells the Go type of the values of the members the struct or
// product at ptr declares no field for: the type of its additionalProperties
// schema, unless patternProperties takes some of them, else raw JSON text.
func extraType(doc *jsonschema.Schema, opts Options, ptr string) genExpr {
	s, err := at(doc, ptr)
	if err != nil {
		return lang.FailGen(err)
	}

	if extraMembers(s) && !bridged(doc, ptr, s, "additionalProperties") {
		return elemType(doc, opts, child(ptr, "additionalProperties"))
	}

	return q(jsontextPath, "Value")
}

// productParts names what the product at ptr is made of: its members
// separated by commas, then "and its own properties" when it declares
// any. It is empty for a struct.
func productParts(doc *jsonschema.Schema, opts Options, ptr string) (string, error) {
	s, err := at(doc, ptr)
	if err != nil {
		return "", err
	}

	names, err := opiter.TryCollect(
		opiter.WrapSeq(opiter.Range(0, len(productOf(doc, ptr, s)))).
			Disassemble(func(i int) (string, error) { return memberName(doc, opts, ptr, i) }),
	)
	if err != nil {
		return "", err
	}

	text := strings.Join(names, ", ")
	if product(doc, ptr, s) && s.Properties != nil {
		text += " and its own properties"
	}

	return text, nil
}

// marshalSteps returns MarshalJSONTo of the struct or product at ptr, or
// nil where the default encoding suffices. A product writes its members and
// its own fields into one object, and a required pointer on a cycle must
// not encode as null. The local type plain drops the method, so that
// encoding v from it does not call the method again.
func marshalSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	body := nilChecks(doc, ptr)
	if body == nil && !product(doc, ptr, s) {
		return nil
	}

	sig := lang.Expr("func (v %s) MarshalJSONTo(enc *%s) error", name, q(jsontextPath, "Encoder"))
	text := "MarshalJSONTo encodes v as one JSON object."

	if product(doc, ptr, s) {
		parts, err := productParts(doc, opts, ptr)
		if err != nil {
			return []genStep{lang.Fail(err)}
		}

		text = "MarshalJSONTo encodes v as one JSON object holding the members of " + parts +
			" and the members no property declares. The first of them to hold a member name " +
			"writes it."
	}

	if body != nil {
		text += " A nil pointer of a required member is an error."
	}

	body = append(body, lang.Stmt("type plain %s", name))
	value := lang.Expr("plain(v)")

	if !product(doc, ptr, s) {
		body = append(body, lang.Stmt("return %s(enc, %s)", q(jsonPath, "MarshalEncode"), value))

		return []genStep{lang.DocFunc(docComment(text), sig, body...)}
	}

	values := opiter.WrapSeq(opiter.Range(0, len(productOf(doc, ptr, s)))).
		Map(func(i int) genExpr { return lang.Expr("v.%s", memberField(doc, opts, ptr, i)) }).
		CollectSlice()

	body = append(body, lang.Stmt(
		"return %s(enc, %s)",
		q(runtimePath, "MarshalObjects"),
		lang.Join(", ", append(values, value)...),
	))

	return []genStep{lang.DocFunc(docComment(text), sig, body...)}
}

// nilChecks returns the statements of MarshalJSONTo of the struct or
// product at ptr that reject a nil pointer of a required member, or nil
// when no required member is a pointer.
func nilChecks(doc *jsonschema.Schema, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	var body []genStep

	for _, n := range s.Properties {
		if !required(doc, ptr, n.Name) || nullable(doc, child(ptr, "properties", n.Name)) ||
			!onCycle(doc, ptr, child(ptr, "properties", n.Name)) {
			continue
		}

		body = append(body, lang.If(
			lang.Expr("v.%s == nil", fieldName(child(ptr, "properties", n.Name), n.Name)),
			lang.Stmt(
				"return %s(%s(%s, \"required member is nil\"), %s)",
				q(validPath, "Field"), q(validPath, "New"), q(validPath, "ErrNil"),
				strconv.Quote(n.Name),
			),
			nil,
		))
	}

	return body
}

// wireSteps returns the struct the object at ptr decodes through: every
// property as an und.Und, or as raw text where its Go type holds null
// itself, then the members the object rejects or keeps.
func wireSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	steps := []genStep{
		lang.Line(docComment(wireName(name) + " keeps a missing member of " + name +
			" apart from null.")),
		lang.Stmt("type %s struct {", wireName(name)),
	}

	for _, n := range s.Properties {
		pp := child(ptr, "properties", n.Name)

		t := lang.Expr("%s[%s]", q(undPath, "Und"), goType(doc, opts, pp))
		if acceptsNull(doc, pp) {
			t = q(jsontextPath, "Value")
		}

		steps = append(steps, lang.Stmt(
			"%s %s `json:%s`", fieldName(pp, n.Name), t, strconv.Quote(n.Name+",omitzero"),
		))
	}

	return append(steps,
		lang.Stmt(
			"%s map[string]%s `json:\",embed\"`",
			fallbackField(doc, opts, ptr),
			extraType(doc, opts, ptr),
		),
		lang.Stmt("}"),
	)
}

// unmarshalSteps returns UnmarshalJSONFrom of the struct or product at ptr.
func unmarshalSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	text := "UnmarshalJSONFrom decodes v from one JSON object. "

	if product(doc, ptr, s) {
		parts, err := productParts(doc, opts, ptr)
		if err != nil {
			return []genStep{lang.Fail(err)}
		}

		text += strings.Replace(parts, "its own", "the own", 1) + " decode from the same object. "
	}

	text += "A member that fails to decode stops it. Otherwise it reports every member the " +
		"schema rejects."
	if opts.Validate {
		text += " A value whose members all convert must pass Validate."
	}

	body := []genStep{
		lang.Stmt("raw, err := %s(dec)", q(runtimePath, "ReadValue")),
		lang.If(lang.Expr("err != nil"), lang.Stmt("return err"), nil),
		lang.Stmt("var w %s", wireName(name)),
		lang.If(
			lang.Expr("err := %s(raw, &w); err != nil", q(runtimePath, "UnmarshalObject")),
			lang.Stmt("return err"),
			nil,
		),
		lang.Stmt("var out %s", name),
		lang.Stmt("var errs []error"),
	}

	for i := range productOf(doc, ptr, s) {
		body = append(body, lang.Stmt(
			"errs = append(errs, %s(raw, &out.%s))",
			q(runtimePath, "Unmarshal"), memberField(doc, opts, ptr, i),
		))
	}

	for _, n := range s.Properties {
		body = append(body, decodeSteps(doc, opts, ptr, n.Name)...)
	}

	body = append(body, extraSteps(doc, opts, ptr)...)

	body = append(body,
		lang.If(
			lang.Expr("err := %s(errs...); err != nil", q(validPath, "Join")),
			lang.Stmt("return err"),
			nil,
		),
		validateStep(opts, lang.Expr("out")),
		lang.Stmt("*v = out"),
		lang.Stmt("return nil"),
	)

	return []genStep{
		lang.DocFunc(
			docComment(text),
			lang.Expr(
				"func (v *%s) UnmarshalJSONFrom(dec *%s) error",
				name,
				q(jsontextPath, "Decoder"),
			),
			body...,
		),
	}
}

// extraSteps returns the statements setting the fallback field of the
// struct or product at ptr from the wire struct. Every member of a product
// decodes the whole object, so the fallback field of a member struct holds
// the members of the others. The product keeps in its own fallback field
// the members none of them declares and clears those of its members, so
// that encoding writes each member once.
func extraSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	f := fallbackField(doc, opts, ptr)

	if !product(doc, ptr, s) {
		return []genStep{lang.Stmt("out.%s = w.%s", f, f)}
	}

	args := lang.Expr("w.%s", f)
	if names := quoteAll(declaredMembers(doc, ptr, nil)); len(names) > 0 {
		args = lang.Expr("w.%s, %s", f, joinComma(names))
	}

	steps := []genStep{lang.Stmt("out.%s = %s(%s)", f, q(runtimePath, "Omit"), args)}

	for i := range productOf(doc, ptr, s) {
		if r, rs, err := resolved(doc, child(ptr, "allOf", strconv.Itoa(i))); err == nil &&
			(structKind(rs) || product(doc, r, rs)) {
			steps = append(steps, lang.Stmt(
				"out.%s.%s = nil", memberField(doc, opts, ptr, i), fallbackField(doc, opts, r),
			))
		}
	}

	return steps
}

// fallbackField returns the Go name of the field of the struct or product
// at ptr that holds the members its properties do not name, as fallbackName
// picks it.
func fallbackField(doc *jsonschema.Schema, opts Options, ptr string) genExpr {
	return func(genCtx) (string, error) {
		s, err := at(doc, ptr)
		if err != nil {
			return "", err
		}

		name, err := typeName(doc, opts, ptr)
		if err != nil {
			return "", err
		}

		taken := slices.Concat(methodNames, otherMethodNames)

		for i := range productOf(doc, ptr, s) {
			m, err := memberName(doc, opts, ptr, i)
			if err != nil {
				return "", err
			}

			taken = append(taken, fieldIdent(m))
		}

		for _, n := range s.Properties {
			// check reports a property giving no Go name.
			if f, err := identifier(
				child(ptr, "properties", n.Name),
				"property",
				n.Name,
			); err == nil {
				taken = append(taken, fieldIdent(f))
			}
		}

		return fallbackName(name, taken), nil
	}
}

// fallbackName returns AdditionalProperties, or, when taken holds it, the
// first of AdditionalProperties_2, AdditionalProperties_3, ... that taken
// does not hold. typ names the struct in the panic on running out of
// suffixes, which a finite taken never causes.
func fallbackName(typ string, taken []string) string {
	const base = "AdditionalProperties"

	if !slices.Contains(taken, base) {
		return base
	}

	// i turns negative when it overflows, which ends the loop.
	for i := 2; i > 0; i++ {
		if n := base + "_" + strconv.Itoa(i); !slices.Contains(taken, n) {
			return n
		}
	}

	panic(fmt.Sprintf(
		"jsonschema: %s: the suffix of the field %s of its additional members overflows int",
		typ, base,
	))
}

// declaredMembers returns the names of the members the object at ptr
// declares a field for, once each in the order of the fields: the
// properties of a struct and, for a product, those of its members and its
// own. A union of objects declares the members any of its members does.
// seen holds the schemas on the way to ptr, which a cycle reaches again.
func declaredMembers(doc *jsonschema.Schema, ptr string, seen []string) []string {
	r, s, err := resolved(doc, ptr)
	if err != nil || slices.Contains(seen, r) {
		return nil
	}

	seen = append(seen, r)

	var names []string

	for _, kw := range []string{"allOf", "oneOf", "anyOf"} {
		for i := range listOf(s, kw) {
			names = append(names, declaredMembers(doc, child(r, kw, strconv.Itoa(i)), seen)...)
		}
	}

	for _, n := range s.Properties {
		names = append(names, n.Name)
	}

	return slices.Collect(opiter.Unique(slices.Values(names)))
}

// decodeSteps returns the statements converting the wire field of the
// property name of the object at ptr into its field.
func decodeSteps(doc *jsonschema.Schema, opts Options, ptr, name string) []genStep {
	pp := child(ptr, "properties", name)
	f := fieldName(pp, name)
	req, null := required(doc, ptr, name), nullable(doc, pp)

	collect := lang.Stmt(
		"errs = append(errs, %s(err, %s))",
		q(validPath, "Field"),
		strconv.Quote(name),
	)
	conv := func(fn string) []genStep {
		return []genStep{
			lang.Stmt("out.%s, err = %s(w.%s)", f, q(runtimePath, fn), f),
			collect,
		}
	}

	var steps []genStep

	switch {
	case acceptsNull(doc, pp):
		fn := "DecodeOptional"
		if req {
			fn = "DecodeRequired"
		}

		steps = []genStep{
			lang.Stmt(
				"out.%s, err = %s[%s](w.%s)",
				f,
				q(runtimePath, fn),
				goType(doc, opts, pp),
				f,
			),
			collect,
		}
	case onCycle(doc, ptr, pp):
		switch {
		case req && null:
			steps = conv("RequiredNullablePointer")
		case req:
			steps = conv("RequiredPointer")
		case null:
			steps = []genStep{lang.Stmt(
				"out.%s = %s(w.%s)", f, q(runtimePath, "OptionalNullablePointer"), f,
			)}
		default:
			steps = conv("OptionalPointer")
		}
	case req && null:
		steps = conv("RequiredNullable")
	case req:
		steps = conv("Required")
	case null:
		steps = []genStep{lang.Stmt("out.%s = w.%s", f, f)}
	default:
		steps = conv("Optional")
	}

	return steps
}

// allowedValues returns the Go literals of the const or the enum of
// scalars of the schema at ptr itself, or nil when it has neither.
func allowedValues(doc *jsonschema.Schema, ptr string) ([]string, error) {
	s, err := at(doc, ptr)
	if err != nil {
		return nil, err
	}

	values := s.Enum
	if s.Const != nil {
		values = []jsontext.Value{s.Const}
	}

	if values == nil {
		return nil, nil
	}

	k, err := kinds(doc, ptr)
	if err != nil {
		return nil, err
	}

	if len(k) != 1 {
		return nil, unsupported(ptr, "enum or const of more than one kind")
	}

	return opiter.TryCollect(
		opiter.WrapSeq(slices.Values(values)).
			Disassemble(func(v jsontext.Value) (string, error) { return goLiteral(ptr, k[0], v) }),
	)
}

// goLiteral spells v as a Go constant of the Go type of the JSON type kind.
func goLiteral(ptr, kind string, v jsontext.Value) (string, error) {
	text := string(v)
	ok := false

	switch kind {
	case "string":
		var s string
		if v.Kind() == '"' && jsonv2.Unmarshal(v, &s) == nil {
			return strconv.Quote(s), nil
		}
	case "integer":
		_, err := strconv.ParseInt(text, 10, 64)
		ok = v.Kind() == '0' && err == nil
	case "number":
		_, err := strconv.ParseFloat(text, 64)
		ok = v.Kind() == '0' && err == nil
	case "boolean":
		ok = v.Kind() == 't' || v.Kind() == 'f'
	}

	if !ok {
		return "", unsupported(ptr, "value %s is no Go constant for the JSON type %s", text, kind)
	}

	return text, nil
}

// fieldName returns the Go name of the field of the property name at ptr,
// as fieldIdent spells it. check has rejected a name giving none.
func fieldName(ptr, name string) genExpr {
	return func(genCtx) (string, error) {
		f, err := identifier(ptr, "property", name)
		return fieldIdent(f), err
	}
}

// memberField returns the name of the field of the member i of the allOf
// at ptr, as fieldIdent spells it.
func memberField(doc *jsonschema.Schema, opts Options, ptr string, i int) genExpr {
	return func(genCtx) (string, error) {
		m, err := memberName(doc, opts, ptr, i)
		return fieldIdent(m), err
	}
}

// fieldIdent returns name, the Go name of a field, with the suffix Field
// when a method of the struct takes the name, as for a property named
// validate. A property named additionalProperties keeps its name, and
// fallbackName moves the fallback field out of its way instead.
func fieldIdent(name string) string {
	if slices.Contains(methodNames, name) {
		return name + "Field"
	}

	return name
}

// fieldType spells the type of the field of the property name of the
// object at ptr: the required x nullable matrix, a pointer on a cycle, and
// the Go type itself where it holds null.
func fieldType(doc *jsonschema.Schema, opts Options, ptr, name string) genExpr {
	pp := child(ptr, "properties", name)
	t := goType(doc, opts, pp)
	req := required(doc, ptr, name)

	switch {
	case acceptsNull(doc, pp):
		if req {
			return t
		}
	case onCycle(doc, ptr, pp):
		return lang.Expr("*%s", t)
	case !req && nullable(doc, pp):
		return lang.Expr("%s[%s]", q(undPath, "Und"), t)
	case req && !nullable(doc, pp):
		return t
	}

	return lang.Expr("%s[%s]", q(optPath, "Option"), t)
}

// tagFor returns the json tag of the field of the property name of the
// object at ptr. An optional member is omitted when zero.
func tagFor(doc *jsonschema.Schema, ptr, name string) string {
	if required(doc, ptr, name) {
		return strconv.Quote(name)
	}

	return strconv.Quote(name + ",omitzero")
}
