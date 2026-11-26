package basic

import (
	"errors"
	"fmt"
	"go/types"
	"slices"
	"strconv"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Operand is a validator a combinator applies to the value it validates.
type Operand struct {
	// Text spells the operand as expr.Print does, as in cmp(ge=1) or
	// not required.
	Text string
	// Rule is the operand resolved through the registry in use.
	Rule ext.Rule
}

// ParseOperands resolves each of nodes, the operands of a combinator,
// through r with ext.Resolve. A registered validator under the name of a
// built-in replaces the built-in here too.
func ParseOperands(nodes []expr.Node, r ext.Resolver) ([]Operand, error) {
	ops := make([]Operand, 0, len(nodes))

	for _, n := range nodes {
		rule, err := ext.Resolve(r, n)
		if err != nil {
			return nil, fmt.Errorf("operand %s: %w", n, err)
		}

		ops = append(ops, Operand{Text: n.String(), Rule: rule})
	}

	return ops, nil
}

// argValues returns the values of args.
func argValues(args []expr.Arg) []expr.Node {
	return opiter.WrapSeq(slices.Values(args)).
		Map(func(a expr.Arg) expr.Node { return a.Value }).
		CollectSlice()
}

// errorSource tells where the errors of a combinator come from.
type errorSource bool

const (
	// operandErrors is the source of a combinator returning the errors of
	// its operands, as and does.
	operandErrors errorSource = false
	// ownErrors is the source of a combinator returning an error of its
	// own, as or does.
	ownErrors errorSource = true
)

// combine returns the rule build makes of the operands nodes resolve to.
// When an operand is a SiblingRule or a StructRule, the combinator is one
// too, and build runs on the operands bound to the struct. src tells how
// the errors of the combinator get the path segment of the field a
// SiblingRule leaves out. r is the registry the operands resolve with.
//
// The rule keeps nodes, not the operands, and resolves them again each
// time it needs them. combine resolves them once to report an error of
// their args and the warnings of r.
func combine(
	r ext.Resolver,
	nodes []expr.Node,
	src errorSource,
	build func(ops []Operand) ext.Rule,
) (ext.Rule, error) {
	ops, err := ParseOperands(nodes, r)
	if err != nil {
		return nil, err
	}

	siblings := opiter.WrapSeq(slices.Values(ops)).
		Any(func(op Operand) bool { return isSiblingRule(op.Rule) })

	whole := opiter.WrapSeq(slices.Values(ops)).
		Any(func(op Operand) bool { return isStructRule(op.Rule) })

	c := combination{nodes: nodes, reg: ext.WithWarn(r, nil), build: build}

	switch {
	case siblings && whole:
		return nil, argsError(errors.New("operands read the siblings of a struct field and" +
			" validate a struct as a whole; no place takes both"))
	case siblings:
		return siblingCombinator{c: c, src: src}, nil
	case whole:
		return structCombinator{c: c}, nil
	}

	return c, nil
}

// combination is a combinator over the operands its nodes resolve to.
type combination struct {
	nodes []expr.Node
	// reg resolves the operands. It drops the warnings combine reported.
	reg   ext.Resolver
	build func(ops []Operand) ext.Rule
}

// rule returns the combinator of the operands resolved, to apply to a
// value of type t. When no single level of t, t itself or a value it holds
// as ext.HeldValue tells, takes every operand, each operand applies to the
// outermost level taking it, as in nil or not cmp(0) on a value of type
// *int. A value holding none passes such an operand. Otherwise the
// combinator applies whole, so that the generator applies it to the level
// taking every operand under one guard. A nil t gives the combinator whole.
func (c combination) rule(t types.Type) (ext.Rule, error) {
	ops, err := ParseOperands(c.nodes, c.reg)
	if err != nil {
		return nil, err
	}

	whole := c.build(ops)
	if t == nil {
		return whole, nil
	}

	checkErr := whole.Check(t)
	if checkErr == nil {
		return whole, nil
	}

	for held, _, _, ok := ext.HeldValue(c.reg, t, nil); ok; held, _, _, ok = ext.HeldValue(
		c.reg, held, nil,
	) {
		if whole.Check(held) == nil {
			return whole, nil
		}
	}

	split := c.build(
		opiter.WrapSeq(slices.Values(ops)).
			Map(func(op Operand) Operand {
				return Operand{Text: op.Text, Rule: onHeld{rule: op.Rule, reg: c.reg}}
			}).
			CollectSlice(),
	)
	if split.Check(t) != nil {
		return whole, nil
	}

	return split, nil
}

// Check accepts the types the combinator accepts.
func (c combination) Check(t types.Type) error {
	r, err := c.rule(t)
	if err != nil {
		return err
	}

	return r.Check(t)
}

// Emit emits the combinator.
func (c combination) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	r, err := c.rule(t)
	if err != nil {
		return nil, err
	}

	return r.Emit(e, t, v)
}

// Annotate annotates with the combinator, or emits it when it records
// nothing.
func (c combination) Annotate(e ext.Emitter, t types.Type, v, ev ext.Gen) (ext.Gen, error) {
	r, err := c.rule(t)
	if err != nil {
		return nil, err
	}

	return annotate(e, r, t, v, ev)
}

func isSiblingRule(r ext.Rule) bool {
	_, ok := r.(ext.SiblingRule)
	return ok
}

func isStructRule(r ext.Rule) bool {
	_, ok := r.(ext.StructRule)
	return ok
}

// checkOperands reports every operand of ops that cannot apply to t.
func checkOperands(ops []Operand, t types.Type) error {
	var errs []error

	for _, op := range ops {
		if err := op.Rule.Check(t); err != nil {
			errs = append(errs, fmt.Errorf("operand %s: %w", op.Text, err))
		}
	}

	return errors.Join(errs...)
}

// emitOperands returns the expression of each operand of ops validating v
// of type t.
func emitOperands(e ext.Emitter, ops []Operand, t types.Type, v ext.Gen) ([]ext.Gen, error) {
	out := make([]ext.Gen, 0, len(ops))

	for _, op := range ops {
		g, err := op.Rule.Emit(e, t, v)
		if err != nil {
			return nil, fmt.Errorf("operand %s: %w", op.Text, err)
		}

		out = append(out, g)
	}

	return out, nil
}

// texts returns the Text of each of ops quoted, as Go string literals.
func texts(ops []Operand) []string {
	return opiter.WrapSeq(slices.Values(ops)).
		Map(func(op Operand) string { return strconv.Quote(op.Text) }).
		CollectSlice()
}

// siblingCombinator is a combinator with an operand reading the siblings
// of the field. As a SiblingRule it applies to the field itself, never to
// the value a pointer or an option holds. Its other operands apply to the
// type of the field, else to the value it holds, and hold when it holds
// none.
type siblingCombinator struct {
	c   combination
	src errorSource
}

// Check rejects every type: an operand needs the struct holding the
// field, which Siblings gives.
func (siblingCombinator) Check(types.Type) error { return errCombinatorSiblings }

// Emit fails as Check does.
func (siblingCombinator) Emit(ext.Emitter, types.Type, ext.Gen) (ext.Gen, error) {
	return nil, errCombinatorSiblings
}

// Siblings binds each SiblingRule operand to s and the field named field.
// The errors such an operand returns are located already. The combinator
// locates the other errors at the field: those of each other operand when
// it returns the errors of its operands, else its own.
func (sc siblingCombinator) Siblings(s ext.Struct, field string) (ext.Rule, error) {
	ops, err := ParseOperands(sc.c.nodes, sc.c.reg)
	if err != nil {
		return nil, err
	}

	bound := make([]Operand, 0, len(ops))

	for _, op := range ops {
		sr, ok := op.Rule.(ext.SiblingRule)

		switch {
		case ok:
			r, err := sr.Siblings(s, field)
			if err != nil {
				return nil, fmt.Errorf("operand %s: %w", op.Text, err)
			}

			op.Rule = r
		case sc.src == operandErrors:
			op.Rule = atField{rule: onHeld{rule: op.Rule, reg: sc.c.reg}, field: field}
		default:
			op.Rule = onHeld{rule: op.Rule, reg: sc.c.reg}
		}

		bound = append(bound, op)
	}

	if sc.src == ownErrors {
		return atField{rule: sc.c.build(bound), field: field}, nil
	}

	return sc.c.build(bound), nil
}

var errCombinatorSiblings = errors.New("an operand reads the siblings of a struct field;" +
	" write the combinator on a field of a struct")

// structCombinator is a combinator with an operand validating a struct as
// a whole. As a StructRule it is written in the doc comment of a struct
// type.
type structCombinator struct{ c combination }

// Check accepts a type every operand accepts, as raw JSON text, which
// members counts the members of. Any other type needs the struct, which
// Struct gives.
func (sc structCombinator) Check(t types.Type) error {
	ops, err := ParseOperands(sc.c.nodes, sc.c.reg)
	if err != nil {
		return err
	}

	if err := checkOperands(ops, t); err != nil {
		return fmt.Errorf("%w: %w", errCombinatorStruct, err)
	}

	return nil
}

// Emit emits the combinator of the operands unbound.
func (sc structCombinator) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	return sc.c.Emit(e, t, v)
}

// Struct binds each StructRule operand to s.
func (sc structCombinator) Struct(s ext.Struct) (ext.Rule, error) {
	ops, err := ParseOperands(sc.c.nodes, sc.c.reg)
	if err != nil {
		return nil, err
	}

	bound := make([]Operand, 0, len(ops))

	for _, op := range ops {
		if sr, ok := op.Rule.(ext.StructRule); ok {
			r, err := sr.Struct(s)
			if err != nil {
				return nil, fmt.Errorf("operand %s: %w", op.Text, err)
			}

			op.Rule = r
		}

		bound = append(bound, op)
	}

	return sc.c.build(bound), nil
}

var errCombinatorStruct = errors.New("an operand validates a struct as a whole;" +
	" write the combinator in the doc comment of a struct type")

// atField is rule with its errors located at the struct field named field.
type atField struct {
	rule  ext.Rule
	field string
}

// Check accepts the types rule accepts.
func (a atField) Check(t types.Type) error { return a.rule.Check(t) }

// Emit wraps the expression of rule with validator.Field.
func (a atField) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	g, err := a.rule.Emit(e, t, v)
	if err != nil {
		return nil, err
	}

	return lang.Expr(
		"%s.Field(%s, %s)",
		e.Import(ext.ValidationPath),
		g,
		strconv.Quote(a.field),
	), nil
}
