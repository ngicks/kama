package validator

import (
	"errors"
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/validator/rule"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// fixedNames are the identifiers the signature and the body of every
// Validate spell. They are reserved against imports so that they keep
// their names. A type parameter or a package-level declaration of the same
// name makes them take fresh names, as local does.
var fixedNames = []string{"v", "stopOnFirstError", "errs", "err"}

// writer writes the Validate method of the target n as source text.
type writer struct {
	*emitter
	sel *selection
	n   *typegraph.Node
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

// method returns the source of Validate of w.n.
func (w *writer) method() (string, error) {
	named := w.n.Detail.Type

	body, err := w.body(named.Underlying())
	if err != nil {
		return "", fmt.Errorf("%s (%s): %w", w.n.Id, w.scope.Position(named.Obj().Pos()), err)
	}

	recv := named.Obj().Name()
	params := []genExpr{lang.Expr("%s bool", decl("stopOnFirstError"))}
	tparams := w.tparams()

	if len(tparams) > 0 {
		recv += "[" + strings.Join(tparams, ", ") + "]"
		for _, tp := range tparams {
			params = append(params, lang.Expr(
				"%s func(v %s, stopOnFirstError bool) error", decl(validateRole(tp)), tp,
			))
		}
	}

	doc := docComment("Validate reports why %s is invalid. With %s it returns the first\n"+
		"error found, otherwise every error joined, or nil when there is none.",
		lang.Name("v"), lang.Name("stopOnFirstError"))
	if len(tparams) > 0 {
		doc = docComment("Validate reports why %s is invalid. With %s it returns the first\n"+
			"error found, otherwise every error joined, or nil when there is none.\n"+
			"A function given for a type parameter validates the values of that type;\n"+
			"nil leaves them unchecked.",
			lang.Name("v"), lang.Name("stopOnFirstError"))
	}

	return lang.Render(
		w.scope.FuncContext(tparams),
		lang.DocFunc(
			doc,
			lang.Expr("func (%s %s) Validate(%s) error",
				decl("v"), recv, lang.Join(", ", params...)),
			body...,
		),
	)
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
// type u. It reports every error of the fields.
func (w *writer) body(u types.Type) ([]genStep, error) {
	var (
		steps []genStep
		errs  []error
	)

	switch u := u.(type) {
	case *types.Struct:
		for i := range u.NumFields() {
			s, err := w.field(u, i)
			steps = append(steps, s...)
			errs = append(errs, err...)
		}
	case *types.Slice, *types.Map:
		s, err := w.nested(u, lang.Name("v"), noWrap, 0)
		if s != nil {
			steps = append(steps, s)
		}

		errs = append(errs, err)
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return w.collect(steps), nil
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
		"return %s.Join(%s...)", w.Import(rule.ValidationPath), lang.Name("errs"),
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

	for _, vo := range w.sel.opts.Loaded.Option.Field(w.n.Id, v.Name()).Validators {
		s, err := w.validator(v, vo, x, wrap)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		steps = append(steps, s)
	}

	s, err := w.nested(v.Type(), x, wrap, 0)
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
// adds no segment, as encoders inline its fields.
func (w *writer) fieldWrap(st *types.Struct, i int, outer wrapFunc) wrapFunc {
	v := st.Field(i)

	tag := reflect.StructTag(st.Tag(i)).Get("json")
	name, _, _ := strings.Cut(tag, ",")

	if tag == "-" {
		name = ""
	}

	if name == "" && v.Embedded() {
		elem := v.Type()
		if p, ok := types.Unalias(elem).(*types.Pointer); ok {
			elem = p.Elem()
		}

		if _, ok := elem.Underlying().(*types.Struct); ok {
			return outer
		}
	}

	if name == "" {
		name = v.Name()
	}

	return func(err genExpr) genExpr {
		return outer(lang.Expr(
			"%s.Field(%s, %s)", w.Import(rule.ValidationPath), err, strconv.Quote(name),
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

// validator returns the step running the validator vo on the field v whose
// value x denotes.
func (w *writer) validator(
	v *types.Var,
	vo ValidatorOptions,
	x genExpr,
	wrap wrapFunc,
) (genStep, error) {
	val, ok := w.sel.opts.Registry.Lookup(vo.Name)
	if !ok {
		return nil, fmt.Errorf(
			"%w: %s: unknown validator %s on field %s; known: %s",
			ErrDirective, vo.src.origin(), vo.src.spelling(), v.Name(),
			strings.Join(w.sel.opts.Registry.Names(), ", "),
		)
	}

	s, err := w.rule(val, vo.Args, v.Type(), x, wrap)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %s: %s on field %s: %w",
			ErrUnsupported, vo.src.origin(), vo.src.spelling(), v.Name(), err,
		)
	}

	return s, nil
}

// rule parses arg with val and applies the rule to the value x of type t.
func (w *writer) rule(
	val rule.Validator,
	arg string,
	t types.Type,
	x genExpr,
	wrap wrapFunc,
) (genStep, error) {
	r, err := val.Parse(arg)
	// An error of the args alone would repeat for the value t holds, so
	// apply never sees it.
	if err != nil {
		return nil, err
	}

	return w.apply(r, t, x, wrap)
}

// apply returns the step running r on the value x of type t. When r
// cannot apply to t and t holds one value, as a pointer or an opt.Option
// does, it applies r to that value when there is one. The error tells
// why r applies to neither.
func (w *writer) apply(r rule.Rule, t types.Type, x genExpr, wrap wrapFunc) (genStep, error) {
	checkErr := r.Check(t)
	if checkErr == nil {
		// Emit runs as the expression renders, so that only rendered code
		// declares helpers.
		return w.check(wrap(func(c genCtx) (string, error) {
			e, err := r.Emit(w.emitter, t, x)
			if err != nil {
				return "", err
			}

			return e(c)
		})), nil
	}

	held, guard, value, ok := heldValue(t, x)
	if !ok {
		return nil, checkErr
	}

	s, err := w.apply(r, held, value, wrap)
	if err != nil {
		return nil, fmt.Errorf("%w; for the value it holds: %w", checkErr, err)
	}

	return lang.If(guard, s, nil), nil
}

// heldValue returns the one value a pointer, an opt.Option or an und.Und x
// of type t may hold: its type, the condition of x holding it and the
// expression of it.
func heldValue(t types.Type, x genExpr) (types.Type, genExpr, genExpr, bool) {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		return p.Elem(), lang.Expr("%s != nil", x), lang.Expr("(*%s)", x), true
	}

	switch {
	case typequery.IsNamed(t, typequery.OptPath, "Option"):
		return typequery.TypeArg(t),
			lang.Expr("%s.IsSome()", x),
			lang.Expr("%s.Value()", x),
			true
	case typequery.IsNamed(t, typequery.UndPath, "Und"):
		return typequery.TypeArg(t),
			lang.Expr("%s.IsDefined()", x),
			lang.Expr("%s.Value()", x),
			true
	}

	return nil, nil, nil, false
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
