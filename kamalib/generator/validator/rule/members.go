package rule

import (
	"errors"
	"fmt"
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// Members is the validator //kama:validate members="<op><n> ...": the
// number of JSON members a value of a struct holds satisfies every
// comparison, as JSON Schema minProperties and maxProperties count them.
// The operators and n are those of Length.
//
// It is a StructRule, written in the doc comment of a struct type. A field
// of an option type or a nillable type counts when it is present as
// Required tests it: an opt.Option holds a value, an und.Und or an
// elastic.Elastic is defined, and a pointer, slice, map, channel, function
// or interface is not nil. Any other field always counts. The embedded
// fallback field, tagged json:",embed", counts its entries and must be a
// map. A struct inlining an embedded struct is an error, since Members
// does not count the members such a struct adds.
type Members struct{}

// Name returns "members".
func (Members) Name() string { return "members" }

// MembersOptions are the options of //kama:validate members="<op><n> ...".
type MembersOptions struct {
	// Comparisons holds one comparison per token of the arg, in order.
	Comparisons []Comparison
}

// Parse decodes arg into comparisons with non-negative integers, as Length
// does.
func (Members) Parse(arg string, _ Resolver) (Rule, error) {
	cs, err := parseLengths(arg)
	if err != nil {
		return nil, err
	}

	return MembersOptions{Comparisons: cs}, nil
}

// Check rejects every type: members counts the fields of a struct, which
// Struct gives.
func (MembersOptions) Check(types.Type) error { return errMembersStruct }

// Emit fails as Check does.
func (MembersOptions) Emit(Emitter, types.Type, Gen) (Gen, error) {
	return nil, errMembersStruct
}

// Struct returns the rule counting the members of s. The members of s must
// be listed, and its embedded fallback field, if any, must be a map.
func (o MembersOptions) Struct(s Struct) (Rule, error) {
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

	return membersOf{comparisons: o.Comparisons, s: s}, nil
}

var errMembersStruct = errors.New("members counts the members of a struct;" +
	" write it in the doc comment of a struct type")

// membersOf is Members applied to s.
type membersOf struct {
	comparisons []Comparison
	s           Struct
}

// Check accepts any type. Struct checked s already.
func (membersOf) Check(types.Type) error { return nil }

// Emit calls kamaValidateMembers once per comparison with the count of the
// members.
func (m membersOf) Emit(e Emitter, _ types.Type, _ Gen) (Gen, error) {
	count, err := m.count(e)
	if err != nil {
		return nil, err
	}

	e.Declare(compareName, compareDecl)
	e.Declare(membersName, membersDecl)

	return joinCalls(e, m.comparisons, func(c Comparison) Gen {
		return lang.Expr("%s(%s, %s, %s)", membersName, count, strconv.Quote(c.Op), c.Operand)
	}), nil
}

// count returns the expression counting the members of m.s: the present
// fields of an optional state passed to kamaValidateCount, the number of
// the other fields, and the length of the fallback map.
func (m membersOf) count(e Emitter) (Gen, error) {
	names, err := m.s.Members()
	if err != nil {
		return nil, err
	}

	var (
		present []Gen
		always  int
	)

	for _, name := range names {
		t, v, _ := m.s.Member(name)
		if !optionalState(t) {
			always++
			continue
		}

		cond, err := presenceCond(e, t, v)
		if err != nil {
			return nil, err
		}

		present = append(present, cond)
	}

	var terms []Gen

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

// optionalState reports whether a value of t may hold no value: t is an
// option type or nillable.
func optionalState(t types.Type) bool {
	return typequery.IsNamed(t, typequery.OptPath, "Option") ||
		typequery.IsNamed(t, typequery.UndPath, "Und") ||
		typequery.IsNamed(t, typequery.ElasticPath, "Elastic") ||
		nillable(t)
}

const (
	countName   = "kamaValidateCount"
	membersName = "kamaValidateMembers"
)

func countDecl(Importer) Step {
	return lang.Func(
		lang.Expr("func %s(%s ...bool) int", countName, shadow("present")),
		lang.Stmt("%s := 0", shadow("n")),
		lang.For(nil, lang.Expr("_, %s := range %s", shadow("ok"), ref("present")), nil,
			lang.If(lang.Expr("%s", ref("ok")), lang.Stmt("%s++", ref("n")), nil),
		),
		lang.Stmt("return %s", ref("n")),
	)
}

func membersDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

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
