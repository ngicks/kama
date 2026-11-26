package basic

import (
	"errors"
	"fmt"
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Members is the validator //kama:validate members(...): the
// number of JSON members a value of a struct holds satisfies every
// comparison, as JSON Schema minProperties and maxProperties count them.
// The operators and n are those of Length.
//
// It is a StructRule, written in the doc comment of a struct type. A field
// of a type an ext.PresenceTester claims, such as an option type of package
// opt, or of a nillable type, whose json tag holds omitzero or omitempty,
// counts when it is present as Required tests it: the PresenceTester tells
// it holds a value, and a pointer, slice, map, channel, function or
// interface is not nil. Any
// other field always counts, as encoders write it, as null when it holds
// no value. The embedded
// fallback field, tagged json:",embed", counts its entries and must be a
// map. A struct inlining an embedded struct is an error, since Members
// does not count the members such a struct adds.
//
// On raw JSON text, as IsRawJson tells, it applies outside the doc comment
// of a struct type too. It counts the members of an object and passes a
// value of another kind.
type Members struct{}

// Name returns "members".
func (Members) Name() string { return "members" }

// MembersArgs are the args of members(...), which Parse reads and Call spells.
type MembersArgs CompareArgs

// Call returns the call of members spelling a.
func (a MembersArgs) Call() (expr.Call, error) { return compareParams.Call(Members{}.Name(), a) }

// MembersOptions are the options of //kama:validate members(...).
type MembersOptions struct {
	// Comparisons holds one comparison per arg, in order.
	Comparisons []Comparison

	// reg is the registry the options were parsed with, whose type supports
	// tell presence.
	reg ext.Resolver
}

// Parse decodes the args of c into comparisons with non-negative integers.
func (Members) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	cs, err := parseLengths(r, c)
	if err != nil {
		return nil, err
	}

	return MembersOptions{Comparisons: cs, reg: r}, nil
}

// Check accepts raw JSON text only. members counts the fields of a struct
// through Struct.
func (MembersOptions) Check(t types.Type) error {
	if !ext.IsRawJson(t) {
		return errMembersStruct
	}

	return nil
}

// Emit calls rawjson.Members once per comparison.
func (o MembersOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	if !ext.IsRawJson(t) {
		return nil, errMembersStruct
	}

	return rawComparisons(e, "Members", v, o.Comparisons, false), nil
}

// Struct returns the rule counting the members of s. The members of s must
// be listed, and its embedded fallback field, if any, must be a map.
func (o MembersOptions) Struct(s ext.Struct) (ext.Rule, error) {
	if _, err := s.Members(); err != nil {
		return nil, err
	}

	if t, _, ok := s.Fallback(); ok {
		if _, isMap := t.Underlying().(*types.Map); !isMap {
			return nil, fmt.Errorf(
				"the embedded fallback field of type %s is no map, whose entries members counts", t,
			)
		}
	}

	return membersOf{comparisons: o.Comparisons, s: s, reg: o.reg}, nil
}

var errMembersStruct = errors.New("members counts the members of a struct;" +
	" write it in the doc comment of a struct type")

// membersOf is Members applied to s.
type membersOf struct {
	comparisons []Comparison
	s           ext.Struct
	reg         ext.Resolver
}

// Check accepts any type. Struct checked s already.
func (membersOf) Check(types.Type) error { return nil }

// Emit calls kamaValidateMembers once per comparison with the count of the
// members.
func (m membersOf) Emit(e ext.Emitter, _ types.Type, _ ext.Gen) (ext.Gen, error) {
	count, err := m.count(e)
	if err != nil {
		return nil, err
	}

	e.Declare(compareName, compareDecl)
	e.Declare(membersName, membersDecl)

	return joinCalls(e, m.comparisons, func(c Comparison) ext.Gen {
		return lang.Expr("%s(%s, %s, %s)", membersName, count, strconv.Quote(c.Op), c.Operand)
	}), nil
}

// count returns the expression counting the members of m.s: the present
// fields of an optional state passed to kamaValidateCount, the number of
// the other fields, and the length of the fallback map.
func (m membersOf) count(e ext.Emitter) (ext.Gen, error) {
	names, err := m.s.Members()
	if err != nil {
		return nil, err
	}

	var (
		present []ext.Gen
		always  int
	)

	for _, name := range names {
		t, v, _ := m.s.Member(name)
		if !optionalState(m.reg, t) || !m.s.Omittable(name) {
			always++
			continue
		}

		cond, err := presenceCond(e, m.reg, t, v)
		if err != nil {
			return nil, err
		}

		present = append(present, cond)
	}

	var terms []ext.Gen

	if len(present) > 0 {
		e.Declare(countName, countDecl)
		terms = append(terms, lang.Expr("%s(%s)", countName, lang.Join(", ", present...)))
	}

	if always > 0 || len(present) == 0 {
		terms = append(terms, lang.Expr("%d", always))
	}

	if _, v, ok := m.s.Fallback(); ok {
		terms = append(terms, lang.Expr("len(%s)", v))
	}

	return lang.Join(" + ", terms...), nil
}

// optionalState reports whether a value of t may hold no value: an
// ext.PresenceTester of reg claims t, or t is nillable.
func optionalState(reg ext.Resolver, t types.Type) bool {
	_, _, ok := ext.Presence(reg, t, nil)
	return ok || nillable(t)
}

const (
	countName   = "kamaValidateCount"
	membersName = "kamaValidateMembers"
)

func countDecl(ext.Importer) ext.Step {
	return lang.Func(
		lang.Expr("func %s(%s ...bool) int", countName, shadow("present")),
		lang.Stmt("%s := 0", shadow("n")),
		lang.For(nil, lang.Expr("_, %s := range %s", shadow("ok"), ref("present")), nil,
			lang.If(lang.Expr("%s", ref("ok")), lang.Stmt("%s++", ref("n")), nil),
		),
		lang.Stmt("return %s", ref("n")),
	)
}

func membersDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s int, %s string, %s int) error",
			membersName, shadow("got"), shadow("op"), shadow("want")),
		lang.If(
			lang.Expr("!%s(%s, %s, %s)", compareName, ref("got"), ref("op"), ref("want")),
			lang.Stmt(
				`return %s.New(%s.ErrLength, "want member count %%s %%d, got %%d", %s, %s, %s)`,
				v,
				v,
				ref("op"),
				ref("want"),
				ref("got"),
			),
			nil,
		),
		lang.Stmt("return nil"),
	)
}
