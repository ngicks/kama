package rule

import (
	"errors"
	"fmt"
	"go/types"
	"maps"
	"slices"
)

// Validator is one kind of field validator //kama:validate can name.
type Validator interface {
	// Name is the word naming the validator in the args of //kama:validate.
	// It must not be hook, ignore, file or package.
	Name() string
	// Parse decodes arg into the rule to apply.
	//
	// arg is the argument of the validator: the value of the arg name=value
	// of //kama:validate, unquoted, or the args of its option file entry. It
	// is empty when the directive names the validator alone. Validators
	// taking several tokens split arg with dircmnt.ParseArgsKeyValue.
	//
	// An error reports an arg that is wrong whatever the type is. It should
	// wrap ErrArgs.
	Parse(arg string) (Rule, error)
}

// ErrArgs marks an error of Validator.Parse, which the argument causes
// for any type. A validator wraps it as fmt.Errorf("%w: ...", ErrArgs) does.
// The generator reports such an error at once. It does not apply the
// validator again to the value a pointer or an option holds. That
// attempt would fail the same way.
var ErrArgs = errors.New("validator: invalid args")

// ValidationPath is the import path of the package of the errors generated
// code returns. A rule imports it through Spelling.Import to build them.
const ValidationPath = "github.com/ngicks/kama/kamalib/pkg/validation"

// Rule is a validator with its arg decoded.
//
// The generator applies a rule to the type of the field first. When Check
// rejects it and the field is a pointer, an opt.Option or an und.Und, the
// generator applies it to the value held, under a guard that skips the
// rule when no value is held.
type Rule interface {
	// Check reports why the rule cannot apply to a value of type t. It
	// returns nil when it can.
	Check(t types.Type) error
	// Emit returns the expression of type error that validates the value
	// expression v of type t. It may ask the Emitter for helpers and
	// imports. Emit is called only after Check accepted t.
	//
	// The expression may refer to the bool parameter stopOnFirstError of
	// the function it lands in by the name e.StopOnFirstError returns. An
	// expression that can find several errors should return only the first
	// one when that parameter is true.
	Emit(e Emitter, t types.Type, v string) (string, error)
}

// Spelling spells types and packages in the file generated code lands in.
type Spelling interface {
	// TypeString spells t, importing the packages it names.
	TypeString(t types.Type) (string, error)
	// Import returns the identifier the file refers to the package at path
	// by, importing it.
	Import(path string) (string, error)
}

// Emitter is what a Validator may use while emitting.
type Emitter interface {
	Spelling
	// Declare declares the package-level declaration named name in the
	// shared helper file of the package. decl returns its source: exactly
	// one function, variable or constant declaration of name, without doc
	// comment, spelled through the Spelling of the helper file. Its locals
	// must not be named like a package it imports. The first decl given for
	// a name wins; later ones are not called. A package-level declaration of
	// that name the generator did not write is an error.
	Declare(name string, decl func(s Spelling) (string, error))
	// StopOnFirstError returns the name by which the function the
	// expression lands in spells its parameter stopOnFirstError. The name
	// differs from stopOnFirstError when the package or a type parameter of
	// the target declares that name.
	StopOnFirstError() string
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
// required, zero, nil, null, regexp, length, cmp, const and format with
// DefaultFormats.
func DefaultRegistry() *Registry {
	return NewRegistry(
		Required{},
		Zero{},
		Nil{},
		Null{},
		Regexp{},
		Length{},
		Cmp{},
		Const{},
		Format{Formats: DefaultFormats()},
	)
}

// Register adds v, replacing the validator of the same name.
func (r *Registry) Register(v Validator) { r.validators[v.Name()] = v }

// Lookup returns the validator named name.
func (r *Registry) Lookup(name string) (Validator, bool) {
	v, ok := r.validators[name]
	return v, ok
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

// noArg reports an argument given to a validator that takes none.
func noArg(arg string) error {
	if arg == "" {
		return nil
	}

	return argsError(fmt.Errorf("unexpected arg %q; want none", arg))
}
