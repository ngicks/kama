package validator

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// Length is the validator //kama:validate length="<op><n> ...": len of
// the value satisfies every comparison, as in >=1 <=10 or ==3. The operators
// are >, >=, ==, !=, <= and <, and n is a non-negative integer literal.
// n may carry a unary plus.
//
// It applies to types of a string, slice, array, map or channel underlying
// type. The length of a string is its number of bytes.
type Length struct{}

// Name returns "length".
func (Length) Name() string { return "length" }

// Check accepts comparisons with non-negative integers, on the types the
// doc comment of Length lists.
func (Length) Check(t types.Type, arg string) error {
	args, err := argTokens(arg)
	if err != nil {
		return err
	}

	if err := comparisons(args); err != nil {
		return argsError(err)
	}

	for _, kv := range args {
		_, n, _ := comparison(kv)

		val, err := literalValue(types.Typ[types.Int], n)
		if err != nil {
			return argsError(err)
		}

		if constant.Sign(val) < 0 {
			return argsError(fmt.Errorf("length %s is negative", n))
		}

		// int accepts 1.0 and 'a' as well, which are no lengths as written.
		// literalValue parsed n already. cmp accepts a unary plus, so length
		// does too.
		x, _ := parser.ParseExpr(n)
		if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.ADD {
			x = u.X
		}

		if lit, ok := x.(*ast.BasicLit); !ok || lit.Kind != token.INT {
			return argsError(fmt.Errorf("length %s is not an integer literal", n))
		}
	}

	if !hasLength(t) {
		return fmt.Errorf("%s is not a string, slice, array, map or channel type", t)
	}

	return nil
}

// Emit calls kamaValidateLength once per comparison.
func (Length) Emit(e Emitter, _ types.Type, v string, arg string) (string, error) {
	args, err := argTokens(arg)
	if err != nil {
		return "", err
	}

	e.Declare(compareName, compareDecl)
	e.Declare(lengthName, lengthDecl)

	return joinCalls(e, args, func(op, n string) string {
		return lengthName + "(len(" + v + "), " + strconv.Quote(op) + ", " + n + ")"
	})
}

// hasLength reports whether len applies to a value of t.
func hasLength(t types.Type) bool {
	if isStringType(t) {
		return true
	}

	if _, isParam := types.Unalias(t).(*types.TypeParam); isParam {
		return false
	}

	switch t.Underlying().(type) {
	case *types.Slice, *types.Array, *types.Map, *types.Chan:
		return true
	}

	return false
}

// Cmp is the validator //kama:validate cmp="<op><value> ...": the value
// satisfies every comparison, as in >=1 <100 or =="a". The operators are
// >, >=, ==, !=, <= and <, and value is a Go literal of the type of the
// value: a number, a quoted string, true or false.
//
// It applies to types of an integer, float or string underlying type, and
// with == and != only to types of a bool underlying type.
type Cmp struct{}

// Name returns "cmp".
func (Cmp) Name() string { return "cmp" }

// Check accepts comparisons with literals of t, on the types the doc
// comment of Cmp lists.
func (Cmp) Check(t types.Type, arg string) error {
	args, err := argTokens(arg)
	if err != nil {
		return err
	}

	if err := comparisons(args); err != nil {
		return argsError(err)
	}

	info := basicInfo(t)

	switch {
	case info&types.IsOrdered != 0:
	case info&types.IsBoolean != 0:
		for _, kv := range args {
			if op, _, _ := comparison(kv); op != "==" && op != "!=" {
				return fmt.Errorf("%s is a bool type, which takes == and != only; got %s", t, op)
			}
		}
	default:
		return fmt.Errorf("%s is not an integer, float, string or bool type", t)
	}

	for _, kv := range args {
		_, lit, _ := comparison(kv)
		if _, err := literalValue(t, lit); err != nil {
			return err
		}
	}

	return nil
}

// Emit calls kamaValidateCmp, or kamaValidateEqual for a bool, once per
// comparison.
func (Cmp) Emit(e Emitter, t types.Type, v string, arg string) (string, error) {
	args, err := argTokens(arg)
	if err != nil {
		return "", err
	}

	helper := cmpName
	if basicInfo(t)&types.IsBoolean != 0 {
		helper = equalName
		e.Declare(equalName, equalDecl)
		e.Declare(spellName, spellDecl)
	} else {
		e.Declare(compareName, compareDecl)
		e.Declare(cmpName, cmpDecl)
		e.Declare(spellName, spellDecl)
	}

	return joinCalls(e, args, func(op, lit string) string {
		return helper + "(" + v + ", " + strconv.Quote(op) + ", " + lit + ")"
	})
}

// joinCalls returns call of the comparison of each arg. It passes several
// calls to kamaValidateAll with the stop flag of e. kamaValidateAll returns
// the first error when the flag is true and all of them joined otherwise.
func joinCalls(
	e Emitter,
	args []dircmnt.KeyValue,
	call func(op, operand string) string,
) (string, error) {
	// Check accepted args, so every arg is a comparison.
	toCall := func(kv dircmnt.KeyValue) string {
		op, operand, _ := comparison(kv)
		return call(op, operand)
	}

	calls := opiter.WrapSeq(slices.Values(args)).
		Map(toCall).
		CollectSlice()

	if len(calls) == 1 {
		return calls[0], nil
	}

	e.Declare(allName, allDecl)

	return allName + "(" + e.StopOnFirstError() + ", " + strings.Join(calls, ", ") + ")", nil
}

// Const is the validator //kama:validate const="<v1> <v2> ...": the value
// equals one of the listed values, as in const="1 2 3" or
// const=`"red" "dark blue"`.
//
// It applies to types of a bool, numeric or string underlying type. Each
// token of the argument is a Go literal of the type. A string type takes
// quoted strings only, and any other type takes bare literals only, so
// that const=`"1"` and const=1 do not mean the same.
type Const struct{}

// Name returns "const".
func (Const) Name() string { return "const" }

// Check accepts one or more values of t, on the types the doc comment of
// Const lists.
func (Const) Check(t types.Type, arg string) error {
	args, err := argTokens(arg)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return argsError(errors.New("want one or more values"))
	}

	for _, kv := range args {
		if kv.HasValue {
			return argsError(fmt.Errorf(
				"arg %q is a key=value pair; quote a value holding =",
				spellArgs([]dircmnt.KeyValue{kv}),
			))
		}
	}

	info := basicInfo(t)
	if info&(types.IsBoolean|types.IsNumeric|types.IsString) == 0 {
		return fmt.Errorf("%s is not a bool, numeric or string type", t)
	}

	for _, kv := range args {
		if info&types.IsString != 0 && !kv.KeyQuoted {
			return fmt.Errorf(
				"%s is no value of %s; want a quoted string, as in %q", kv.Key, t, kv.Key,
			)
		}

		if _, err := literalValue(t, constLiteral(kv)); err != nil {
			return err
		}
	}

	return nil
}

// constLiteral returns the Go literal kv spells: kv.Key quoted again when
// it was written quoted.
func constLiteral(kv dircmnt.KeyValue) string {
	if kv.KeyQuoted {
		return strconv.Quote(kv.Key)
	}

	return kv.Key
}

// Emit calls kamaValidateConst with the values.
func (Const) Emit(e Emitter, _ types.Type, v string, arg string) (string, error) {
	args, err := argTokens(arg)
	if err != nil {
		return "", err
	}

	e.Declare(constName, constDecl)
	e.Declare(spellName, spellDecl)

	values := opiter.WrapSeq(slices.Values(args)).
		Map(constLiteral).
		CollectSlice()

	return constName + "(" + v + ", " + strings.Join(values, ", ") + ")", nil
}

const (
	compareName = "kamaValidateCompare"
	lengthName  = "kamaValidateLength"
	cmpName     = "kamaValidateCmp"
	equalName   = "kamaValidateEqual"
	constName   = "kamaValidateConst"
	allName     = "kamaValidateAll"
	// spellName is the helper spelling a value in an error message: a
	// string quoted, as %#v would, and a number in decimal, as %#v would
	// not for an unsigned integer.
	spellName = "kamaValidateSpell"
)

func compareDecl(s Spelling) (string, error) {
	c, err := s.Import("cmp")
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateCompare[T %s.Ordered](got T, op string, want T) bool {
	switch op {
	case ">":
		return got > want
	case ">=":
		return got >= want
	case "==":
		return got == want
	case "!=":
		return got != want
	case "<=":
		return got <= want
	case "<":
		return got < want
	}
	return false
}`, c), nil
}

func lengthDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateLength(got int, op string, want int) error {
	if !kamaValidateCompare(got, op, want) {
		return %[1]s.New(%[1]s.ErrLength, "want length %%s %%d, got %%d", op, want, got)
	}
	return nil
}`, v), nil
}

func cmpDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	c, err := s.Import("cmp")
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateCmp[T %[2]s.Ordered](got T, op string, want T) error {
	if !kamaValidateCompare(got, op, want) {
		return %[1]s.New(%[1]s.ErrCompare, "want %%s %%s, got %%s",
			op, kamaValidateSpell(want), kamaValidateSpell(got))
	}
	return nil
}`, v, c), nil
}

func equalDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateEqual[T comparable](got T, op string, want T) error {
	if (got == want) != (op == "==") {
		return %[1]s.New(%[1]s.ErrCompare, "want %%s %%s, got %%s",
			op, kamaValidateSpell(want), kamaValidateSpell(got))
	}
	return nil
}`, v), nil
}

func constDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	sl, err := s.Import("slices")
	if err != nil {
		return "", err
	}

	str, err := s.Import("strings")
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateConst[T comparable](got T, want ...T) error {
	if !%[2]s.Contains(want, got) {
		spelled := make([]string, len(want))
		for i, w := range want {
			spelled[i] = kamaValidateSpell(w)
		}
		return %[1]s.New(%[1]s.ErrConst, "want one of %%s, got %%s",
			%[3]s.Join(spelled, ", "), kamaValidateSpell(got))
	}
	return nil
}`, v, sl, str), nil
}

func allDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateAll(stopOnFirstError bool, errs ...error) error {
	if !stopOnFirstError {
		return %[1]s.Join(errs...)
	}
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}`, v), nil
}

func spellDecl(s Spelling) (string, error) {
	pkgs := map[string]string{}

	for _, p := range []string{"fmt", "reflect", "strconv"} {
		ident, err := s.Import(p)
		if err != nil {
			return "", err
		}

		pkgs[p] = ident
	}

	return fmt.Sprintf(`func kamaValidateSpell(v any) string {
	if rv := %[2]s.ValueOf(v); rv.Kind() == %[2]s.String {
		return %[3]s.Quote(rv.String())
	}
	return %[1]s.Sprint(v)
}`, pkgs["fmt"], pkgs["reflect"], pkgs["strconv"]), nil
}
