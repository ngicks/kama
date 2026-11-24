package rule

import (
	"errors"
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strings"
)

// Validator is one kind of validator //kama:validate can name.
type Validator interface {
	// Name is the word naming the validator in the args of //kama:validate.
	// It must not be hook, ignore, file or package.
	Name() string
	// Parse decodes arg into the rule to apply.
	//
	// arg is the argument of the validator: the value of the arg name=value
	// of //kama:validate, unquoted, the args of the call name(...) as
	// dircmnt.SpellCallArgs spells them, or the args of its option file
	// entry. It is empty when the directive names the validator alone.
	// Validators taking several tokens split arg with
	// dircmnt.ParseArgsKeyValue.
	//
	// r parses other validators through the registry in use. A validator
	// whose arg names validators, as and(...) does, parses them with
	// ParseOperands. Other validators ignore r.
	//
	// An error reports an arg that is wrong whatever the type is. It should
	// wrap ErrArgs.
	Parse(arg string, r Resolver) (Rule, error)
}

// Resolver parses a validator by name, as the registry in use holds it.
// *Registry is a Resolver.
type Resolver interface {
	// Resolve returns the rule of the validator named name parsed with
	// arg. An error of a name it does not know wraps ErrUnknown.
	Resolve(name, arg string) (Rule, error)
}

// ErrUnknown marks the error of Resolver.Resolve for a name the registry
// does not hold. It wraps ErrArgs too.
var ErrUnknown = errors.New("validator: unknown validator")

// ErrArgs marks an error of Validator.Parse, which the argument causes
// for any type. A validator wraps it as fmt.Errorf("%w: ...", ErrArgs) does.
// The generator reports such an error at once. It does not apply the
// validator again to the value a pointer or an option holds. That
// attempt would fail the same way.
var ErrArgs = errors.New("validator: invalid args")

// ValidationPath is the import path of the package of the errors generated
// code returns. A rule imports it through Importer.Import to build them.
const ValidationPath = "github.com/ngicks/kama/kamalib/pkg/validation"

// Rule is a validator with its arg decoded.
//
// The generator applies a rule to the type of the field first. When Check
// rejects it and the field is a pointer, an opt.Option or an und.Und, the
// generator applies it to the value held, under a guard that skips the
// rule when no value is held. A rule in the doc comment of a type applies
// to the value of the type itself.
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
// doc comment of a struct type takes only rules of this kind, and a field
// takes none of them. The generator never calls Check and Emit of the rule
// itself. It applies the rule Struct returns to the struct type and its
// value instead.
type StructRule interface {
	Rule
	// Struct returns the rule validating a value of s, or the error telling
	// why the rule cannot validate s.
	Struct(s Struct) (Rule, error)
}

// SiblingRule is a Rule of a struct field that reads the other fields of
// the struct holding the field. The generator never calls Check and Emit
// of the rule itself. It applies the rule Siblings returns to the field.
// It never applies that rule to the value a pointer or an option of the
// field holds, and it adds no path segment of the field to its errors. The
// errors are located at the struct, and the rule adds the segment of each
// error itself, as validation.Field does.
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

// Registry holds validators by name.
type Registry struct {
	validators map[string]Validator
}

// NewRegistry returns a registry holding vs. A later validator replaces an
// earlier one of the same name.
func NewRegistry(vs ...Validator) *Registry {
	r := &Registry{validators: map[string]Validator{}}
	for _, v := range vs {
		r.Register(v)
	}

	return r
}

// DefaultRegistry returns a new registry holding the built-in validators:
// required, zero, nil, null, regexp, length, runes, cmp, const, multipleof,
// unique, format with DefaultFormats, requires and members, and the
// combinators and, or, not, oneof and if.
func DefaultRegistry() *Registry {
	return NewRegistry(
		Required{},
		Zero{},
		Nil{},
		Null{},
		Regexp{},
		Length{},
		Runes{},
		Cmp{},
		Const{},
		MultipleOf{},
		Unique{},
		Format{Formats: DefaultFormats()},
		Requires{},
		Members{},
		Closed{},
		And{},
		Or{},
		Not{},
		OneOf{},
		If{},
	)
}

// Register adds v, replacing the validator of the same name.
func (r *Registry) Register(v Validator) { r.validators[v.Name()] = v }

// Lookup returns the validator named name.
func (r *Registry) Lookup(name string) (Validator, bool) {
	v, ok := r.validators[name]
	return v, ok
}

// Resolve parses arg with the validator named name. It implements
// Resolver.
func (r *Registry) Resolve(name, arg string) (Rule, error) {
	v, ok := r.Lookup(name)
	if !ok {
		return nil, unknownErr{fmt.Errorf(
			"unknown validator %s; known: %s", name, strings.Join(r.Names(), ", "),
		)}
	}

	return v.Parse(arg, r)
}

// Names returns the names of the validators, sorted.
func (r *Registry) Names() []string { return slices.Sorted(maps.Keys(r.validators)) }

// Clone returns a registry holding the validators of r, which changes to
// either do not affect.
func (r *Registry) Clone() *Registry {
	return &Registry{validators: maps.Clone(r.validators)}
}

// argsError marks err as an error of the args alone. It keeps the message
// of err. The built-in validators word their messages on their own.
func argsError(err error) error { return argsErr{err} }

type argsErr struct{ error }

func (e argsErr) Unwrap() error { return e.error }

func (argsErr) Is(target error) bool { return target == ErrArgs }

type unknownErr struct{ error }

func (e unknownErr) Unwrap() error { return e.error }

func (unknownErr) Is(target error) bool { return target == ErrUnknown || target == ErrArgs }

// noArg reports an argument given to a validator that takes none.
func noArg(arg string) error {
	if arg == "" {
		return nil
	}

	return argsError(fmt.Errorf("unexpected arg %q; want none", arg))
}
