package jsonschema

import (
	"slices"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// A decoder decodes a value into a type of package wire where its Go type cannot tell what the
// schema rejects: a null where encoding/json/v2 decodes it into the zero value, and a number
// such as 1.0 for an int64. The wire type records such a value, and the conversion to the Go
// type reports it with the other errors of the value. wireType spells the type a value decodes
// into and wireConv the function converting it into the Go type of the value.

// slot is where a decoded value sits, which decides who tells its null apart.
type slot int

const (
	// slotMember is a value inside und.Und or opt.Option, or the member of a union, which never
	// sees null unless the member admits it.
	slotMember slot = iota
	// slotAlone is a value nothing around rejects null for: the value of a defined type and
	// of a member no property declares.
	slotAlone
	// slotElem is an element of a wire.NonNullSlice or a value of a wire.NonNullMap, which the
	// container decodes and records null for.
	slotElem
)

// wireType spells the type the schema at ptr decodes into at the slot at. It is the Go type of
// the schema where that type tells everything the schema rejects.
func wireType(doc *jsonschema.Schema, opts Options, ptr string, at slot) genExpr {
	return func(c genCtx) (string, error) { return wireTypeSeen(c, doc, opts, ptr, at, nil) }
}

// elemWireType is wireType for a value elemType spells, whose opt.Option tells null apart.
func elemWireType(doc *jsonschema.Schema, opts Options, ptr string, at slot) genExpr {
	return func(c genCtx) (string, error) { return elemWireSeen(c, doc, opts, ptr, at, nil) }
}

// wireConv spells the function converting the value of wireType into the Go type of the schema
// at ptr, a func(W) (T, error), or the empty string where wireType spells the Go type itself.
func wireConv(doc *jsonschema.Schema, opts Options, ptr string, at slot) genExpr {
	return func(c genCtx) (string, error) { return convSeen(c, doc, opts, ptr, at, nil) }
}

// elemWireConv is wireConv for elemWireType.
func elemWireConv(doc *jsonschema.Schema, opts Options, ptr string, at slot) genExpr {
	return func(c genCtx) (string, error) { return elemConvSeen(c, doc, opts, ptr, at, nil) }
}

// builtinWire is wireType for the built-in Go type builtinType spells.
func builtinWire(doc *jsonschema.Schema, opts Options, ptr string, at slot) genExpr {
	return func(c genCtx) (string, error) {
		r, s, err := resolved(doc, ptr)
		if err != nil {
			return "", err
		}

		return builtinWireSeen(c, doc, opts, r, s, at, []string{r})
	}
}

// builtinConv is wireConv for builtinWire.
func builtinConv(doc *jsonschema.Schema, opts Options, ptr string, at slot) genExpr {
	return func(c genCtx) (string, error) {
		r, s, err := resolved(doc, ptr)
		if err != nil {
			return "", err
		}

		return builtinConvSeen(c, doc, opts, r, s, at, []string{r})
	}
}

// converted returns the step then renders when conv spells a conversion and the step same
// renders when it spells none.
func converted(conv genExpr, then, same func(conv genExpr) genStep) genStep {
	return genhelper.FuncStep[genCtx](func(c genCtx, b []byte) (genCtx, []byte, error) {
		text, err := conv(c)
		if err != nil {
			return c, nil, err
		}

		step := same(conv)
		if text != "" {
			step = then(lang.Expr("%s", text))
		}

		return step.Render(c, b)
	})
}

func wireTypeSeen(
	c genCtx,
	doc *jsonschema.Schema,
	opts Options,
	ptr string,
	at slot,
	seen []string,
) (string, error) {
	r, s, err := resolved(doc, ptr)
	if err != nil {
		return "", err
	}

	if slices.Contains(seen, r) {
		return "", unsupported(r, "holds itself through no declared type")
	}

	seen = append(seen, r)

	switch {
	case s.Bool.IsSome() || declares(doc, r) || r == "" && raw(doc, r):
		return goTypeSeen(c, doc, opts, r, seen[:len(seen)-1])
	case nullableUnion(doc, r):
		return wireTypeSeen(c, doc, opts, valueMember(doc, r), at, seen)
	}

	return builtinWireSeen(c, doc, opts, r, s, at, seen)
}

func convSeen(
	c genCtx,
	doc *jsonschema.Schema,
	opts Options,
	ptr string,
	at slot,
	seen []string,
) (string, error) {
	r, s, err := resolved(doc, ptr)
	if err != nil {
		return "", err
	}

	if slices.Contains(seen, r) {
		return "", unsupported(r, "holds itself through no declared type")
	}

	seen = append(seen, r)

	switch {
	case s.Bool.IsSome() || declares(doc, r) || r == "" && raw(doc, r):
		return "", nil
	case nullableUnion(doc, r):
		return convSeen(c, doc, opts, valueMember(doc, r), at, seen)
	}

	return builtinConvSeen(c, doc, opts, r, s, at, seen)
}

func elemWireSeen(
	c genCtx,
	doc *jsonschema.Schema,
	opts Options,
	ptr string,
	at slot,
	seen []string,
) (string, error) {
	if !nullable(doc, ptr) {
		return wireTypeSeen(c, doc, opts, ptr, at, seen)
	}

	t, err := wireTypeSeen(c, doc, opts, ptr, slotMember, seen)
	if err != nil {
		return "", err
	}

	return lang.Expr("%s[%s]", q(optPath, "Option"), t)(c)
}

func elemConvSeen(
	c genCtx,
	doc *jsonschema.Schema,
	opts Options,
	ptr string,
	at slot,
	seen []string,
) (string, error) {
	if !nullable(doc, ptr) {
		return convSeen(c, doc, opts, ptr, at, seen)
	}

	conv, err := convSeen(c, doc, opts, ptr, slotMember, seen)
	if err != nil || conv == "" {
		return "", err
	}

	return "kamaJsonschemaOptionOf(" + conv + ")", nil
}

// builtinWireSeen is wireType for the built-in Go type of the schema s at r.
func builtinWireSeen(
	c genCtx,
	doc *jsonschema.Schema,
	opts Options,
	r string,
	s *jsonschema.Schema,
	at slot,
	seen []string,
) (string, error) {
	kind, err := builtinKind(doc, r)
	if err != nil {
		return "", err
	}

	switch kind {
	case "string", "number", "boolean":
		t, err := builtinTypeSeen(c, doc, opts, r, s, seen)
		if err != nil || at != slotAlone {
			return t, err
		}

		return lang.Expr("%s[%s]", q(wirePath, "NonNull"), t)(c)
	case "integer":
		if at == slotElem {
			return "int64", nil
		}

		return q(wirePath, "Int64")(c)
	case "array", "object":
		elem, ok := containerElem(doc, r, s, kind)

		wired, err := containerWired(c, doc, opts, elem, ok, at, seen)
		if err != nil || !wired {
			return builtinTypeSeen(c, doc, opts, r, s, seen)
		}

		e, err := q(jsontextPath, "Value")(c)
		if ok {
			e, err = elemWireSeen(c, doc, opts, elem, slotElem, seen)
		}

		if err != nil {
			return "", err
		}

		if kind == "array" {
			return lang.Expr("%s[%s]", q(wirePath, "NonNullSlice"), e)(c)
		}

		return lang.Expr("%s[string, %s]", q(wirePath, "NonNullMap"), e)(c)
	}

	return builtinTypeSeen(c, doc, opts, r, s, seen)
}

// builtinConvSeen is wireConv for builtinWireSeen.
func builtinConvSeen(
	c genCtx,
	doc *jsonschema.Schema,
	opts Options,
	r string,
	s *jsonschema.Schema,
	at slot,
	seen []string,
) (string, error) {
	kind, err := builtinKind(doc, r)
	if err != nil {
		return "", err
	}

	switch kind {
	case "string", "number", "boolean":
		if at != slotAlone {
			return "", nil
		}
	case "integer":
		if at == slotElem {
			return "", nil
		}
	case "array", "object":
		elem, ok := containerElem(doc, r, s, kind)

		wired, err := containerWired(c, doc, opts, elem, ok, at, seen)
		if err != nil || !wired {
			return "", err
		}

		conv := ""
		if ok {
			conv, err = elemConvSeen(c, doc, opts, elem, slotElem, seen)
		}

		if err != nil {
			return "", err
		}

		switch {
		case conv == "":
		case kind == "array":
			return lang.Expr("%s(%s)", q(wirePath, "SliceOf"), conv)(c)
		default:
			return lang.Expr("%s[string](%s)", q(wirePath, "MapOf"), conv)(c)
		}
	default:
		return "", nil
	}

	t, err := builtinWireSeen(c, doc, opts, r, s, at, seen)

	return t + ".Value", err
}

// builtinKind returns the JSON type of the built-in Go type of the schema at r, or the empty
// string for raw JSON text.
func builtinKind(doc *jsonschema.Schema, r string) (string, error) {
	if raw(doc, r) {
		return "", nil
	}

	k, err := kinds(doc, r)
	if err != nil {
		return "", err
	}

	nonNull := opiter.WrapSeq(slices.Values(k)).
		Filter(func(k string) bool { return k != "null" }).
		CollectSlice()
	if len(nonNull) != 1 {
		return "", unsupported(r, "admits no single kind")
	}

	return nonNull[0], nil
}

// containerElem returns the pointer of the schema of the elements of the slice or the values
// of the map the schema s at r of the JSON type kind is, as builtinType spells it. ok is false
// for elements of raw JSON text.
func containerElem(
	doc *jsonschema.Schema,
	r string,
	s *jsonschema.Schema,
	kind string,
) (string, bool) {
	if kind == "object" {
		return child(r, "additionalProperties"), true
	}

	if s.Items == nil || bridged(doc, r, s, "items") {
		return "", false
	}

	return child(r, "items"), true
}

// containerWired reports whether a slice or a map of the elements of the schema at elem, or
// of raw JSON text when typed is false, decodes into a wire container at the slot at: where
// nothing around rejects a null container, where its elements cannot tell null apart, or where
// they decode into a wire type.
func containerWired(
	c genCtx,
	doc *jsonschema.Schema,
	opts Options,
	elem string,
	typed bool,
	at slot,
	seen []string,
) (bool, error) {
	switch {
	case at == slotAlone:
		return true, nil
	case !typed:
		return false, nil
	case !decidesNull(doc, elem):
		return true, nil
	}

	conv, err := elemConvSeen(c, doc, opts, elem, slotElem, seen)

	return conv != "", err
}

// decidesNull reports whether the Go type of a value of the schema at ptr outside an object
// property tells null apart itself: an opt.Option, raw JSON text or a type the generator
// declares, whose decoder rejects or keeps null.
func decidesNull(doc *jsonschema.Schema, ptr string) bool {
	r, s, err := resolved(doc, ptr)

	return err == nil && (s.Bool.IsSome() || nullable(doc, ptr) || declares(doc, r) || raw(doc, r))
}
