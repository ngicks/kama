package jsonschema

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"iter"
	"maps"
	"path"
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

// helpersFile returns the file of the package declaring the helpers decls,
// the declarations of the other generated files, refer to, and every
// helper those refer to in turn, in the order of their names, together
// with those names. ok is false when decls refer to none.
func helpersFile(
	opts Options,
	dir string,
	decls []ast.Decl,
) (base.Composed, []string, bool, error) {
	imports, err := newImports(opts.Package)
	if err != nil {
		return base.Composed{}, nil, false, err
	}

	c := genhelper.NewBaseContext(
		genhelper.NewScope(),
		&genhelper.Package{Imports: imports, Path: opts.Package},
	)

	// srcs holds the source of each helper rendered so far. The helpers a
	// helper refers to are known only once it is rendered, so they are
	// collected as the rendering goes.
	srcs := map[string]string{}
	todo := helperRefs(decls)

	for len(todo) > 0 {
		name := todo[0]
		todo = todo[1:]

		if _, ok := srcs[name]; ok {
			continue
		}

		step, ok := helperDecl(name)
		if !ok {
			// An identifier of the prefix naming no helper is no reference to
			// one, and the build reports it if it names nothing at all.
			continue
		}

		src, err := lang.Render(c, step)
		if err != nil {
			return base.Composed{}, nil, false, fmt.Errorf("jsonschema: helper %s: %w", name, err)
		}

		f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+src, 0)
		if err != nil {
			return base.Composed{}, nil, false, fmt.Errorf(
				"jsonschema: parse helper %s: %w",
				name,
				err,
			)
		}

		srcs[name] = src

		todo = append(todo, helperRefs(f.Decls)...)
	}

	if len(srcs) == 0 {
		return base.Composed{}, nil, false, nil
	}

	names := slices.Sorted(maps.Keys(srcs))

	var src strings.Builder

	for _, name := range names {
		src.WriteString(srcs[name])
		src.WriteString("\n")
	}

	parts, err := splitDecls(src.String())
	if err != nil {
		return base.Composed{}, nil, false, fmt.Errorf("jsonschema: parse helpers: %w", err)
	}

	return base.Composed{
		Path:    path.Join(dir, opts.file(naming.SharedFile(helperKind))),
		Package: opts.Package,
		Imports: imports,
		Parts:   parts,
	}, names, true, nil
}

// helperRefs returns the identifiers starting with helperPrefix that decls
// use and do not declare themselves, sorted and each once. Comments,
// string literals and struct tags hold no identifier, so text of a schema
// that lands there refers to no helper, and neither does an unexported
// type the generator names after a schema.
func helperRefs(decls []ast.Decl) []string {
	declared := opiter.WrapSeq(slices.Values(decls)).
		FlatMap(func(d ast.Decl) iter.Seq[string] { return slices.Values(declNames(d)) }).
		CollectSlice()

	var used []string

	for _, d := range decls {
		ast.Inspect(d, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				// The name after the dot belongs to another scope.
				ast.Inspect(n.X, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok {
						used = append(used, id.Name)
					}

					return true
				})

				return false
			case *ast.Ident:
				used = append(used, n.Name)
			}

			return true
		})
	}

	return slices.Compact(
		opiter.WrapSeq(slices.Values(used)).
			Filter(func(name string) bool {
				return strings.HasPrefix(name, helperPrefix) && !slices.Contains(declared, name)
			}).
			Collect(slices.Sorted),
	)
}

// helperKind is the kind of the file declaring the helpers, as its name
// spells it.
const helperKind = "helper"

// checkHelperNames reports a declaration of a Go file of dir in fsys that
// declares the package pkg, that the generator did not write and that is
// named like a helper of names, or that stands in the helpers file of pkg.
// The generator reads no package, so it tells the declarations apart by
// their syntax and their generated marks. An external test package shares
// the directory of the package it tests, and the package clause tells
// their files apart.
func checkHelperNames(
	fsys fs.FS,
	reg *genmark.Registry,
	dir, pkg string,
	names []string,
) error {
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
		if err != nil || f.Name.Name != pkg {
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
	case helperPrefix + "Decode":
		return helper(name+" decodes the next value of dec into v. An error is located "+
			"relative to that value. Below the root it comes in a json.SemanticError "+
			"naming the pointer of the value, as json names it only for a method that "+
			"failed before or after reading the whole value, so that the decoder of the "+
			"enclosing value locates it in turn.",
			`func kamaJsonschemaDecode(dec *%[2]s.Decoder, v any) error {
	depth := dec.StackDepth()
	err := %[1]s.UnmarshalDecode(dec, v, kamaJsonschemaPinned)
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
		return helper(name+" is kamaJsonschemaDecode for a value that must be a JSON object. "+
			"A value of another kind is skipped without being kept and returned as a "+
			"validation error.",
			`func kamaJsonschemaDecodeObject(dec *%[1]s.Decoder, v any) error {
	if k := dec.PeekKind(); k != %[1]s.KindBeginObject {
		if err := dec.SkipValue(); err != nil {
			return err
		}
		return %[2]s.New(%[2]s.ErrKind, "want object, got %%s", kamaJsonschemaKindName(k))
	}
	return kamaJsonschemaDecode(dec, v)
}`, jt, v), true
	case helperPrefix + "Checked":
		return helper(name+" joins err, the problems decoding a value recorded, with verr, "+
			"the error of ValidateShallow of the value, leaving out what verr reports at or "+
			"below a value that failed to decode. Such a value holds no value of its own, so "+
			"its validation tells nothing. A value that failed to decode as a whole keeps no "+
			"validation error.",
			`func kamaJsonschemaChecked(err, verr error) error {
	if err == nil || verr == nil {
		return %[1]s.Join(err, verr)
	}
	failed := %[1]s.Pointers(err)
	kept := []error{err}
	for _, e := range %[1]s.Under(verr, "") {
		p := %[1]s.Pointers(e)[0]
		if !%[3]s.ContainsFunc(failed, func(f string) bool {
			return p == f || %[2]s.HasPrefix(p, f+"/")
		}) {
			kept = append(kept, e)
		}
	}
	return %[1]s.Join(kept...)
}`, v, "strings", "slices"), true
	case helperPrefix + "SomeValid":
		return helper(
			name+" returns the error of ValidateShallow of the value o holds, or nil "+
				"for None.",
			`func kamaJsonschemaSomeValid[T interface{ ValidateShallow(bool) error }](
	o %[1]s.Option[T],
) error {
	if o.IsNone() {
		return nil
	}
	return o.Value().ValidateShallow(false)
}`,
			optPath,
		), true
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
	return kamaJsonschemaLocate(%[2]s.UnmarshalDecode(d, v, kamaJsonschemaPinned), 1)
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
	case helperPrefix + "RequiredOf":
		return helper(
			name+" is kamaJsonschemaRequired for a member whose wire field conv "+
				"converts.",
			`func kamaJsonschemaRequiredOf[W, T any](
	dst *T,
	u %[2]s.Und[W],
	conv func(W) (T, error),
) error {
	switch {
	case u.IsUndefined():
		return %[1]s.New(%[1]s.ErrRequired, "member is missing")
	case u.IsNull():
		return %[1]s.New(%[1]s.ErrNull, "member is null")
	}
	v, err := conv(u.Value())
	*dst = v
	return err
}`,
			v,
			u,
		), true
	case helperPrefix + "OptionalOf":
		return helper(name+" is kamaJsonschemaOptional for a member whose wire field conv "+
			"converts.",
			`func kamaJsonschemaOptionalOf[W, T any](
	dst *%[2]s.Option[T],
	u %[3]s.Und[W],
	conv func(W) (T, error),
) error {
	if u.IsNull() {
		return %[1]s.New(%[1]s.ErrNull, "member is null")
	}
	if !u.IsDefined() {
		return nil
	}
	v, err := conv(u.Value())
	*dst = %[2]s.Some(v)
	return err
}`, v, o, u), true
	case helperPrefix + "RequiredNullableOf":
		return helper(name+" is kamaJsonschemaRequiredNullable for a member whose wire field "+
			"conv converts.",
			`func kamaJsonschemaRequiredNullableOf[W, T any](
	dst *%[2]s.Option[T],
	u %[3]s.Und[W],
	conv func(W) (T, error),
) error {
	if u.IsUndefined() {
		return %[1]s.New(%[1]s.ErrRequired, "member is missing")
	}
	if u.IsNull() {
		return nil
	}
	v, err := conv(u.Value())
	*dst = %[2]s.Some(v)
	return err
}`, v, o, u), true
	case helperPrefix + "NullableOf":
		return helper(
			name+" sets dst to the value of an optional, nullable member whose "+
				"wire field conv converts.",
			`func kamaJsonschemaNullableOf[W, T any](
	dst *%[1]s.Und[T],
	u %[1]s.Und[W],
	conv func(W) (T, error),
) error {
	switch {
	case u.IsNull():
		*dst = %[1]s.Null[T]()
	case u.IsDefined():
		v, err := conv(u.Value())
		*dst = %[1]s.Defined(v)
		return err
	}
	return nil
}`,
			u,
		), true
	case helperPrefix + "RequiredPointerOf":
		return helper(
			name+" is kamaJsonschemaRequiredOf for a member held by pointer.",
			`func kamaJsonschemaRequiredPointerOf[W, T any](
	dst **T,
	u %[1]s.Und[W],
	conv func(W) (T, error),
) error {
	var v T
	err := kamaJsonschemaRequiredOf(&v, u, conv)
	if u.IsDefined() {
		*dst = &v
	}
	return err
}`,
			u,
		), true
	case helperPrefix + "OptionalPointerOf":
		return helper(
			name+" is kamaJsonschemaOptionalOf for a member held by pointer. Nil "+
				"means absent.",
			`func kamaJsonschemaOptionalPointerOf[W, T any](
	dst **T,
	u %[2]s.Und[W],
	conv func(W) (T, error),
) error {
	if u.IsNull() {
		return %[1]s.New(%[1]s.ErrNull, "member is null")
	}
	return kamaJsonschemaOptionalNullablePointerOf(dst, u, conv)
}`,
			v,
			u,
		), true
	case helperPrefix + "RequiredNullablePointerOf":
		return helper(name+" is kamaJsonschemaRequiredNullableOf for a member held by "+
			"pointer. Nil means null.",
			`func kamaJsonschemaRequiredNullablePointerOf[W, T any](
	dst **T,
	u %[2]s.Und[W],
	conv func(W) (T, error),
) error {
	if u.IsUndefined() {
		return %[1]s.New(%[1]s.ErrRequired, "member is missing")
	}
	return kamaJsonschemaOptionalNullablePointerOf(dst, u, conv)
}`, v, u), true
	case helperPrefix + "OptionalNullablePointerOf":
		return helper(name+" sets dst to the value of an optional, nullable member held by "+
			"pointer whose wire field conv converts. Nil means absent or null.",
			`func kamaJsonschemaOptionalNullablePointerOf[W, T any](
	dst **T,
	u %[1]s.Und[W],
	conv func(W) (T, error),
) error {
	if !u.IsDefined() {
		return nil
	}
	v, err := conv(u.Value())
	*dst = &v
	return err
}`, u), true
	case helperPrefix + "OptionOf":
		return helper(name+" returns a function converting an opt.Option of a wire value by "+
			"conv. None stays None.",
			`func kamaJsonschemaOptionOf[W, T any](
	conv func(W) (T, error),
) func(%[1]s.Option[W]) (%[1]s.Option[T], error) {
	return func(o %[1]s.Option[W]) (%[1]s.Option[T], error) {
		if o.IsNone() {
			return %[1]s.None[T](), nil
		}
		v, err := conv(o.Value())
		return %[1]s.Some(v), err
	}
}`, o), true
	case helperPrefix + "MembersOf":
		return helper(name+" sets dst to members, the members no property declares, each "+
			"converted by conv, and reports the error of each at its name. A nil members "+
			"leaves dst nil.",
			`func kamaJsonschemaMembersOf[W, T any](
	dst *map[string]T,
	members map[string]W,
	conv func(W) (T, error),
) error {
	if members == nil {
		return nil
	}
	out := make(map[string]T, len(members))
	var errs []error
	for name, w := range members {
		v, err := conv(w)
		if err != nil {
			errs = append(errs, %[1]s.Field(err, name))
		}
		out[name] = v
	}
	*dst = out
	return %[1]s.Join(errs...)
}`, v), true
	case helperPrefix + "NullTo":
		return helper(
			name+" turns the null u holds into the value null decodes into, for a "+
				"member whose Go type holds null itself, such as raw JSON text or a union with a "+
				"member of type null. The member decides about null as it does inside a value.",
			`func kamaJsonschemaNullTo[W any](dec *%[1]s.Decoder, u %[2]s.Und[W]) (%[2]s.Und[W], error) {
	if !u.IsNull() {
		return u, nil
	}
	var w W
	err := kamaJsonschemaUnmarshal(dec, %[1]s.Value("null"), &w)
	return %[2]s.Defined(w), err
}`,
			jsontextPath,
			u,
		), true
	case helperPrefix + "RequiredTakesNull":
		return helper(
			name+" is kamaJsonschemaRequired for a member whose Go type holds null "+
				"itself.",
			`func kamaJsonschemaRequiredTakesNull[T any](
	dst *T,
	dec *%[1]s.Decoder,
	u %[2]s.Und[T],
) error {
	u, err := kamaJsonschemaNullTo(dec, u)
	if err != nil {
		return err
	}
	return kamaJsonschemaRequired(dst, u)
}`,
			jsontextPath,
			u,
		), true
	case helperPrefix + "OptionalTakesNull":
		return helper(name+" is kamaJsonschemaOptional for a member whose Go type holds null "+
			"itself.",
			`func kamaJsonschemaOptionalTakesNull[T any](
	dst *%[3]s.Option[T],
	dec *%[1]s.Decoder,
	u %[2]s.Und[T],
) error {
	u, err := kamaJsonschemaNullTo(dec, u)
	if err != nil {
		return err
	}
	return kamaJsonschemaOptional(dst, u)
}`, jsontextPath, u, o), true
	case helperPrefix + "RequiredTakesNullOf":
		return helper(name+" is kamaJsonschemaRequiredOf for a member whose Go type holds null "+
			"itself.",
			`func kamaJsonschemaRequiredTakesNullOf[W, T any](
	dst *T,
	dec *%[1]s.Decoder,
	u %[2]s.Und[W],
	conv func(W) (T, error),
) error {
	u, err := kamaJsonschemaNullTo(dec, u)
	if err != nil {
		return err
	}
	return kamaJsonschemaRequiredOf(dst, u, conv)
}`, jsontextPath, u), true
	case helperPrefix + "OptionalTakesNullOf":
		return helper(name+" is kamaJsonschemaOptionalOf for a member whose Go type holds null "+
			"itself.",
			`func kamaJsonschemaOptionalTakesNullOf[W, T any](
	dst *%[3]s.Option[T],
	dec *%[1]s.Decoder,
	u %[2]s.Und[W],
	conv func(W) (T, error),
) error {
	u, err := kamaJsonschemaNullTo(dec, u)
	if err != nil {
		return err
	}
	return kamaJsonschemaOptionalOf(dst, u, conv)
}`, jsontextPath, u, o), true
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
	case helperPrefix + "Stubbed":
		return helper(name+" returns the first error of errs, the errors of the members "+
			"of a union, that wraps validator.ErrStub, or nil.",
			`func kamaJsonschemaStubbed(errs []error) error {
	for _, err := range errs {
		if %[1]s.Is(err, %[2]s.ErrStub) {
			return err
		}
	}
	return nil
}`, "errors", v), true
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
			"values, each of which must marshal to an object, with the options of enc. A member "+
			"name already written is skipped.",
			`func kamaJsonschemaMarshalObjects(enc *%[1]s.Encoder, values ...any) error {
	seen := map[string]bool{}
	if err := enc.WriteToken(%[1]s.BeginObject); err != nil {
		return err
	}
	for _, v := range values {
		b, err := %[2]s.Marshal(v, enc.Options())
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
