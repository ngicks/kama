package jsonschema

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
)

// helperPrefix starts the name of every helper the generated decoders
// call. The helpers are unexported, so no other package sees them.
const helperPrefix = "kamaJsonschema"

// helperRef matches a reference to a helper in generated source text.
var helperRef = regexp.MustCompile(`\b` + helperPrefix + `[A-Za-z0-9]*\b`)

// helpersFile returns the file of the package declaring the helpers text,
// the source of the other generated declarations, refers to, and every
// helper those refer to in turn, in the order of their names. ok is false
// when text refers to none.
func helpersFile(opts Options, dir, text string) (base.Composed, bool, error) {
	imports, err := newImports(opts.Package)
	if err != nil {
		return base.Composed{}, false, err
	}

	c := genhelper.NewBaseContext(
		genhelper.NewScope(),
		&genhelper.Package{Imports: imports, Path: opts.Package},
	)

	// decls holds the source of each helper rendered so far. The helpers a
	// text refers to are known only once it is rendered, so they are
	// collected as the rendering goes.
	decls := map[string]string{}
	todo := helperRef.FindAllString(text, -1)

	for len(todo) > 0 {
		name := todo[0]
		todo = todo[1:]

		if _, ok := decls[name]; ok {
			continue
		}

		step, ok := helperDecl(name)
		if !ok {
			return base.Composed{}, false, fmt.Errorf("jsonschema: no helper %s", name)
		}

		src, err := lang.Render(c, step)
		if err != nil {
			return base.Composed{}, false, fmt.Errorf("jsonschema: helper %s: %w", name, err)
		}

		decls[name] = src
		todo = append(todo, helperRef.FindAllString(src, -1)...)
	}

	if len(decls) == 0 {
		return base.Composed{}, false, nil
	}

	var src strings.Builder

	for _, name := range slices.Sorted(maps.Keys(decls)) {
		src.WriteString(decls[name])
		src.WriteString("\n")
	}

	parts, err := splitDecls(src.String())
	if err != nil {
		return base.Composed{}, false, fmt.Errorf("jsonschema: parse helpers: %w", err)
	}

	return base.Composed{
		Path:    path.Join(dir, naming.SharedFile(helperKind)),
		Package: opts.Package,
		Imports: imports,
		Parts:   parts,
	}, true, nil
}

// helperKind is the kind of the file declaring the helpers, as its name
// spells it.
const helperKind = "helper"

// checkHelperNames reports a declaration of a Go file of dir in fsys that
// the generator did not write and that is named like a helper of names, or
// that stands in the helpers file. The generator reads no package, so it
// tells the declarations apart by their syntax and their generated marks.
func checkHelperNames(fsys fs.FS, reg *genmark.Registry, dir string, names []string) error {
	if fsys == nil {
		return nil
	}

	entries, err := fs.ReadDir(fsys, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("jsonschema: read %q: %w", dir, err)
	}

	var errs []error

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}

		file := path.Join(dir, e.Name())

		src, err := fs.ReadFile(fsys, file)
		if err != nil {
			return fmt.Errorf("jsonschema: read %q: %w", file, err)
		}

		f, err := parser.ParseFile(
			token.NewFileSet(), file, src, parser.ParseComments|parser.SkipObjectResolution,
		)
		if err != nil {
			// The build reports a file that does not parse.
			continue
		}

		helpers := naming.IsShared(helperKind, e.Name())

		for _, d := range f.Decls {
			if own(reg, d) {
				continue
			}

			if helpers {
				if g, ok := d.(*ast.GenDecl); !ok || g.Tok != token.IMPORT {
					errs = append(errs, base.ExplainConflict(
						fmt.Errorf("%w: %s holds a declaration the generator did not write",
							ErrConflict, file),
						reg,
						base.AstDeclDocs(d)...,
					))
				}

				continue
			}

			for _, name := range declNames(d) {
				if slices.Contains(names, name) {
					errs = append(errs, base.ExplainConflict(
						fmt.Errorf(
							"%w: %s declares %s, which names a helper of the generated decoders",
							ErrConflict,
							file,
							name,
						),
						reg,
						base.AstDeclDocs(d)...,
					))
				}
			}
		}
	}

	return errors.Join(errs...)
}

// own reports whether d carries the generated mark of this generator.
func own(reg *genmark.Registry, d ast.Decl) bool {
	return slices.ContainsFunc(base.AstDeclDocs(d), func(doc *ast.CommentGroup) bool {
		name, ok := reg.Generated(doc)
		return ok && name == GeneratorId().String()
	})
}

// declNames returns the package-level names d declares. A method declares
// none.
func declNames(d ast.Decl) []string {
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Recv == nil {
			return []string{d.Name.Name}
		}
	case *ast.GenDecl:
		return opiter.WrapSeq(slices.Values(d.Specs)).
			FlatMap(func(s ast.Spec) func(func(string) bool) {
				return slices.Values(specNames(s))
			}).
			CollectSlice()
	}

	return nil
}

func specNames(s ast.Spec) []string {
	switch s := s.(type) {
	case *ast.TypeSpec:
		return []string{s.Name.Name}
	case *ast.ValueSpec:
		return opiter.WrapSeq(slices.Values(s.Names)).
			Map(func(n *ast.Ident) string { return n.Name }).
			CollectSlice()
	}

	return nil
}

// pkg spells the identifier the file refers to the package at path by,
// recording the import.
func pkg(path string) genExpr {
	return func(c genCtx) (string, error) {
		s, err := c.Package().Qual(path, "X")
		return strings.TrimSuffix(s, ".X"), err
	}
}

// helper returns the declaration of a helper as the source text format,
// whose verbs %[1]s, %[2]s, ... spell the packages at paths.
func helper(doc, format string, paths ...string) genStep {
	args := opiter.WrapSeq(slices.Values(paths)).
		Map(func(p string) any { return pkg(p) }).
		CollectSlice()

	return lang.Line(lang.Join("", docComment(doc), lang.Expr(format, args...)))
}

// helperDecl returns the declaration of the helper name.
//
// The helpers stay plain functions over the standard library, the
// validator package and package option, so that a package generated from
// a schema imports nothing of kama but the runtime module.
func helperDecl(name string) (genStep, bool) {
	const (
		j  = jsonPath
		jt = jsontextPath
		v  = validPath
	)

	switch name {
	case helperPrefix + "Pinned":
		return helper(
			name+" are the options a generated decoder decodes with whatever its caller "+
				"passes. Member names match exactly, as JSON Schema compares them. Errors "+
				"come as json/v2 reports them, since a method error a legacy report passes "+
				"on bare loses the location of the value that failed.",
			`var kamaJsonschemaPinned = %[1]s.JoinOptions(
	%[1]s.MatchCaseInsensitiveNames(false),
	%[2]s.ReportErrorsWithLegacySemantics(false),
)`,
			j,
			jsonv1Path,
		), true
	case helperPrefix + "Own":
		return helper(name+" are the unmarshalers every generated decoder adds to those of "+
			"its caller.",
			`var kamaJsonschemaOwn = %[1]s.JoinUnmarshalers(
	kamaJsonschemaRejectNull,
	kamaJsonschemaIntegerNumber,
)`,
			j), true
	case helperPrefix + "Joined":
		return helper(
			name+" holds the last unmarshalers of a caller and those joined with "+
				"kamaJsonschemaOwn from them. A decoder nested in another sees the joined "+
				"ones, and recognizing them keeps it from joining kamaJsonschemaOwn once "+
				"more per level of nesting, each join starting with empty caches.",
			`var kamaJsonschemaJoined %[1]s.Pointer[[2]*%[2]s.Unmarshalers]`,
			"sync/atomic",
			j,
		), true
	case helperPrefix + "Unmarshalers":
		return helper(
			name+" returns the unmarshalers of opts, the options of a caller, "+
				"followed by kamaJsonschemaOwn. An unmarshaler of the caller thus takes "+
				"precedence for the types it handles, at every depth.",
			`func kamaJsonschemaUnmarshalers(opts %[1]s.Options) *%[1]s.Unmarshalers {
	caller, _ := %[1]s.GetOption(opts, %[1]s.WithUnmarshalers)
	if caller == nil || caller == kamaJsonschemaOwn {
		return kamaJsonschemaOwn
	}
	if last := kamaJsonschemaJoined.Load(); last != nil && (caller == last[0] || caller == last[1]) {
		return last[1]
	}
	joined := %[1]s.JoinUnmarshalers(caller, kamaJsonschemaOwn)
	kamaJsonschemaJoined.Store(&[2]*%[1]s.Unmarshalers{caller, joined})
	return joined
}`,
			j,
		), true
	case helperPrefix + "RejectNull":
		return helper(
			name+" rejects JSON null for a string, a number, a boolean, a slice "+
				"and a map, since nullability is expressed by the types of package option. It "+
				"leaves raw JSON text alone, which carries null as data.",
			`var kamaJsonschemaRejectNull = %[1]s.UnmarshalFromFunc(
	func(dec *%[2]s.Decoder, v any) error {
		_, isValue := v.(*%[2]s.Value)
		_, isRaw := v.(interface{ RawJson() %[2]s.Value })
		if isValue || isRaw || dec.PeekKind() != %[2]s.KindNull {
			return %[3]s.ErrUnsupported
		}
		switch %[4]s.TypeOf(v).Elem().Kind() {
		case %[4]s.String, %[4]s.Int64, %[4]s.Float64, %[4]s.Bool, %[4]s.Slice, %[4]s.Map:
			return %[5]s.New(%[5]s.ErrNull, "value is null")
		}
		return %[3]s.ErrUnsupported
	},
)`,
			j,
			jt,
			"errors",
			"reflect",
			v,
		), true
	case helperPrefix + "IntegerNumber":
		return helper(
			name+" decodes a number with a zero fraction, such as 1.0 or 1e2, "+
				"into an int64, since JSON Schema calls such a number an integer.",
			`var kamaJsonschemaIntegerNumber = %[1]s.UnmarshalFromFunc(
	func(dec *%[2]s.Decoder, v *int64) error {
		if dec.PeekKind() != %[2]s.KindNumber {
			return %[3]s.ErrUnsupported
		}
		raw, err := dec.ReadValue()
		if err != nil {
			return err
		}
		n, ok := %[4]s.Int64(string(raw))
		if !ok {
			return %[5]s.New(%[5]s.ErrKind, "want integer in the range of int64, got %%s", raw)
		}
		*v = n
		return nil
	},
)`,
			j,
			jt,
			"errors",
			rawjsonPath,
			v,
		), true
	case helperPrefix + "Decode":
		return helper(name+" decodes the next value of dec into v. An error is located "+
			"relative to that value. Below the root it comes in a json.SemanticError "+
			"naming the pointer of the value, as json names it only for a method that "+
			"failed before or after reading the whole value, so that the decoder of the "+
			"enclosing value locates it in turn.",
			`func kamaJsonschemaDecode(dec *%[2]s.Decoder, v any) error {
	depth := dec.StackDepth()
	err := %[1]s.UnmarshalDecode(
		dec, v, %[1]s.WithUnmarshalers(kamaJsonschemaUnmarshalers(dec.Options())), kamaJsonschemaPinned,
	)
	se, ok := err.(*%[1]s.SemanticError)
	if !ok || depth == 0 {
		return kamaJsonschemaLocate(err, depth)
	}
	var base %[2]s.Pointer
	n := 0
	for tok := range se.JSONPointer.Tokens() {
		if n == depth {
			break
		}
		base = base.AppendToken(tok)
		n++
	}
	return &%[1]s.SemanticError{JSONPointer: base, Err: kamaJsonschemaLocate(err, depth)}
}`, j, jt), true
	case helperPrefix + "DecodeObject":
		return helper(name+" is kamaJsonschemaDecode for a value that must be a JSON object.",
			`func kamaJsonschemaDecodeObject(dec *%[1]s.Decoder, v any) error {
	if k := dec.PeekKind(); k != %[1]s.KindBeginObject {
		if err := dec.SkipValue(); err != nil {
			return err
		}
		return %[2]s.New(%[2]s.ErrKind, "want object, got %%s", kamaJsonschemaKindName(k))
	}
	return kamaJsonschemaDecode(dec, v)
}`, jt, v), true
	case helperPrefix + "Unmarshal":
		return helper(name+" decodes raw, a value dec read, into v with the options of dec, "+
			"so that it decodes as a value dec decodes. An error is located relative to raw. "+
			"raw decodes as the element of an array, so that a decoder failing in it "+
			"names the pointer of raw, as kamaJsonschemaDecode does below the root.",
			`func kamaJsonschemaUnmarshal(dec *%[1]s.Decoder, raw %[1]s.Value, v any) error {
	d := %[1]s.NewDecoder(%[3]s.MultiReader(
		%[4]s.NewReader("["), %[5]s.NewReader(raw), %[4]s.NewReader("]"),
	), dec.Options())
	if _, err := d.ReadToken(); err != nil {
		return err
	}
	return kamaJsonschemaLocate(%[2]s.UnmarshalDecode(
		d, v, %[2]s.WithUnmarshalers(kamaJsonschemaUnmarshalers(d.Options())), kamaJsonschemaPinned,
	), 1)
}`, jt, j, "io", "strings", "bytes"), true
	case helperPrefix + "UnmarshalObject":
		return helper(name+" is kamaJsonschemaUnmarshal for a value that must be a JSON object.",
			`func kamaJsonschemaUnmarshalObject(dec *%[1]s.Decoder, raw %[1]s.Value, v any) error {
	if raw.Kind() != %[1]s.KindBeginObject {
		return %[2]s.New(%[2]s.ErrKind, "want object, got %%s", kamaJsonschemaKindName(raw.Kind()))
	}
	return kamaJsonschemaUnmarshal(dec, raw, v)
}`, jt, v), true
	case helperPrefix + "Locate":
		return helper(name+" returns err, a json.SemanticError of a decoder depth values "+
			"deep, as a validation error located relative to the value at that depth. It "+
			"takes a token of digits for an array index, which gives the same pointer as "+
			"an object member of that name.",
			`func kamaJsonschemaLocate(err error, depth int) error {
	se, ok := err.(*%[1]s.SemanticError)
	if !ok {
		return err
	}
	leaf := se.Err
	if _, ok := %[2]s.AsType[*%[3]s.Error](leaf); !ok {
		// The pointer moves to the validation path, so the copy drops it and the
		// offset to keep it from showing twice.
		c := *se
		c.ByteOffset = 0
		c.JSONPointer = ""
		leaf = &c
	}
	tokens := %[4]s.Collect(se.JSONPointer.Tokens())
	for _, tok := range %[4]s.Backward(tokens[min(depth, len(tokens)):]) {
		if i, err := %[5]s.Atoi(tok); err == nil && i >= 0 && %[5]s.Itoa(i) == tok {
			leaf = %[3]s.Index(leaf, i)
		} else {
			leaf = %[3]s.Field(leaf, tok)
		}
	}
	return leaf
}`, j, "errors", v, "slices", "strconv"), true
	case helperPrefix + "KindName":
		return helper(name+" names the JSON type of a value of kind k.",
			`func kamaJsonschemaKindName(k %[1]s.Kind) string {
	switch k {
	case %[1]s.KindNull:
		return "null"
	case %[1]s.KindTrue, %[1]s.KindFalse:
		return "boolean"
	case %[1]s.KindString:
		return "string"
	case %[1]s.KindNumber:
		return "number"
	case %[1]s.KindBeginObject:
		return "object"
	case %[1]s.KindBeginArray:
		return "array"
	}
	return "invalid"
}`, jt), true
	}

	return conversionDecl(name)
}

// conversionDecl returns the declaration of the helper name converting a
// wire field into the field of a struct.
func conversionDecl(name string) (genStep, bool) {
	const (
		v = validPath
		o = optPath
		u = undPath
	)

	switch name {
	case helperPrefix + "Required":
		return helper(name+" sets dst to the value of a required, non-nullable member.",
			`func kamaJsonschemaRequired[T any](dst *T, u %[2]s.Und[T]) error {
	switch {
	case u.IsUndefined():
		return %[1]s.New(%[1]s.ErrRequired, "member is missing")
	case u.IsNull():
		return %[1]s.New(%[1]s.ErrNull, "member is null")
	}
	*dst = u.Value()
	return nil
}`, v, u), true
	case helperPrefix + "Optional":
		return helper(name+" sets dst to the value of an optional, non-nullable member. "+
			"None means absent.",
			`func kamaJsonschemaOptional[T any](dst *%[2]s.Option[T], u %[3]s.Und[T]) error {
	if u.IsNull() {
		return %[1]s.New(%[1]s.ErrNull, "member is null")
	}
	*dst = u.Flatten()
	return nil
}`, v, o, u), true
	case helperPrefix + "RequiredNullable":
		return helper(
			name+" sets dst to the value of a required, nullable member. None "+
				"means null.",
			`func kamaJsonschemaRequiredNullable[T any](dst *%[2]s.Option[T], u %[3]s.Und[T]) error {
	if u.IsUndefined() {
		return %[1]s.New(%[1]s.ErrRequired, "member is missing")
	}
	*dst = u.Flatten()
	return nil
}`,
			v,
			o,
			u,
		), true
	case helperPrefix + "RequiredPointer":
		return helper(name+" is kamaJsonschemaRequired for a member held by pointer.",
			`func kamaJsonschemaRequiredPointer[T any](dst **T, u %[1]s.Und[T]) error {
	var v T
	if err := kamaJsonschemaRequired(&v, u); err != nil {
		return err
	}
	*dst = &v
	return nil
}`, u), true
	case helperPrefix + "OptionalPointer":
		return helper(name+" is kamaJsonschemaOptional for a member held by pointer. Nil "+
			"means absent.",
			`func kamaJsonschemaOptionalPointer[T any](dst **T, u %[2]s.Und[T]) error {
	if u.IsNull() {
		return %[1]s.New(%[1]s.ErrNull, "member is null")
	}
	kamaJsonschemaOptionalNullablePointer(dst, u)
	return nil
}`, v, u), true
	case helperPrefix + "RequiredNullablePointer":
		return helper(name+" is kamaJsonschemaRequiredNullable for a member held by "+
			"pointer. Nil means null.",
			`func kamaJsonschemaRequiredNullablePointer[T any](dst **T, u %[2]s.Und[T]) error {
	if u.IsUndefined() {
		return %[1]s.New(%[1]s.ErrRequired, "member is missing")
	}
	kamaJsonschemaOptionalNullablePointer(dst, u)
	return nil
}`, v, u), true
	case helperPrefix + "OptionalNullablePointer":
		return helper(name+" sets dst to the value of an optional, nullable member held by "+
			"pointer. Nil means absent or null.",
			`func kamaJsonschemaOptionalNullablePointer[T any](dst **T, u %[1]s.Und[T]) {
	if u.IsDefined() {
		v := u.Value()
		*dst = &v
	}
}`, u), true
	case helperPrefix + "Present":
		return helper(name+" is the wire field of a member whose Go type holds null "+
			"itself, such as raw JSON text or a union with a member of type null. It tells "+
			"a missing member from one holding null, which decodes into value.",
			`type kamaJsonschemaPresent[T any] struct {
	ok    bool
	value T
}

// UnmarshalJSONFrom decodes the value of p.
func (p *kamaJsonschemaPresent[T]) UnmarshalJSONFrom(dec *%[1]s.Decoder) error {
	p.ok = true
	return %[2]s.UnmarshalDecode(dec, &p.value)
}`, jsontextPath, jsonPath), true
	case helperPrefix + "RequiredPresent":
		return helper(name+" sets dst to the value of a required member whose Go type holds "+
			"null itself.",
			`func kamaJsonschemaRequiredPresent[T any](dst *T, p kamaJsonschemaPresent[T]) error {
	if !p.ok {
		return %[1]s.New(%[1]s.ErrRequired, "member is missing")
	}
	*dst = p.value
	return nil
}`, v), true
	case helperPrefix + "OptionalPresent":
		return helper(
			name+" sets dst to the value of an optional member whose Go type holds "+
				"null itself. None means absent.",
			`func kamaJsonschemaOptionalPresent[T any](dst *%[1]s.Option[T], p kamaJsonschemaPresent[T]) {
	if p.ok {
		*dst = %[1]s.Some(p.value)
	}
}`,
			o,
		), true
	}

	return unionHelperDecl(name)
}

// unionHelperDecl returns the declaration of the helper name of a union or
// of a product of allOf.
func unionHelperDecl(name string) (genStep, bool) {
	const (
		j  = jsonPath
		jt = jsontextPath
		v  = validPath
	)

	switch name {
	case helperPrefix + "NoMatch":
		return helper(name+" reports a value no member of a union decodes. kind tells "+
			"a oneOf from an anyOf, and errs holds why each member failed.",
			`func kamaJsonschemaNoMatch(kind error, errs []error) error {
	return %[1]s.Join(append([]error{%[1]s.New(kind, "value matches no member")}, errs...)...)
}`, v), true
	case helperPrefix + "UnknownMember":
		return helper(name+" returns the error of the unknown member of a union, err as "+
			"decoding left it. A nil err, as of an unknown member built in Go, gives the "+
			"error of no match.",
			`func kamaJsonschemaUnknownMember(kind, err error) error {
	if err == nil {
		return %[1]s.New(kind, "value matches no member")
	}
	return err
}`, v), true
	case helperPrefix + "Members":
		return helper(name+" yields the members of raw, an object. It stops silently at "+
			"malformed input, since raw is read whole and checked by the decoder before. "+
			"That decoder may allow duplicate names and invalid UTF-8, so the scan allows "+
			"them too.",
			`func kamaJsonschemaMembers(raw %[1]s.Value) %[2]s.Seq2[string, %[1]s.Value] {
	return func(yield func(string, %[1]s.Value) bool) {
		dec := %[1]s.NewDecoder(
			%[3]s.NewReader(raw), %[1]s.AllowDuplicateNames(true), %[1]s.AllowInvalidUTF8(true),
		)
		if tok, err := dec.ReadToken(); err != nil || tok.Kind() != %[1]s.KindBeginObject {
			return
		}
		for dec.PeekKind() == %[1]s.KindString {
			tok, err := dec.ReadToken()
			if err != nil {
				return
			}
			// ReadValue voids tok, so the name is taken first.
			name := tok.String()
			value, err := dec.ReadValue()
			if err != nil {
				return
			}
			if !yield(name, value.Clone()) {
				return
			}
		}
	}
}`, jt, "iter", "bytes"), true
	case helperPrefix + "HasMembers":
		return helper(name+" reports whether raw is an object with every member of names.",
			`func kamaJsonschemaHasMembers(raw %[1]s.Value, names ...string) bool {
	found := make([]bool, len(names))
	for name := range kamaJsonschemaMembers(raw) {
		for i, n := range names {
			if n == name {
				found[i] = true
			}
		}
	}
	return raw.Kind() == %[1]s.KindBeginObject && !%[2]s.Contains(found, false)
}`, jt, "slices"), true
	case helperPrefix + "OnlyMembers":
		return helper(name+" reports whether raw is an object whose every member is one of "+
			"names.",
			`func kamaJsonschemaOnlyMembers(raw %[1]s.Value, names ...string) bool {
	for name := range kamaJsonschemaMembers(raw) {
		if !%[2]s.Contains(names, name) {
			return false
		}
	}
	return raw.Kind() == %[1]s.KindBeginObject
}`, jt, "slices"), true
	case helperPrefix + "MemberEquals":
		return helper(name+" reports whether raw is an object whose member name equals the "+
			"JSON text want as a JSON value.",
			`func kamaJsonschemaMemberEquals(raw %[1]s.Value, name, want string) bool {
	for n, v := range kamaJsonschemaMembers(raw) {
		if n == name {
			return %[2]s.Equal(v, %[1]s.Value(want))
		}
	}
	return false
}`, jt, rawjsonPath), true
	case helperPrefix + "Omit":
		return helper(
			name+" returns the members of members not named by names, or nil when "+
				"none is left.",
			`func kamaJsonschemaOmit(
	members map[string]%[1]s.Value,
	names ...string,
) map[string]%[1]s.Value {
	var out map[string]%[1]s.Value
	for name, v := range members {
		if %[2]s.Contains(names, name) {
			continue
		}
		if out == nil {
			out = map[string]%[1]s.Value{}
		}
		out[name] = v
	}
	return out
}`,
			jt,
			"slices",
		), true
	case helperPrefix + "MarshalObjects":
		return helper(name+" writes one object holding the members of every value of "+
			"values, each of which must marshal to an object. A member name already "+
			"written is skipped.",
			`func kamaJsonschemaMarshalObjects(enc *%[1]s.Encoder, values ...any) error {
	seen := map[string]bool{}
	if err := enc.WriteToken(%[1]s.BeginObject); err != nil {
		return err
	}
	for _, v := range values {
		b, err := %[2]s.Marshal(v)
		if err != nil {
			return err
		}
		raw := %[1]s.Value(b)
		if raw.Kind() != %[1]s.KindBeginObject {
			return %[3]s.New(%[3]s.ErrKind, "want object, got %%s", kamaJsonschemaKindName(raw.Kind()))
		}
		for name, value := range kamaJsonschemaMembers(raw) {
			if seen[name] {
				continue
			}
			seen[name] = true
			if err := enc.WriteToken(%[1]s.String(name)); err != nil {
				return err
			}
			if err := enc.WriteValue(value); err != nil {
				return err
			}
		}
	}
	return enc.WriteToken(%[1]s.EndObject)
}`, jt, j, v), true
	}

	return nil, false
}
