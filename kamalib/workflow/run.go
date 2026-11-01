package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
)

var ErrRun = errors.New("workflow: step failed")

// Run executes the graph once, one step at a time in execution order.
//
// Each step is instantiated with dryRun unset and has its Param, In, Out,
// Run and AsInput called as Proc describes. Run stops at the first failing
// step and does not start the rest. Once no step is left to run, every proc
// that was instantiated is cleaned up, in reverse order of first use, even
// when a step failed. The returned error joins the step failure with any
// CleanUp errors.
func (g *Graph) Run(ctx context.Context) error {
	values := map[string]reflect.Value{}

	// Procs are tracked by kind: a Proc's dynamic type need not be
	// comparable, so it cannot be a map key itself.
	var (
		used   []Proc
		usedBy = map[string]bool{}
	)

	var runErr error
	for i := range g.nodes {
		n := &g.nodes[i]

		if err := ctx.Err(); err != nil {
			runErr = err
			break
		}

		if !usedBy[n.Kind] {
			usedBy[n.Kind] = true
			used = append(used, n.Proc)
		}

		v, err := runNode(ctx, n, values)
		if err != nil {
			runErr = fmt.Errorf("%w: step %q: %w", ErrRun, n.Name, err)
			break
		}
		if v.IsValid() {
			values[n.Name] = v
		}
	}

	errs := []error{runErr}
	for _, p := range slices.Backward(used) {
		if err := p.CleanUp(ctx); err != nil {
			errs = append(errs, fmt.Errorf("workflow: clean up: %w", err))
		}
	}

	return errors.Join(errs...)
}

// runNode runs one step and returns its AsInput value, or the zero Value
// when the proc has no AsInput.
func runNode(ctx context.Context, n *Node, values map[string]reflect.Value) (reflect.Value, error) {
	inst, err := n.Proc.Instantiate(ctx, false)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("%w: %w", ErrInstantiate, err)
	}
	if inst == nil {
		return reflect.Value{}, fmt.Errorf("%w: nil instance", ErrInvalidProc)
	}

	m, err := methods(inst, n.Proc)
	if err != nil {
		return reflect.Value{}, err
	}

	var args []reflect.Value
	if n.Param.IsValid() {
		args = []reflect.Value{n.Param}
	}
	if err, _ := reflect.TypeAssert[error](m.param.Call(args)[0]); err != nil {
		return reflect.Value{}, fmt.Errorf("%w: %w", ErrParam, err)
	}

	m.in.Call(refValues(n.In, values))
	m.out.Call(refValues(n.Out, values))

	if err := inst.Run(ctx); err != nil {
		return reflect.Value{}, err
	}

	if !m.asInput.IsValid() {
		return reflect.Value{}, nil
	}

	return m.asInput.Call(nil)[0], nil
}

func refValues(refs []string, values map[string]reflect.Value) []reflect.Value {
	out := make([]reflect.Value, len(refs))
	for i, r := range refs {
		out[i] = values[r]
	}

	return out
}
