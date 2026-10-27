// Package workflow forms a graph of steps from a Spec.
//
// Each step names a registered Proc. Values flow between steps through
// slots: a step's outs refer to slots it allocates or to slots of the steps
// named in its out list, and a step's ins refer to the slots of the steps
// named in its in list. See Proc for how an instance receives them.
//
// Build forms the graph and checks it without running anything. It matches
// every in and out list against the proc's declared types and then builds
// every instance with dryRun set, so a spec naming the wrong inputs or
// outputs, or carrying a bad param, fails before any step executes.
//
// Order follows in, out and after edges. A step that reads a slot does not
// wait for other steps writing into the same slot unless it lists them in
// after. Steps with no ordering between them come in name order.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
)

var (
	ErrUnsupportedVersion = errors.New("workflow: unsupported spec version")
	ErrInvalidStep        = errors.New("workflow: invalid step")
	ErrUnknownKind        = errors.New("workflow: unknown kind")
	ErrUnknownStep        = errors.New("workflow: unknown step")
	ErrArity              = errors.New("workflow: argument count mismatch")
	ErrTypeMismatch       = errors.New("workflow: type mismatch")
	ErrCycle              = errors.New("workflow: dependency cycle")
	ErrBuild              = errors.New("workflow: build failed")
)

// Slot is a value passed between steps.
type Slot struct {
	// Type is the pointer type of the slot.
	Type reflect.Type
	// Owner is the step that allocates the slot.
	Owner string
}

// Node is a step in a formed graph.
type Node struct {
	Name  string
	Kind  string
	Proc  Proc
	Param Param
	// In and Out are indices into Graph.Slots, in argument order.
	In    []int
	Out   []int
	After []string
}

// Graph is a formed, checked graph in execution order.
type Graph struct {
	nodes []Node
	slots []Slot
}

// Nodes returns the nodes in execution order. The slice is shared with the
// graph; callers must not modify it. It is clipped, so appending to it
// allocates instead of writing into the graph.
func (g *Graph) Nodes() []Node { return slices.Clip(g.nodes) }

// Slots returns every slot, indexed by Node.In and Node.Out. The slice is
// shared and clipped the same way as Nodes.
func (g *Graph) Slots() []Slot { return slices.Clip(g.slots) }

// Build forms the graph s describes from the procs in reg and dry-runs every
// proc's BuildContext.
func (s Spec) Build(ctx context.Context, reg *Registry) (*Graph, error) {
	if s.Version != SpecVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, s.Version)
	}

	procs := map[string]Proc{}
	for name, st := range s.Steps {
		if st.Proc == "" {
			return nil, fmt.Errorf("%w: %q names no proc", ErrInvalidStep, name)
		}

		p, ok := reg.Lookup(st.Proc)
		if !ok {
			return nil, fmt.Errorf("%w: %q for step %q", ErrUnknownKind, st.Proc, name)
		}

		procs[name] = p
	}

	order, err := sortSteps(s.Steps)
	if err != nil {
		return nil, err
	}

	g := &Graph{}
	// refers maps a step to the slots its outs refer to.
	refers := map[string][]int{}

	for _, name := range order {
		st := s.Steps[name]
		p := procs[name]

		in, err := flatten(name, "in", st.In, refers)
		if err != nil {
			return nil, err
		}
		if err := matchIn(name, g.slots, in, p.InTypes()); err != nil {
			return nil, err
		}

		var out []int
		if len(st.Out) == 0 {
			for _, t := range p.OutTypes() {
				out = append(out, len(g.slots))
				g.slots = append(g.slots, Slot{Type: t, Owner: name})
			}
		} else {
			out, err = flatten(name, "out", st.Out, refers)
			if err != nil {
				return nil, err
			}
			if err := matchOut(name, g.slots, out, p.OutTypes()); err != nil {
				return nil, err
			}
		}

		refers[name] = out
		g.nodes = append(g.nodes, Node{
			Name:  name,
			Kind:  st.Proc,
			Proc:  p,
			Param: NewParam(st.Param),
			In:    in,
			Out:   out,
			After: st.After,
		})
	}

	if err := g.dryRun(ctx); err != nil {
		return nil, err
	}

	return g, nil
}

// flatten expands step names into the slots their outs refer to.
func flatten(name, list string, refs Names, refers map[string][]int) ([]int, error) {
	var slots []int

	for _, r := range refs {
		s := refers[r]
		if len(s) == 0 {
			return nil, fmt.Errorf(
				"%w: %s of %q names %q, which has no outs",
				ErrInvalidStep,
				list,
				name,
				r,
			)
		}

		slots = append(slots, s...)
	}

	return slots, nil
}

func matchIn(name string, slots []Slot, in []int, want []reflect.Type) error {
	if len(in) != len(want) {
		return fmt.Errorf(
			"%w: in of %q gives %d slots, proc takes %d",
			ErrArity,
			name,
			len(in),
			len(want),
		)
	}

	for i, id := range in {
		if got := slots[id].Type; !got.AssignableTo(want[i]) {
			return fmt.Errorf(
				"%w: in %d of %q: slot of %q holds %v, want %v",
				ErrTypeMismatch, i, name, slots[id].Owner, got, want[i],
			)
		}
	}

	return nil
}

func matchOut(name string, slots []Slot, out []int, want []reflect.Type) error {
	if len(out) != len(want) {
		return fmt.Errorf(
			"%w: out of %q gives %d slots, proc takes %d",
			ErrArity,
			name,
			len(out),
			len(want),
		)
	}

	for i, id := range out {
		// A step writes into the slot itself, so the type must be exact.
		if got := slots[id].Type; got != want[i] {
			return fmt.Errorf(
				"%w: out %d of %q: slot of %q holds %v, want %v",
				ErrTypeMismatch, i, name, slots[id].Owner, got, want[i],
			)
		}
	}

	return nil
}

// dryRun builds every instance with dryRun set and checks its In and Out
// methods against the proc's declared types. It reports every failing step.
func (g *Graph) dryRun(ctx context.Context) error {
	var errs []error

	for _, n := range g.nodes {
		inst, err := n.Proc.BuildContext(ctx, n.Param, true)
		if err != nil {
			errs = append(errs, fmt.Errorf("%w: step %q: %w", ErrBuild, n.Name, err))
			continue
		}

		if inst == nil {
			errs = append(errs, fmt.Errorf("%w: step %q: nil instance", ErrInvalidProc, n.Name))
			continue
		}

		if err := checkMethod(inst, "In", n.Proc.InTypes()); err != nil {
			errs = append(errs, fmt.Errorf("step %q: %w", n.Name, err))
		}
		if err := checkMethod(inst, "Out", n.Proc.OutTypes()); err != nil {
			errs = append(errs, fmt.Errorf("step %q: %w", n.Name, err))
		}
	}

	return errors.Join(errs...)
}

func checkMethod(inst Instance, name string, want []reflect.Type) error {
	m := reflect.ValueOf(inst).MethodByName(name)
	if !m.IsValid() {
		if len(want) == 0 {
			return nil
		}

		return fmt.Errorf("%w: %T has no %s method", ErrInvalidProc, inst, name)
	}

	t := m.Type()
	if t.IsVariadic() || t.NumOut() != 0 || t.NumIn() != len(want) {
		return fmt.Errorf("%w: %T.%s is %v, want %d arguments and no results",
			ErrInvalidProc, inst, name, t, len(want))
	}

	for i, w := range want {
		if t.In(i) != w {
			return fmt.Errorf("%w: %T.%s argument %d is %v, declared %v",
				ErrInvalidProc, inst, name, i, t.In(i), w)
		}
	}

	return nil
}

// sortSteps orders steps so each comes after its in, out and after steps.
// Among steps ready at the same time, the smaller name comes first.
func sortSteps(steps map[string]Step) ([]string, error) {
	pending := map[string]int{}
	dependents := map[string][]string{}

	for name, st := range steps {
		deps := st.deps()
		for _, d := range deps {
			if _, ok := steps[d]; !ok {
				return nil, fmt.Errorf("%w: %q referenced by %q", ErrUnknownStep, d, name)
			}

			dependents[d] = append(dependents[d], name)
		}

		pending[name] = len(deps)
	}

	var ready []string
	for name, n := range pending {
		if n == 0 {
			ready = append(ready, name)
		}
	}

	order := make([]string, 0, len(steps))
	for len(ready) > 0 {
		slices.Sort(ready)
		name := ready[0]
		ready = ready[1:]
		order = append(order, name)

		for _, d := range dependents[name] {
			pending[d]--
			if pending[d] == 0 {
				ready = append(ready, d)
			}
		}
	}

	if len(order) != len(steps) {
		var stuck []string
		for name, n := range pending {
			if n > 0 {
				stuck = append(stuck, name)
			}
		}
		slices.Sort(stuck)

		return nil, fmt.Errorf("%w: among %v", ErrCycle, stuck)
	}

	return order, nil
}
