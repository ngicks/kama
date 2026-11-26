package basic

import (
	"errors"
	"fmt"
	"go/types"
	"slices"
	"strconv"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// Operand is a validator a combinator applies to the value it validates.
type Operand struct {
	// Text spells the operand by what the validator receives, its name and
	// its arg, with the name bare and the value quoted, as in cmp=">=1" or
	// not(required). A directive and an option file writing the same
	// operand, one quoting a value and the other not, give the same Text.
	// The values in the args of a call are quoted too.
	Text string
	// Rule is the operand parsed through the registry in use.
	Rule ext.Rule
}

// ParseOperands parses arg, the args of a combinator as
// dircmnt.ParseCallArgs reads them, into operands. Each arg names a
// validator by its key, which r parses with the arg dircmnt.KeyValue.Arg
// returns: cmp=">=10", not(required) gives cmp with >=10 and not with
// required. A registered validator under the name of a built-in replaces
// the built-in here too.
func ParseOperands(arg string, r ext.Resolver) ([]Operand, error) {
	args, err := dircmnt.ParseCallArgs(arg)
	if err != nil {
		return nil, argsError(err)
	}

	ops := make([]Operand, 0, len(args))

	for _, kv := range args {
		kv.KeyQuoted = false
		text := quoteValues(kv).String()

		rule, err := r.Resolve(kv.Key, kv.Arg())
		if err != nil {
			return nil, fmt.Errorf("operand %s: %w", text, err)
		}

		ops = append(ops, Operand{Text: text, Rule: rule})
	}

	return ops, nil
}

// quoteValues returns kv with every value quoted, those of its call args
// included. A quoted and a bare value read back alike.
func quoteValues(kv dircmnt.KeyValue) dircmnt.KeyValue {
	kv.ValueQuoted = kv.HasValue
	kv.Args = opiter.WrapSeq(slices.Values(kv.Args)).
		Map(quoteValues).
		CollectSlice()

	return kv
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

// combine returns the rule build makes of ops. When an operand is a
// SiblingRule or a StructRule, the combinator is one too, and build runs
// on the operands bound to the struct. src tells how the errors of the
// combinator get the path segment of the field a SiblingRule leaves out. r
// is the registry the operands were parsed with.
func combine(
	r ext.Resolver,
	ops []Operand,
	src errorSource,
	build func(ops []Operand) ext.Rule,
) (ext.Rule, error) {
	siblings := opiter.WrapSeq(slices.Values(ops)).
		Any(func(op Operand) bool { return isSiblingRule(op.Rule) })

	whole := opiter.WrapSeq(slices.Values(ops)).
		Any(func(op Operand) bool { return isStructRule(op.Rule) })

	switch {
	case siblings && whole:
		return nil, argsError(errors.New("operands read the siblings of a struct field and" +
			" validate a struct as a whole; no place takes both"))
	case siblings:
		return siblingCombinator{ops: ops, src: src, build: build, reg: r}, nil
	case whole:
		return structCombinator{ops: ops, build: build}, nil
	}

	return build(ops), nil
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
	ops   []Operand
	src   errorSource
	build func(ops []Operand) ext.Rule
	// reg is the registry the operands were parsed with, whose type
	// supports tell the value a field holds.
	reg ext.Resolver
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
func (c siblingCombinator) Siblings(s ext.Struct, field string) (ext.Rule, error) {
	bound := make([]Operand, 0, len(c.ops))

	for _, op := range c.ops {
		sr, ok := op.Rule.(ext.SiblingRule)

		switch {
		case ok:
			r, err := sr.Siblings(s, field)
			if err != nil {
				return nil, fmt.Errorf("operand %s: %w", op.Text, err)
			}

			op.Rule = r
		case c.src == operandErrors:
			op.Rule = atField{rule: onHeld{rule: op.Rule, reg: c.reg}, field: field}
		default:
			op.Rule = onHeld{rule: op.Rule, reg: c.reg}
		}

		bound = append(bound, op)
	}

	if c.src == ownErrors {
		return atField{rule: c.build(bound), field: field}, nil
	}

	return c.build(bound), nil
}

var errCombinatorSiblings = errors.New("an operand reads the siblings of a struct field;" +
	" write the combinator on a field of a struct")

// structCombinator is a combinator with an operand validating a struct as
// a whole. As a StructRule it is written in the doc comment of a struct
// type.
type structCombinator struct {
	ops   []Operand
	build func(ops []Operand) ext.Rule
}

// Check accepts a type every operand accepts, as raw JSON text, which
// members counts the members of. Any other type needs the struct, which
// Struct gives.
func (c structCombinator) Check(t types.Type) error {
	if err := checkOperands(c.ops, t); err != nil {
		return fmt.Errorf("%w: %w", errCombinatorStruct, err)
	}

	return nil
}

// Emit emits the combinator of the operands unbound.
func (c structCombinator) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	return c.build(c.ops).Emit(e, t, v)
}

// Struct binds each StructRule operand to s.
func (c structCombinator) Struct(s ext.Struct) (ext.Rule, error) {
	bound := make([]Operand, 0, len(c.ops))

	for _, op := range c.ops {
		if sr, ok := op.Rule.(ext.StructRule); ok {
			r, err := sr.Struct(s)
			if err != nil {
				return nil, fmt.Errorf("operand %s: %w", op.Text, err)
			}

			op.Rule = r
		}

		bound = append(bound, op)
	}

	return c.build(bound), nil
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
