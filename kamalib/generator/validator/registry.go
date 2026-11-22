package validator

import (
	"go/types"
	"maps"
	"slices"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// Validator is one kind of field validator a directive can name.
//
// The generator applies a validator to the type of the field first. When
// Check rejects it and the field is a pointer, an opt.Option or an
// und.Und, the generator applies it to the value held, under a guard that
// skips the validator when no value is held.
type Validator interface {
	// Name is the directive name: //kama:validate:<Name>.
	Name() string
	// Check reports why the validator cannot apply to a value of type t with
	// these args. It returns nil when it can. An error the args cause
	// whatever t is should wrap ErrArgs.
	Check(t types.Type, args []dircmnt.KeyValue) error
	// Emit returns the expression of type error that validates the value
	// expression v of type t. It may ask the Emitter for helpers and
	// imports. Emit is called only after Check accepted t and args.
	//
	// The expression may refer to the bool parameter stopOnFirstError of
	// the function it lands in by the name e.StopOnFirstError returns. An
	// expression that can find several errors should return only the first
	// one when that parameter is true.
	Emit(e Emitter, t types.Type, v string, args []dircmnt.KeyValue) (string, error)
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
	// differs from stopOnFirstError when the package declares that name.
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
