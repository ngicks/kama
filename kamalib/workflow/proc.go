package workflow

import (
	"context"
	"errors"
	"reflect"
)

var ErrInvalidProc = errors.New("workflow: invalid proc")

// Proc is a kind of step, registered under a name and instantiated once per
// step that uses it.
//
// An instance returned by Instantiate has, besides Run, four methods whose
// types are concrete per proc, so they cannot be part of the Instance
// interface:
//
//	Param(p P) error
//	In(in1 I1, ..., inN IN)
//	Out(out1 O1, ..., outM OM)
//	AsInput() A
//
// P is ParamType, I1 to IN are InTypes, O1 to OM are OutTypes and A is
// AsInputType. Param, In and Out are mandatory. Param takes no argument when
// ParamType is nil, and In and Out take none when their type lists are
// empty. AsInput may be omitted when AsInputType is nil.
//
// The executor calls these methods through reflection, in this order:
//
//   - Param with the step's param decoded into P.
//   - In with the AsInput values of the steps named in the step's in list.
//   - Out with the AsInput values of the steps named in its out list. The
//     step writes its results into them.
//   - Run.
//   - AsInput, once. The result is passed to every step that names this one
//     in its in or out list. Steps writing into it through Out mutate it, so
//     a value others write into must be a pointer or another reference.
//
// Because the types are known before anything runs, graph formation checks
// every edge in the spec against them. Naming a step that has no AsInput,
// listing a wrong number of ins or outs, or naming a step whose AsInputType
// does not fit an argument fails formation, and no step is executed.
type Proc interface {
	// ParamType returns the type a step's param is decoded into. Unknown
	// fields are rejected. Nil means the proc takes no param.
	ParamType() reflect.Type
	// InTypes returns the argument types of the instance's In method.
	InTypes() []reflect.Type
	// OutTypes returns the argument types of the instance's Out method.
	OutTypes() []reflect.Type
	// AsInputType returns the result type of the instance's AsInput method.
	// Nil means other steps cannot name this one in their in or out lists.
	AsInputType() reflect.Type
	// Instantiate returns a new instance.
	//
	// Graph formation calls it with dryRun set, to sanity-check the graph:
	// the instance's methods are checked against the declared types and its
	// Param is called, then the instance is dropped without Run. A dry-run
	// instance must validate its param in Param, but neither Instantiate nor
	// Param may open files, start processes or otherwise touch state outside
	// the process. Checks that need such state belong in an instance created
	// with dryRun unset, which is the one that runs.
	Instantiate(ctx context.Context, dryRun bool) (Instance, error)
}

// Instance is a configured proc ready to run once. See Proc for the Param,
// In, Out and AsInput methods it also has.
type Instance interface {
	Run(ctx context.Context) error
}
