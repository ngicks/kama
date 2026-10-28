// Package workflow forms a graph of steps from a Spec.
//
// Each step names a registered Proc. Values flow between steps through their
// instances: a step named in another step's in or out list hands over its
// AsInput value. See Proc for the methods an instance has and when the
// executor calls them.
//
// Build forms the graph and checks it without running anything. It matches
// every in and out list against the procs' declared types and then
// instantiates every step with dryRun set, checking its methods and passing
// it its param. A spec naming the wrong steps, or carrying a bad param, fails
// before any step executes.
//
// Order follows in, out and after edges. A step that reads a value does not
// wait for other steps writing into the same value unless it lists them in
// after. Steps with no ordering between them come in name order.
package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

var (
	ErrUnsupportedVersion = errors.New("workflow: unsupported spec version")
	ErrInvalidStep        = errors.New("workflow: invalid step")
	ErrUnknownKind        = errors.New("workflow: unknown kind")
	ErrUnknownStep        = errors.New("workflow: unknown step")
	ErrArity              = errors.New("workflow: argument count mismatch")
	ErrTypeMismatch       = errors.New("workflow: type mismatch")
	ErrCycle              = errors.New("workflow: dependency cycle")
	ErrParam              = errors.New("workflow: invalid param")
	ErrInstantiate        = errors.New("workflow: instantiation failed")
)

var errorType = reflect.TypeFor[error]()

// Node is a step in a formed graph.
type Node struct {
	Name string
	Kind string
	Proc Proc
	// Param is the step's param decoded into the proc's ParamType. It is the
	// zero Value when the proc takes no param.
	Param reflect.Value
	In    []string
	Out   []string
	After []string
}

// Graph is a formed, checked graph in execution order.
type Graph struct {
	nodes []Node
}

// Nodes returns the nodes in execution order. The slice is shared with the
// graph; callers must not modify it. It is clipped, so appending to it
// allocates instead of writing into the graph.
func (g *Graph) Nodes() []Node { return slices.Clip(g.nodes) }

// Build forms the graph s describes from the procs in reg and dry-runs every
// step.
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

	for _, name := range order {
		st := s.Steps[name]
		p := procs[name]

		if err := matchRefs(name, "in", st.In, p.InTypes(), procs); err != nil {
			return nil, err
		}
		if err := matchRefs(name, "out", st.Out, p.OutTypes(), procs); err != nil {
			return nil, err
		}

		g.nodes = append(g.nodes, Node{
			Name:  name,
			Kind:  st.Proc,
			Proc:  p,
			In:    st.In,
			Out:   st.Out,
			After: st.After,
		})
	}

	if err := g.dryRun(ctx, s.Steps); err != nil {
		return nil, err
	}

	return g, nil
}

// matchRefs checks that the AsInput values of the steps named in refs fit
// the argument types want, by position.
func matchRefs(name, list string, refs Names, want []reflect.Type, procs map[string]Proc) error {
	if len(refs) != len(want) {
		return fmt.Errorf(
			"%w: %s of %q names %d steps, proc takes %d",
			ErrArity,
			list,
			name,
			len(refs),
			len(want),
		)
	}

	for i, r := range refs {
		got := procs[r].AsInputType()
		if got == nil {
			return fmt.Errorf(
				"%w: %s of %q names %q, which has no AsInput",
				ErrInvalidStep,
				list,
				name,
				r,
			)
		}

		if !got.AssignableTo(want[i]) {
			return fmt.Errorf(
				"%w: %s %d of %q: %q gives %v, want %v",
				ErrTypeMismatch, list, i, name, r, got, want[i],
			)
		}
	}

	return nil
}

// dryRun instantiates every step with dryRun set, checks its methods against
// the proc's declared types and passes it its decoded param. It reports
// every failing step.
func (g *Graph) dryRun(ctx context.Context, steps map[string]Step) error {
	var errs []error

	for i := range g.nodes {
		n := &g.nodes[i]

		param, err := decodeParam(steps[n.Name].Param, n.Proc.ParamType())
		if err != nil {
			errs = append(errs, fmt.Errorf("%w: step %q: %w", ErrParam, n.Name, err))
			continue
		}
		n.Param = param

		if err := checkInstance(ctx, n); err != nil {
			errs = append(errs, fmt.Errorf("step %q: %w", n.Name, err))
		}
	}

	return errors.Join(errs...)
}

func checkInstance(ctx context.Context, n *Node) error {
	inst, err := n.Proc.Instantiate(ctx, true)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInstantiate, err)
	}
	if inst == nil {
		return fmt.Errorf("%w: nil instance", ErrInvalidProc)
	}

	p := n.Proc

	var paramIn []reflect.Type
	if t := p.ParamType(); t != nil {
		paramIn = []reflect.Type{t}
	}

	var asInputOut []reflect.Type
	if t := p.AsInputType(); t != nil {
		asInputOut = []reflect.Type{t}
	}

	param, err := method(inst, "Param", paramIn, nil, []reflect.Type{errorType})
	if err != nil {
		return err
	}
	if _, err := method(inst, "In", p.InTypes(), nil); err != nil {
		return err
	}
	if _, err := method(inst, "Out", p.OutTypes(), nil); err != nil {
		return err
	}
	if _, err := method(inst, "AsInput", nil, asInputOut); err != nil {
		return err
	}

	if param.IsValid() {
		res := param.Call([]reflect.Value{n.Param})
		if len(res) == 1 {
			if err, _ := reflect.TypeAssert[error](res[0]); err != nil {
				return fmt.Errorf("%w: %w", ErrParam, err)
			}
		}
	}

	return nil
}

// method returns inst's method name if its signature takes in and returns
// one of results. A method that takes nothing and returns nothing may be
// absent, and then the zero Value is returned.
func method(
	inst Instance,
	name string,
	in []reflect.Type,
	results ...[]reflect.Type,
) (reflect.Value, error) {
	m := reflect.ValueOf(inst).MethodByName(name)
	if !m.IsValid() {
		if len(in) == 0 &&
			slices.ContainsFunc(results, func(r []reflect.Type) bool { return len(r) == 0 }) {
			return reflect.Value{}, nil
		}

		return reflect.Value{}, fmt.Errorf("%w: %T has no %s method", ErrInvalidProc, inst, name)
	}

	t := m.Type()
	if !t.IsVariadic() && slices.Equal(slices.Collect(t.Ins()), in) &&
		slices.ContainsFunc(
			results,
			func(r []reflect.Type) bool { return slices.Equal(slices.Collect(t.Outs()), r) },
		) {
		return m, nil
	}

	want := make([]string, 0, len(results))
	for _, r := range results {
		want = append(want, reflect.FuncOf(in, r, false).String())
	}

	return reflect.Value{}, fmt.Errorf(
		"%w: %T.%s is %v, want %s",
		ErrInvalidProc, inst, name, t, strings.Join(want, " or "),
	)
}

// decodeParam decodes raw, as produced by a JSON, YAML or TOML decoder, into
// a value of type t. Unknown fields are rejected so a typo in a spec fails
// at graph formation.
func decodeParam(raw any, t reflect.Type) (reflect.Value, error) {
	if t == nil {
		if raw != nil {
			return reflect.Value{}, errors.New("proc takes no param")
		}

		return reflect.Value{}, nil
	}

	v := reflect.New(t)
	if raw == nil {
		return v.Elem(), nil
	}

	// Re-encoding as JSON gives every source format one decoding path.
	b, err := json.Marshal(raw)
	if err != nil {
		return reflect.Value{}, err
	}

	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v.Interface()); err != nil {
		return reflect.Value{}, err
	}

	return v.Elem(), nil
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
