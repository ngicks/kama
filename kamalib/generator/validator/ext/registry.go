package ext

import (
	"errors"
	"fmt"
	"go/types"
	"iter"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Validator is one kind of validator //kama:validate can name.
type Validator interface {
	// Name is the name of the call naming the validator in the args of
	// //kama:validate. It is a name as expr.IsName reports, and it must not
	// be hook, pre, post, ignore, decoded, file or package.
	Name() string
	// Parse decodes c, a call of the validator, into the rule to apply. c is
	// a bare name, as in required, or a call with args, as in length(ge=1).
	// Params.Bind binds the args of c to the parameters of the validator.
	//
	// r is the resolver in use. A validator whose args are rules, as
	// oneof(...) is, resolves them through Resolve with r. A rule testing
	// the values a type holds keeps r and asks its type supports, through
	// HeldValue, Presence and the other functions taking a Resolver. A
	// validator reports an arg it does not know through r.Warn.
	//
	// An error reports an arg that is wrong whatever the type is. It should
	// wrap ErrArgs.
	Parse(c expr.Call, r Resolver) (Rule, error)
}

// Operator gives the meaning of the expression nodes of one kind other
// than expr.Call, as and gives to expr.And. It returns the rule of n
// resolved with r, or an error that should wrap ErrArgs. A Registry holds
// one operator per kind of node.
type Operator func(n expr.Node, r Resolver) (Rule, error)

// Resolver is the registry in use as a validator sees it: the validators
// by name, the operators by kind of node, the type supports and the sink
// of warnings. *Registry is a Resolver.
type Resolver interface {
	// Lookup returns the validator named name.
	Lookup(name string) (Validator, bool)
	// Operator returns the operator of the kind of n.
	Operator(n expr.Node) (Operator, bool)
	// Names returns the names of the validators, sorted.
	Names() []string
	// TypeSupports returns the type supports in the order they were
	// registered, which is the order they are asked in.
	TypeSupports() iter.Seq[TypeSupport]
	// Warn reports a problem of a directive that does not stop the run, as
	// an arg no parameter takes.
	Warn(w error)
}

// Resolve returns the rule of the node n with r: the validator of the name
// of a call parses it, and the operator of its kind resolves any other
// node. A literal and a name r does not know are errors wrapping
// ErrUnknown.
func Resolve(r Resolver, n expr.Node) (Rule, error) {
	if c, ok := n.(expr.Call); ok {
		v, ok := r.Lookup(c.Name)
		if !ok {
			return nil, unknownErr{fmt.Errorf(
				"unknown validator %s; known: %s", c.Name, strings.Join(r.Names(), ", "),
			)}
		}

		return v.Parse(c, r)
	}

	op, ok := r.Operator(n)
	if !ok {
		return nil, unknownErr{fmt.Errorf("%s is no rule", n)}
	}

	return op(n, r)
}

// WithWarn returns r reporting its warnings to warn. A nil warn drops
// them.
func WithWarn(r Resolver, warn func(w error)) Resolver { return warner{Resolver: r, warn: warn} }

type warner struct {
	Resolver
	warn func(w error)
}

func (w warner) Warn(err error) {
	if w.warn != nil && err != nil {
		w.warn(err)
	}
}

// ErrUnknown marks the error of Resolve for a name the registry does not
// hold and for a node that is no rule. It wraps ErrArgs too.
var ErrUnknown = errors.New("validator: unknown validator")

// ErrArgs marks an error of Validator.Parse, which the argument causes
// for any type. A validator wraps it as fmt.Errorf("%w: ...", ErrArgs) does.
// The generator reports such an error at once. It does not apply the
// validator again to the value HeldValue tells. That
// attempt would fail the same way.
var ErrArgs = errors.New("validator: invalid args")

// ValidationPath is the import path of the package of the errors generated
// code returns. A rule imports it through Importer.Import to build them.
const ValidationPath = "github.com/ngicks/kama/kamaruntime/validator"

// Rule is a validator with its arg decoded.
//
// The generator applies a rule to the type of the field first. When Check
// rejects it and the field holds one value, as HeldValue tells for a
// pointer and a type a Holder claims, the generator applies it to the
// value held, under a guard that skips the rule when no value is held. A
// rule in the doc comment of a type applies to the value of the type
// itself.
type Rule interface {
	// Check reports why the rule cannot apply to a value of type t. It
	// returns nil when it can.
	Check(t types.Type) error
	// Emit returns the expression of type error that validates the value
	// expression v of type t. It may ask the Emitter for helpers and
	// imports. Emit is called only after Check accepted t, while the
	// expression renders.
	//
	// The expression may refer to the bool parameter stopOnFirstError of
	// the function it lands in through e.StopOnFirstError. An expression
	// that can find several errors should return only the first one when
	// that parameter is true.
	Emit(e Emitter, t types.Type, v Gen) (Gen, error)
}

// StructRule is a Rule validating a value of a struct type as a whole. The
// doc comment of a struct type takes only rules of this kind. On a struct
// type the generator never calls Check and Emit of the rule itself. It
// applies the rule Struct returns to the struct type and its value
// instead.
//
// Elsewhere the rule is an ordinary Rule. The doc comment of a type of
// another kind and a field take it when its Check accepts the type, or for
// a field the value HeldValue tells, as
// members accepts raw JSON text. The generator then calls Check and Emit
// of the rule itself. A Check rejecting every type but the struct types
// keeps the rule to the doc comments of struct types.
type StructRule interface {
	Rule
	// Struct returns the rule validating a value of s, or the error telling
	// why the rule cannot validate s.
	Struct(s Struct) (Rule, error)
}

// SiblingRule is a Rule of a struct field that reads the other fields of
// the struct holding the field. The generator never calls Check and Emit
// of the rule itself. It applies the rule Siblings returns to the field.
// It never applies that rule to the value HeldValue tells for the
// field, and it adds no path segment of the field to its errors. The
// errors are located at the struct, and the rule adds the segment of each
// error itself, as validator.Field does.
type SiblingRule interface {
	Rule
	// Siblings returns the rule validating the field of s named field,
	// which reads the other fields of s, or the error telling why the rule
	// cannot read them. field is the name the json tag of the field gives,
	// else the name of the field.
	Siblings(s Struct, field string) (Rule, error)
}

// Struct is the struct type a StructRule validates, or the struct holding
// the field a SiblingRule validates. It names the fields by the JSON
// members encoders write them as.
type Struct interface {
	// Members returns the names of the members the fields encode as, in the
	// order of the fields. A field encodes as a member when it is exported,
	// its json tag is not "-" and encoders do not inline it. The member
	// takes the name of the json tag, else the name of the field. Members
	// reports an error when the struct inlines an embedded struct. It does
	// not list the members such a struct adds.
	Members() ([]string, error)
	// Member returns the type of the field encoding as the member name and
	// the expression of its value, or false when no field Members lists
	// encodes as name.
	Member(name string) (types.Type, Gen, bool)
	// Omittable reports whether encoders may leave out the member name: the
	// json tag of its field holds omitzero or omitempty. Encoders write any
	// other member, as null for a value holding none.
	Omittable(name string) bool
	// Fallback returns the type of the embedded fallback field, a field
	// tagged json:",embed" whose type is no struct, and the expression of
	// its value, or false when the struct has none.
	Fallback() (types.Type, Gen, bool)
}

// Importer spells packages by path in the file generated code lands in.
type Importer interface {
	// Import returns the text of the identifier the file refers to the
	// package at path by. The file imports the package once the text
	// renders.
	Import(path string) Gen
}

// Emitter is what a Rule may use while emitting.
type Emitter interface {
	Importer
	// Declare declares the package-level declaration named name in the
	// shared helper file of the package. decl returns the step rendering
	// it: exactly one function, variable or constant declaration of name,
	// without doc comment, importing packages through imp, the Importer of
	// the helper file. The step renders in a context of its own, whose
	// Package spells types for the helper file. Its locals must not be
	// named like a package it imports. The first decl given for a name
	// wins; later ones are not called. A package-level declaration of that
	// name the generator did not write is an error.
	Declare(name string, decl func(imp Importer) Step)
	// StopOnFirstError returns the text of the name by which the function
	// the expression lands in spells its parameter stopOnFirstError. The
	// name differs from stopOnFirstError when the package or a type
	// parameter of the target declares that name.
	StopOnFirstError() Gen
}

// Annotator is a Rule on raw JSON text that reports the members of an
// object and the elements of an array it evaluated, as JSON Schema
// annotations tell the keywords unevaluatedProperties and
// unevaluatedItems. unevalprops and unevalitems call Annotate on their
// scope, and the combinators pass it on to the operands implementing it.
// A rule that does not implement it evaluates nothing.
type Annotator interface {
	Rule
	// Annotate is Emit that also records, through ev, an expression of type
	// *rawjson.Evaluated of the package at RawJsonPath, what the rule
	// evaluated on v when it holds. ev may be the literal nil, which records
	// nothing.
	Annotate(e Emitter, t types.Type, v, ev Gen) (Gen, error)
}

// Registry holds validators by name.
type Registry struct {
	validators map[string]Validator
	operators  map[reflect.Type]Operator
	supports   []TypeSupport
}

// NewRegistry returns a registry holding vs and no type support. A later
// validator replaces an earlier one of the same name.
func NewRegistry(vs ...Validator) *Registry {
	r := &Registry{validators: map[string]Validator{}, operators: map[reflect.Type]Operator{}}
	for _, v := range vs {
		r.Register(v)
	}

	return r
}

// defaults holds what extension packages register in their init
// functions. Callers get clones of it, so that a change to one registry
// never leaks into another run.
var defaults = NewRegistry()

// Register adds vs to the default registry. It panics when a validator of
// the same name is registered already, since two extension packages then
// claim one name. An extension package calls it in its init function.
func Register(vs ...Validator) {
	for _, v := range vs {
		if _, ok := defaults.Lookup(v.Name()); ok {
			panic(fmt.Errorf("ext: validator %s is registered twice", v.Name()))
		}

		defaults.Register(v)
	}
}

// RegisterOperator adds op to the default registry as the operator of the
// nodes of the kind of kind, the zero value of a node type such as
// expr.And{}. It panics when an operator of that kind is registered
// already. An extension package calls it in its init function.
func RegisterOperator(kind expr.Node, op Operator) {
	if _, ok := defaults.Operator(kind); ok {
		panic(fmt.Errorf("ext: operator of %T is registered twice", kind))
	}

	defaults.RegisterOperator(kind, op)
}

// RegisterTypeSupport adds ss to the default registry. It panics when a
// support of the same name is registered already. An extension package
// calls it in its init function.
func RegisterTypeSupport(ss ...TypeSupport) {
	for _, s := range ss {
		if slices.ContainsFunc(defaults.supports, func(d TypeSupport) bool {
			return d.Name() == s.Name()
		}) {
			panic(fmt.Errorf("ext: type support %s is registered twice", s.Name()))
		}

		defaults.RegisterTypeSupport(s)
	}
}

// DefaultRegistry returns a new registry holding the validators, the
// operators and the type supports registered through Register,
// RegisterOperator and RegisterTypeSupport.
// Package validator imports packages basic and opt for their
// registrations.
func DefaultRegistry() *Registry { return defaults.Clone() }

// Register adds v, replacing the validator of the same name.
func (r *Registry) Register(v Validator) { r.validators[v.Name()] = v }

// RegisterOperator adds op as the operator of the nodes of the kind of
// kind, replacing the operator of that kind.
func (r *Registry) RegisterOperator(kind expr.Node, op Operator) {
	r.operators[reflect.TypeOf(kind)] = op
}

// Operator returns the operator of the kind of n. It implements Resolver.
func (r *Registry) Operator(n expr.Node) (Operator, bool) {
	op, ok := r.operators[reflect.TypeOf(n)]
	return op, ok
}

// Warn drops w. It implements Resolver. WithWarn gives a Resolver that
// reports warnings.
func (*Registry) Warn(error) {}

// RegisterTypeSupport adds s after the supports of r, replacing the
// support of the same name in its place.
func (r *Registry) RegisterTypeSupport(s TypeSupport) {
	i := slices.IndexFunc(r.supports, func(d TypeSupport) bool { return d.Name() == s.Name() })
	if i >= 0 {
		r.supports[i] = s
		return
	}

	r.supports = append(r.supports, s)
}

// TypeSupports returns the type supports of r in the order they were
// registered. It implements Resolver.
func (r *Registry) TypeSupports() iter.Seq[TypeSupport] { return slices.Values(r.supports) }

// Lookup returns the validator named name. It implements Resolver.
func (r *Registry) Lookup(name string) (Validator, bool) {
	v, ok := r.validators[name]
	return v, ok
}

// Resolve returns the rule of n, as the function Resolve does with r.
func (r *Registry) Resolve(n expr.Node) (Rule, error) { return Resolve(r, n) }

// Names returns the names of the validators, sorted. It implements
// Resolver.
func (r *Registry) Names() []string { return slices.Sorted(maps.Keys(r.validators)) }

// Clone returns a registry holding the validators, the operators and the
// type supports of r, which changes to either do not affect.
func (r *Registry) Clone() *Registry {
	return &Registry{
		validators: maps.Clone(r.validators),
		operators:  maps.Clone(r.operators),
		supports:   slices.Clone(r.supports),
	}
}

type unknownErr struct{ error }

func (e unknownErr) Unwrap() error { return e.error }

func (unknownErr) Is(target error) bool { return target == ErrUnknown || target == ErrArgs }
