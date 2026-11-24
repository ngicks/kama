package rule

import (
	"errors"
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"
)

// Requires is the validator //kama:validate requires="<name> ...": when
// the field is present, each sibling field named is present too, as JSON
// Schema dependentRequired tests. A name is the JSON member name a sibling
// field encodes as. Present means what Required tests: an option holds a
// value, a nillable value is not nil, and any other value is not its zero
// value. An und.Und or an elastic.Elastic holding null is therefore absent.
//
// It is a SiblingRule. It applies to a struct field whose type Required
// applies to, and each sibling named must be of such a type. The error of a
// missing sibling is located at that sibling.
type Requires struct{}

// Name returns "requires".
func (Requires) Name() string { return "requires" }

// RequiresOptions are the options of //kama:validate requires="<name> ...".
type RequiresOptions struct {
	// Names are the JSON member names of the sibling fields, in order.
	Names []string
}

// Parse decodes arg into one or more names separated by spaces.
func (Requires) Parse(arg string) (Rule, error) {
	args, err := argTokens(arg)
	if err != nil {
		return nil, err
	}

	if len(args) == 0 {
		return nil, argsError(errors.New("want one or more JSON names of sibling fields"))
	}

	names := make([]string, 0, len(args))

	for _, kv := range args {
		if kv.HasValue {
			return nil, argsError(fmt.Errorf("arg %q is no JSON name", kv.String()))
		}

		names = append(names, kv.Key)
	}

	return RequiresOptions{Names: names}, nil
}

// Check rejects every type: requires needs the struct holding the field,
// which Siblings gives.
func (RequiresOptions) Check(types.Type) error { return errRequiresSiblings }

// Emit fails as Check does.
func (RequiresOptions) Emit(Emitter, types.Type, Gen) (Gen, error) {
	return nil, errRequiresSiblings
}

// Siblings returns the rule testing the field named field of s and the
// siblings of o.Names. Each name must be a member of s whose type Required
// applies to.
func (o RequiresOptions) Siblings(s Struct, field string) (Rule, error) {
	for _, name := range o.Names {
		t, _, ok := s.Member(name)
		if !ok {
			return nil, fmt.Errorf("no sibling field encodes as the member %q", name)
		}

		if err := (RequiredOptions{}).Check(t); err != nil {
			return nil, fmt.Errorf("sibling %q has no presence test: %w", name, err)
		}
	}

	return requiresOf{names: o.Names, field: field, s: s}, nil
}

var errRequiresSiblings = errors.New("requires reads the siblings of a struct field;" +
	" write it on a field of a struct")

// requiresOf is Requires applied to the field named field of s.
type requiresOf struct {
	names []string
	field string
	s     Struct
}

// Check accepts the types Required accepts.
func (requiresOf) Check(t types.Type) error { return RequiredOptions{}.Check(t) }

// Emit calls kamaValidateRequires with the presence of the field and of
// each sibling.
func (r requiresOf) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	e.Declare(requiresName, requiresDecl)

	present, err := presenceCond(e, t, v)
	if err != nil {
		return nil, err
	}

	siblings := []Gen{present}

	for _, name := range r.names {
		st, sv, ok := r.s.Member(name)
		if !ok {
			return nil, fmt.Errorf("requires: no sibling %q", name)
		}

		cond, err := presenceCond(e, st, sv)
		if err != nil {
			return nil, err
		}

		siblings = append(siblings, cond)
	}

	names := opiter.WrapSeq(slices.Values(r.names)).
		Map(strconv.Quote).
		CollectSlice()

	return lang.Expr("%s(%s, %s, %s, []string{%s}, %s)",
		requiresName, e.StopOnFirstError(), strconv.Quote(r.field), siblings[0],
		strings.Join(names, ", "), lang.Join(", ", siblings[1:]...)), nil
}

const requiresName = "kamaValidateRequires"

func requiresDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s bool, %s string, %s bool, %s []string, %s ...bool) error",
			requiresName, shadow("stopOnFirstError"), shadow("field"), shadow("present"),
			shadow("names"), shadow("siblings")),
		lang.If(lang.Expr("!%s", ref("present")), lang.Stmt("return nil"), nil),
		lang.Stmt("var %s []error", shadow("errs")),
		// For renders its clause in the scope of the loop, which the range
		// variables are declared in.
		lang.For(nil, lang.Expr("%s, %s := range %s", shadow("i"), shadow("ok"), ref("siblings")),
			nil,
			lang.If(
				lang.Expr("!%s", ref("ok")),
				lang.Steps(
					lang.Stmt(
						`%s := %s.Field(%s.New(%s.ErrRequired, "want a value, as %%q has one", %s), %s[%s])`,
						shadow("err"),
						v,
						v,
						v,
						ref("field"),
						ref("names"),
						ref("i"),
					),
					lang.If(
						lang.Expr("%s", ref("stopOnFirstError")),
						lang.Stmt("return %s", ref("err")),
						nil,
					),
					lang.Stmt("%s = append(%s, %s)", ref("errs"), ref("errs"), ref("err")),
				),
				nil,
			),
		),
		lang.Stmt("return %s.Join(%s...)", v, ref("errs")),
	)
}
