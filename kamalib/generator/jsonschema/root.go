package jsonschema

import (
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// definedSteps returns the declarations of a type defined over the
// built-in Go type of the schema at ptr: the root when it declares no type
// of its own, or a schema valueType reports. The type encodes and decodes
// as its underlying type. Its directive names the validators of the
// keywords of the schema, which UnmarshalJSONFrom then runs through
// Validate.
//
// The root needs a name to be decoded into, while a schema elsewhere in
// the document is spelled by a built-in type unless keywords need a
// Validate. The defined type loses the methods of the underlying type,
// such as those of opt.Option, so its own methods convert to it.
func definedSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	underlying := definedType(doc, opts, ptr)
	args, marked := definedArgs(doc, opts, ptr)

	decodeDoc := "UnmarshalJSONFrom decodes v as its underlying type. It leaves in v what " +
		"decoded of a value it rejects."
	decode := []genStep{converted(
		definedConv(doc, opts, ptr),
		func(conv genExpr) genStep {
			return lang.Steps(
				lang.Stmt("var w %s", definedWire(doc, opts, ptr)),
				lang.If(
					lang.Expr("err := kamaJsonschemaDecode(dec, &w); err != nil"),
					lang.Stmt("return err"),
					nil,
				),
				lang.Stmt("out, err := %s(w)", conv),
			)
		},
		func(genExpr) genStep {
			return lang.Steps(
				lang.Stmt("var out %s", underlying),
				lang.If(
					lang.Expr("err := kamaJsonschemaDecode(dec, &out); err != nil"),
					lang.Stmt("return err"),
					nil,
				),
				lang.Stmt("var err error"),
			)
		},
	)}

	text := typeDoc(doc, ptr, name+" is generated from the schema at #"+ptr+".")
	declDoc := docComment(text)

	// checks holds the validations of the value, which kamaJsonschemaChecked
	// joins with the problems of decoding.
	var checks []genExpr

	if opts.Validate && rootUnion(doc, ptr) {
		decodeDoc += " The union it holds must pass ValidateShallow, since decoding a union " +
			"fails on no value."

		holder := lang.Expr("out.ValidateShallow(false)")
		if nullable(doc, ptr) {
			holder = lang.Expr("kamaJsonschemaSomeValid(out)")
		}

		checks = append(checks, holder)
	}

	if marked {
		declDoc = validatedDoc(text, decodedArgs(opts, args))

		if opts.Validate {
			decodeDoc += " The value must pass ValidateShallow."

			// v holds the value by now, and is addressable as the pointer
			// receiver of ValidateShallow of a struct needs.
			checks = append(checks, lang.Expr("v.ValidateShallow(false)"))
		}
	}

	decode = append(decode, lang.Stmt("*v = %s(out)", name))

	switch len(checks) {
	case 0:
		decode = append(decode, lang.Stmt("return err"))
	case 1:
		decode = append(decode, lang.Stmt("return kamaJsonschemaChecked(err, %s)", checks[0]))
	default:
		decode = append(decode, lang.Stmt(
			"return kamaJsonschemaChecked(err, %s(%s))",
			q(validPath, "Join"), lang.Join(", ", checks...),
		))
	}

	steps := []genStep{
		lang.Line(declDoc),
		lang.Stmt("type %s %s", name, underlying),
		lang.DocFunc(
			docComment("MarshalJSONTo encodes v as its underlying type."),
			lang.Expr(
				"func (v %s) MarshalJSONTo(enc *%s) error", name, q(jsontextPath, "Encoder"),
			),
			lang.Stmt("return %s(enc, %s(v))", q(jsonPath, "MarshalEncode"), underlying),
		),
		lang.DocFunc(
			docComment(decodeDoc),
			lang.Expr(
				"func (v *%s) UnmarshalJSONFrom(dec *%s) error", name, q(jsontextPath, "Decoder"),
			),
			decode...,
		),
	}

	if raw(doc, ptr) {
		steps = append(steps, lang.DocFunc(
			docComment("RawJson returns v as raw JSON text, which tells the validators to check "+
				"v as JSON Schema does."),
			lang.Expr("func (v %s) RawJson() %s", name, q(jsontextPath, "Value")),
			lang.Stmt("return %s(v)", q(jsontextPath, "Value")),
		))
	}

	return steps
}

// rootUnion reports whether ptr is the root, defined over a union holder or
// a nullable one. The root has no Validate of its own to report the unknown
// member of the holder, so its decoder validates the holder.
func rootUnion(doc *jsonschema.Schema, ptr string) bool {
	if ptr != "" {
		return false
	}

	r, s, err := resolvedValue(doc, ptr)

	return err == nil && union(s) && declares(doc, r)
}

// definedArgs returns the args of //kama:validate on the defined type of
// the schema at ptr, and whether the type carries the directive: it names
// validators, the type is a slice or a map, whose Validate walks the
// elements, or the type is raw JSON text, whose Validate ref may call. A defined type of a struct,
// such as the opt.Option of a
// nullable root, carries none. Its value has validated itself on decoding.
func definedArgs(doc *jsonschema.Schema, opts Options, ptr string) ([]expr.Arg, bool) {
	args := declArgs(doc, opts, ptr)
	if len(args) > 0 || raw(doc, ptr) {
		return args, true
	}

	switch placeKind(doc, ptr) {
	case "array", "map":
		return nil, ptr != "" || !nullable(doc, ptr)
	}

	return nil, false
}

// definedType spells the underlying type of the defined type of the schema
// at ptr: its built-in Go type, in an opt.Option for a nullable root.
func definedType(doc *jsonschema.Schema, opts Options, ptr string) genExpr {
	return func(c genCtx) (string, error) {
		// goType names a root of raw JSON text after the type defined here.
		if ptr != "" || raw(doc, ptr) {
			return builtinType(c, doc, opts, ptr)
		}

		t, err := goType(doc, opts, "")(c)
		if err != nil || !nullable(doc, "") {
			return t, err
		}

		return lang.Expr("%s[%s]", q(optPath, "Option"), t)(c)
	}
}

// definedWire is definedType for the wire type the defined type of the
// schema at ptr decodes into, which records null itself unless the root
// is nullable.
func definedWire(doc *jsonschema.Schema, opts Options, ptr string) genExpr {
	return func(c genCtx) (string, error) {
		if ptr != "" || raw(doc, ptr) {
			return builtinWire(doc, opts, ptr)(c)
		}

		if !nullable(doc, "") {
			return wireType(doc, opts, "", slotMember)(c)
		}

		t, err := wireType(doc, opts, "", slotMember)(c)
		if err != nil {
			return "", err
		}

		return lang.Expr("%s[%s]", q(optPath, "Option"), t)(c)
	}
}

// definedConv is wireConv for definedWire.
func definedConv(doc *jsonschema.Schema, opts Options, ptr string) genExpr {
	return func(c genCtx) (string, error) {
		if ptr != "" || raw(doc, ptr) {
			return builtinConv(doc, opts, ptr)(c)
		}

		if !nullable(doc, "") {
			return wireConv(doc, opts, "", slotMember)(c)
		}

		conv, err := wireConv(doc, opts, "", slotMember)(c)
		if err != nil || conv == "" {
			return "", err
		}

		return "kamaJsonschemaOptionOf(" + conv + ")", nil
	}
}
