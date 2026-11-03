package workflow

import (
	"context"
	"errors"
	"reflect"
)

var ErrInvalidProc = errors.New("workflow: invalid proc")

// Proc is a kind of step, registered under a name as a constructor. Build
// constructs one Proc per kind a graph uses, and the Proc instantiates once
// per step of that kind.
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
//   - Run, with a Reporter for the step's warnings and progress.
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
	// fields are ignored. Nil means the proc takes no param.
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
	//
	// The instance's methods must match the types the proc declares. See
	// Instance for the methods of readInstance:
	//
	//	type ReadProc struct{}
	//
	//	func (ReadProc) ParamType() reflect.Type {
	//		return reflect.TypeFor[ReadParam]()
	//	}
	//
	//	func (ReadProc) InTypes() []reflect.Type {
	//		return []reflect.Type{reflect.TypeFor[*Source]()}
	//	}
	//
	//	func (ReadProc) OutTypes() []reflect.Type {
	//		return []reflect.Type{reflect.TypeFor[*Sink]()}
	//	}
	//
	//	func (ReadProc) AsInputType() reflect.Type {
	//		return reflect.TypeFor[*[]byte]()
	//	}
	//
	//	func (ReadProc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	//		return &readInstance{}, nil
	//	}
	Instantiate(ctx context.Context, dryRun bool) (Instance, error)
	// CleanUp releases what the proc holds for its instances once every step
	// is done, such as values handed out through AsInput. An instance's own
	// per-run state is dropped with the instance after its Run.
	CleanUp(ctx context.Context) error
}

// Instance is a configured proc ready to run once. See Proc for the Param,
// In, Out and AsInput methods it also has.
//
// The argument and result types of the concrete methods must match the
// types the proc declares. For ReadProc, shown under Proc.Instantiate:
//
//	func (i *readInstance) Param(p ReadParam) error          // ParamType
//	func (i *readInstance) In(src *Source)                   // InTypes
//	func (i *readInstance) Out(dst *Sink)                    // OutTypes
//	func (i *readInstance) Run(ctx context.Context, r workflow.Reporter) error
//	func (i *readInstance) AsInput() *[]byte                 // AsInputType
//
// With a nil ParamType, Param is Param() error. With empty InTypes or
// OutTypes, In or Out takes no argument.
//
// Run reports through r what the caller should hear about without the step
// failing, such as a skipped input, and how far it has got. r is valid only
// until Run returns.
type Instance interface {
	Run(ctx context.Context, r Reporter) error
}
