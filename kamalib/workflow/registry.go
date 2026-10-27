package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

var (
	ErrInvalidProc   = errors.New("workflow: invalid proc")
	ErrDuplicateKind = errors.New("workflow: duplicate kind")
)

// Proc is a kind of step, registered under a name and instantiated once per
// step that uses it.
//
// An instance returned by BuildContext has two methods besides Run:
//
//	In(in1 I1, ..., inN IN)
//	Out(out1 *O1, ..., outM *OM)
//
// Their argument types equal InTypes and OutTypes, in order. The types are
// concrete per proc, so the methods cannot be part of the Instance interface.
// The executor calls them through reflection before Run: In with the slots
// named by the step's in list, Out with the slots named by its out list. A
// method may be omitted when its type list is empty.
//
// Because InTypes and OutTypes are known before anything runs, graph
// formation checks every edge in the spec against them. A wrong number of
// ins or outs, or a slot whose type does not fit an argument, fails formation
// and no step is executed.
type Proc interface {
	// InTypes returns the argument types of the instance's In method.
	InTypes() []reflect.Type
	// OutTypes returns the argument types of the instance's Out method.
	// Each must be a pointer: for a step with an empty out list, the executor
	// allocates one fresh value per out and passes a pointer to it.
	OutTypes() []reflect.Type
	// BuildContext returns a new instance configured by param.
	//
	// Graph formation calls it with dryRun set, to sanity-check the graph:
	// the instance is inspected for In and Out and dropped without Run. A
	// dry-run build must decode and validate param, but it must not open
	// files, start processes or otherwise touch state outside the process.
	// Checks that need such state belong in the build with dryRun unset,
	// which immediately precedes Run.
	BuildContext(ctx context.Context, param Param, dryRun bool) (Instance, error)
}

// Instance is a configured proc ready to run once. See Proc for the In and
// Out methods it also has.
type Instance interface {
	Run(ctx context.Context) error
}

// Param is a step's raw param, as produced by a JSON, YAML or TOML decoder.
type Param struct {
	raw any
}

func NewParam(raw any) Param { return Param{raw: raw} }

// Decode decodes the param into v. Unknown fields are rejected so a typo in
// a spec fails at graph formation. A missing param leaves v untouched.
func (p Param) Decode(v any) error {
	if p.raw == nil {
		return nil
	}

	// Re-encoding as JSON gives every source format one decoding path.
	b, err := json.Marshal(p.raw)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// Registry maps kinds to procs.
type Registry struct {
	procs map[string]Proc
}

func NewRegistry() *Registry {
	return &Registry{procs: map[string]Proc{}}
}

// Register adds p under kind.
func (r *Registry) Register(kind string, p Proc) error {
	if p == nil {
		return fmt.Errorf("%w: %q is nil", ErrInvalidProc, kind)
	}

	for i, t := range p.InTypes() {
		if t == nil {
			return fmt.Errorf("%w: %q: in %d is nil", ErrInvalidProc, kind, i)
		}
	}

	for i, t := range p.OutTypes() {
		if t == nil || t.Kind() != reflect.Pointer {
			return fmt.Errorf(
				"%w: %q: out %d must be a pointer, got %v",
				ErrInvalidProc,
				kind,
				i,
				t,
			)
		}
	}

	if _, dup := r.procs[kind]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateKind, kind)
	}

	r.procs[kind] = p
	return nil
}

// Lookup returns the proc registered under kind.
func (r *Registry) Lookup(kind string) (Proc, bool) {
	p, ok := r.procs[kind]
	return p, ok
}
