package validator

import (
	"errors"
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// fixedNames are the identifiers the signature and the body of every
// Validate spell. They are reserved against imports so that they keep
// their names. A type parameter or a package-level declaration of the same
// name makes them take fresh names, as local does.
var fixedNames = []string{"v", "stopOnFirstError", "ev", "errs", "err"}

// writer writes the Validate method of the target n as source text, its
// ValidateShallow when shallow is set, or its ValidateEvaluated when
// evaluated is set.
type writer struct {
	*emitter
	sel       *selection
	n         *typegraph.Node
	shallow   bool
	evaluated bool
}

// tparams returns the names the method spells the type parameters of w.n
// by.
func (w *writer) tparams() []string {
	return base.MethodTypeParams(
		w.scope.Pkg.Types.Scope(),
		w.n.Detail.Type.TypeParams(),
		fixedNames,
	)
}

// wrapFunc returns the expression wrapping the error err with path
// segments.
type wrapFunc func(err genExpr) genExpr

func noWrap(err genExpr) genExpr { return err }

// method returns the source of Validate of w.n, of ValidateShallow when
// w.shallow is set, or of ValidateEvaluated when w.evaluated is set.
func (w *writer) method() (string, error) {
	named := w.n.Detail.Type
	delegates := w.shallow && !w.sel.shallowDiffers(named)

	body, err := w.body(named.Underlying())
	if err != nil {
		return "", fmt.Errorf("%s (%s): %w", w.n.Id, w.scope.Position(named.Obj().Pos()), err)
	}

	recv := named.Obj().Name()
	params := []genExpr{lang.Expr("%s bool", decl("stopOnFirstError"))}
	tparams := w.tparams()
	// post runs in a deferred call, which reports through the named result.
	result := lang.Expr("error")

	if !delegates && w.sel.typeOptions(w.n.Id).Post != "" {
		result = lang.Expr("(%s error)", decl("err"))
	}

	if len(tparams) > 0 {
		recv += "[" + strings.Join(tparams, ", ") + "]"
		for _, tp := range tparams {
			params = append(params, lang.Expr(
				"%s func(v %s, stopOnFirstError bool) error", decl(validateRole(tp)), tp,
			))
		}
	}

	text := "Validate reports why %s is invalid. With %s it returns the first\n" +
		"error found, otherwise every error joined, or nil when there is none."
	name := "Validate"

	if w.shallow {
		text = "ValidateShallow is Validate for a decoded %s. With %s it returns the\n" +
			"first error found, otherwise every error joined, or nil when there is none.\n" +
			"It takes the values it holds of a type marked decoded as valid, since\n" +
			"their decoders validated them, and calls ValidateShallow where Validate\n" +
			"calls a Validate that has one."
		name = shallowName
	}

	if delegates {
		text = "ValidateShallow is Validate for a decoded %s, whose decoder calls it.\n" +
			"%s is passed on to Validate, since %s holds no value ValidateShallow takes\n" +
			"as valid."
	}

	if w.evaluated {
		text = "ValidateEvaluated is Validate recording into %s the members and the\n" +
			"elements of %s that its validators evaluated, as JSON Schema annotations\n" +
			"tell unevaluatedProperties and unevaluatedItems. A nil %s records nothing."
		name = evaluatedName

		params = append(
			params,
			lang.Expr("%s *%s.Evaluated", decl("ev"), w.Import(ext.RawJsonPath)),
		)
	}

	if len(tparams) > 0 {
		text += "\nA function given for a type parameter validates the values of that type;\n" +
			"nil leaves them unchecked."
	}

	names := docNames(w.evaluated)

	if delegates {
		args := []genExpr{lang.Name("stopOnFirstError")}
		for _, tp := range tparams {
			args = append(args, lang.Name(validateRole(tp)))
		}

		body = []genStep{lang.Stmt(
			"return %s.Validate(%s)", lang.Name("v"), lang.Join(", ", args...),
		)}
		names = []any{lang.Name("v"), lang.Name("stopOnFirstError"), lang.Name("v")}
	}

	return lang.Render(
		w.scope.FuncContext(tparams),
		lang.DocFunc(
			docComment(text, names...),
			lang.Expr("func (%s %s) %s(%s) %s",
				decl("v"), recv, name, lang.Join(", ", params...), result),
			body...,
		),
	)
}

// docNames returns the names the doc comment of method spells, in order.
func docNames(evaluated bool) []any {
	if evaluated {
		return []any{lang.Name("ev"), lang.Name("v"), lang.Name("ev")}
	}

	return []any{lang.Name("v"), lang.Name("stopOnFirstError")}
}

// local returns the argument of lang.Expr declaring the local named name:
// under exactly that name, as genhelper.Shadow does, unless a type
// parameter of w.n or a package-level declaration of the package has the
// name. The local would then hide that type parameter or declaration from
// the types spelled in its scope, such as the parameter types of a nested
// function literal, so it gets a fresh name.
func (w *writer) local(name string) any {
	if slices.Contains(w.tparams(), name) || w.scope.Pkg.Types.Scope().Lookup(name) != nil {
		return decl(name)
	}

	return genhelper.Shadow(name)
}

// validateRole is the role of the parameter validating values of the type
// parameter named tp.
func validateRole(tp string) string { return "validate" + tp }

// body returns the statements of Validate for a target of the underlying
// type u: the function pre of the type names, the validators of the type,
// then those of the fields and the walk of the values u holds. The
// function post of the type names runs in a deferred call, so that it runs
// after every return. validator.Root locates a plain error of pre or post
// at the value, so that a decoder records it as an error of validation
// rather than failing on it. It reports every error of them.
func (w *writer) body(u types.Type) ([]genStep, error) {
	var (
		steps []genStep
		errs  []error
	)

	o := w.sel.typeOptions(w.n.Id)
	if o.Pre != "" {
		if err := w.sel.checkTypeFunc(w.n, o.Pre, o.preSrc); err != nil {
			errs = append(errs, err)
		} else {
			steps = append(steps, w.check(lang.Expr(
				"%s.Root(%s(%s, %s))",
				w.Import(ext.ValidationPath), o.Pre, lang.Name("v"), lang.Name("stopOnFirstError"),
			)))
		}
	}

	for _, vo := range w.sel.typeValidators(w.n.Id) {
		s, err := w.typeValidator(u, vo)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		steps = append(steps, s)
	}

	switch u := u.(type) {
	case *types.Struct:
		for i := range u.NumFields() {
			s, err := w.field(u, i)
			steps = append(steps, s...)
			errs = append(errs, err...)
		}
	case *types.Slice, *types.Array, *types.Map:
		s, err := w.nested(u, lang.Name("v"), noWrap, 0)
		if s != nil {
			steps = append(steps, s)
		}

		errs = append(errs, err)
	}

	var post []genStep

	if o.Post != "" {
		if err := w.sel.checkTypeFunc(w.n, o.Post, o.postSrc); err != nil {
			errs = append(errs, err)
		} else {
			vp := w.Import(ext.ValidationPath)
			post = append(post, lang.Stmt(
				"defer func() { %s = %s.Join(%s, %s.Root(%s(%s, %s))) }()",
				lang.Name("err"), vp, lang.Name("err"),
				vp, o.Post, lang.Name("v"), lang.Name("stopOnFirstError"),
			))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return append(post, w.collect(steps)...), nil
}

// collect returns steps between the declaration of the errors they collect
// and the return of them joined. No steps give a body returning nil.
func (w *writer) collect(steps []genStep) []genStep {
	if len(steps) == 0 {
		return []genStep{lang.Stmt("return nil")}
	}

	// A function literal for a type argument collects into its own errs.
	// Its errs may shadow the errs of the method.
	out := []genStep{lang.Stmt("var %s []error", w.local("errs"))}
	out = append(out, steps...)

	return append(out, lang.Stmt(
		"return %s.Join(%s...)", w.Import(ext.ValidationPath), lang.Name("errs"),
	))
}

// field returns the steps of the field i of st: its validators, the
// validation of the values it holds and its hook.
func (w *writer) field(st *types.Struct, i int) ([]genStep, []error) {
	v := st.Field(i)
	if !w.accessible(v) {
		return nil, nil
	}

	wrap := w.fieldWrap(st, i, noWrap)
	x := lang.Expr("%s.%s", lang.Name("v"), v.Name())

	var (
		steps []genStep
		errs  []error
	)

	for _, vo := range w.sel.loadedOptions.Option.Field(w.n.Id, v.Name()).Validators {
		s, err := w.validator(st, i, vo, x, wrap)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if s != nil {
			steps = append(steps, s)
		}
	}

	s, err := w.heldNested(v.Type(), x, wrap, 0, w.fieldItems(v), true)
	if err != nil {
		errs = append(errs, fmt.Errorf("field %s: %w", v.Name(), err))
	} else if s != nil {
		steps = append(steps, s)
	}

	if w.sel.hookOn(w.n, v) {
		if err := w.sel.checkHook(w.n, v); err != nil {
			errs = append(errs, err)
		} else {
			steps = append(steps, w.check(wrap(lang.Expr(
				"%s.%s(%s)", lang.Name("v"), hookName(v), lang.Name("stopOnFirstError"),
			))))
		}
	}

	return steps, errs
}

// fieldWrap returns the wrapping of the errors of the field i of st with its
// path segment, then with outer: the name in its json tag when it has one
// other than "-", else its Go name. An embedded struct without a json name
// and a field tagged json:",embed" add no segment, as encoders inline
// their fields or entries.
func (w *writer) fieldWrap(st *types.Struct, i int, outer wrapFunc) wrapFunc {
	v := st.Field(i)
	name, _ := tagName(st, i)

	if name == "" && v.Embedded() && isStructOrPointer(v.Type()) || tagOption(st, i, "embed") {
		return outer
	}

	if name == "" {
		name = v.Name()
	}

	return func(err genExpr) genExpr {
		return outer(lang.Expr(
			"%s.Field(%s, %s)", w.Import(ext.ValidationPath), err, strconv.Quote(name),
		))
	}
}

// accessible reports whether generated code can read the field v.
func (w *writer) accessible(v *types.Var) bool { return accessible(v, w.scope.Pkg.Types) }

// accessible reports whether code of pkg can read the field v: it is not
// blank, and it is exported or declared in pkg. An unexported field of a
// struct of another package, as in type T other.S, cannot be spelled.
func accessible(v *types.Var, pkg *types.Package) bool {
	return v.Name() != "_" && (v.Exported() || v.Pkg() == pkg)
}

// validator returns the step running the validator vo on the field i of
// st, whose value x denotes.
func (w *writer) validator(
	st *types.Struct,
	i int,
	vo ValidatorOptions,
	x genExpr,
	wrap wrapFunc,
) (genStep, error) {
	v := st.Field(i)
	place := "field " + v.Name()

	switch {
	case slices.Contains(levelWords, vo.Item.Name):
		return nil, nil
	case vo.Item.Name != "":
		return w.role(vo, vo.Item, w.fieldItems(v), place, v.Type(), x, wrap, 0)
	}

	r, err := w.resolve(vo, vo.Item.Value, place)
	if err != nil {
		return nil, err
	}

	if sr, ok := r.(ext.SiblingRule); ok {
		s, err := w.sibling(sr, st, i, x)
		if err != nil {
			return nil, ruleError(vo, place, err)
		}

		return s, nil
	}

	if _, ok := r.(ext.StructRule); ok && !acceptsHeld(w.sel.opts.Registry, r, v.Type()) {
		return nil, fmt.Errorf(
			"%w: %s: %s on %s validates a struct as a whole;"+
				" write it in the doc comment of a struct type",
			ErrDirective, vo.src.origin(), vo.src.spelling(), place,
		)
	}

	s, err := w.apply(r, v.Type(), x, wrap)
	if err != nil {
		return nil, ruleError(vo, place, err)
	}

	return s, nil
}

// sibling returns the step running sr on the field i of st, whose value x
// denotes. The rule tests the field itself, so a pointer or an option
// counts as present by its own test and is never unwrapped. Its errors are
// located at the struct. The rule adds the segment of each error itself.
func (w *writer) sibling(sr ext.SiblingRule, st *types.Struct, i int, x genExpr) (genStep, error) {
	v := st.Field(i)

	name, _ := tagName(st, i)
	if name == "" {
		name = v.Name()
	}

	r, err := sr.Siblings(structView{st: st, x: lang.Name("v"), pkg: w.scope.Pkg.Types}, name)
	if err != nil {
		return nil, err
	}

	if err := r.Check(v.Type()); err != nil {
		return nil, err
	}

	return w.emit(r, v.Type(), x, noWrap), nil
}

// typeValidator returns the step running the validator vo, named in the
// doc comment of w.n, on the receiver of Validate. u is the underlying type
// of w.n. Only an ext.StructRule applies to a struct type, and only to one.
// An ext.SiblingRule applies to no type.
func (w *writer) typeValidator(u types.Type, vo ValidatorOptions) (genStep, error) {
	named := w.n.Detail.Type
	place := "type " + named.Obj().Name()

	if vo.Item.Name != "" {
		return w.role(vo, vo.Item, nil, place, named, lang.Name("v"), noWrap, 0)
	}

	r, err := w.resolve(vo, vo.Item.Value, place)
	if err != nil {
		return nil, err
	}

	sr, isStructRule := r.(ext.StructRule)
	_, isSiblingRule := r.(ext.SiblingRule)
	st, isStruct := u.(*types.Struct)

	switch {
	case isStruct && isStructRule:
		r, err = sr.Struct(structView{st: st, x: lang.Name("v"), pkg: w.scope.Pkg.Types})
		if err != nil {
			return nil, ruleError(vo, place, err)
		}
	case isStruct:
		return nil, fmt.Errorf(
			"%w: %s: %s on struct %s; a struct type takes only validators of the struct"+
				" as a whole, so write it on a field",
			ErrDirective, vo.src.origin(), vo.src.spelling(), place,
		)
	case isStructRule && r.Check(named) != nil:
		return nil, fmt.Errorf(
			"%w: %s: %s on %s validates a struct as a whole, and %s is no struct",
			ErrDirective, vo.src.origin(), vo.src.spelling(), place, named.Obj().Name(),
		)
	case isSiblingRule:
		return nil, fmt.Errorf(
			"%w: %s: %s on %s reads the siblings of a struct field; write it on a field",
			ErrDirective, vo.src.origin(), vo.src.spelling(), place,
		)
	}

	s, err := w.apply(r, named, lang.Name("v"), noWrap)
	if err != nil {
		return nil, ruleError(vo, place, err)
	}

	return s, nil
}

// resolve returns the rule of n, a rule of the item vo, which resolves the
// rules it holds through the registry, as a combinator does. place names
// what n applies to in errors, as in field Name. An error of the args alone
// would repeat for the value a pointer or an option holds, so apply never
// sees it.
func (w *writer) resolve(vo ValidatorOptions, n expr.Node, place string) (ext.Rule, error) {
	res := ext.WithWarn(w.sel.opts.Registry, func(err error) {
		if w.sel.warn != nil {
			w.sel.warn(fmt.Errorf(
				"%s: %s on %s: %w", vo.src.origin(), vo.src.spelling(), place, err,
			))
		}
	})

	r, err := ext.Resolve(res, n)
	if errors.Is(err, ext.ErrUnknown) {
		return nil, fmt.Errorf(
			"%w: %s: %s on %s: %w", ErrDirective, vo.src.origin(), vo.src.spelling(), place, err,
		)
	}

	if err != nil {
		return nil, ruleError(vo, place, err)
	}

	return r, nil
}

// ruleError reports err of the rule of the validator vo applied to place.
func ruleError(vo ValidatorOptions, place string, err error) error {
	return fmt.Errorf(
		"%w: %s: %s on %s: %w", ErrUnsupported, vo.src.origin(), vo.src.spelling(), place, err,
	)
}

// apply returns the step running r on the value x of type t. When r
// cannot apply to t and t holds one value, as ext.HeldValue tells for a
// pointer or an opt.Option, it applies r to that value when there is one.
// The error tells why r applies to neither.
func (w *writer) apply(r ext.Rule, t types.Type, x genExpr, wrap wrapFunc) (genStep, error) {
	checkErr := r.Check(t)
	if checkErr == nil {
		return w.emit(r, t, x, wrap), nil
	}

	held, guard, value, ok := ext.HeldValue(w.sel.opts.Registry, t, x)
	if !ok {
		return nil, checkErr
	}

	s, err := w.apply(r, held, value, wrap)
	if err != nil {
		return nil, fmt.Errorf("%w; for the value it holds: %w", checkErr, err)
	}

	return lang.If(guard, s, nil), nil
}

// acceptsHeld reports whether r applies to t or to the value t holds, as
// apply tries them. An ext.StructRule applies outside a struct type this
// way, as members does to raw JSON text.
func acceptsHeld(reg ext.Resolver, r ext.Rule, t types.Type) bool {
	for {
		if r.Check(t) == nil {
			return true
		}

		held, _, _, ok := ext.HeldValue(reg, t, nil)
		if !ok {
			return false
		}

		t = held
	}
}

// emit returns the step running r, which accepted t, on the value x of
// type t. In ValidateEvaluated an Annotator records into ev.
func (w *writer) emit(r ext.Rule, t types.Type, x genExpr, wrap wrapFunc) genStep {
	// Emit runs as the expression renders, so that only rendered code
	// declares helpers.
	return w.check(wrap(func(c genCtx) (string, error) {
		emit := r.Emit
		if a, ok := r.(ext.Annotator); ok && w.evaluated {
			// The function literals of the rule declare records of their own,
			// which a name resolved later would refer to, so the parameter is
			// spelled now.
			ev, err := lang.Name("ev")(c)
			if err != nil {
				return "", err
			}

			emit = func(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
				return a.Annotate(e, t, v, lang.Expr("%s", ev))
			}
		}

		e, err := emit(w.emitter, t, x)
		if err != nil {
			return "", err
		}

		return e(c)
	}))
}

// check returns the statement collecting the error the expression err
// gives, or returning it when stopOnFirstError is set.
func (w *writer) check(err genExpr) genStep {
	report := genSteps{
		lang.If(lang.Name("stopOnFirstError"), lang.Stmt("return %s", lang.Name("err")), nil),
		lang.Stmt("%s = append(%s, %s)", lang.Name("errs"), lang.Name("errs"), lang.Name("err")),
	}

	return genhelper.FuncStep[genCtx](func(c genCtx, b []byte) (genCtx, []byte, error) {
		outer := c.Scope()
		// The variable of the init statement belongs to the if statement, so
		// it is declared in a scope of its own.
		c = genhelper.Push(c)

		cond, e := lang.Expr(
			"%s := %s; %s != nil", w.local("err"), err, lang.Name("err"),
		)(c)
		if e != nil {
			return c, nil, e
		}

		c, body, e := report.Render(c, nil)
		if e != nil {
			return c, nil, e
		}

		p := c.Printer(b)
		p.Printf("if %s {\n%s}\n", cond, body)

		return c.WithScope(outer), p.Bytes(), nil
	})
}
