// Package workflow forms a graph of steps from a Spec.
//
// Each step names a registered Proc. Values flow between steps through their
// instances: a step named in another step's in or out list hands over its
// AsInput value. See Proc for the methods an instance has and when the
// executor calls them.
//
// Build decodes every step's param into the proc's ParamType with
// encoding/json/v2. It does so for specs in every format. Param keys match the
// JSON names of the fields case-sensitively. Unknown keys are ignored.
//
// Build forms the graph and checks it without running anything. It matches
// every in and out list against the procs' declared types and then
// instantiates every step with dryRun set, checking its methods and passing
// it its param. A spec naming the wrong steps, or carrying a bad param, fails
// before any step executes.
//
// Graph.Run then executes the steps one at a time and cleans up the procs
// it used. It reports each step's start and end, and the warnings and
// progress the instances report, to a ProgressControl.
//
// Order follows in, out and after edges. A step that reads a value does not
// wait for other steps writing into the same value unless it lists them in
// after. Steps with no ordering between them come in name order.
package workflow

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/ngicks/opiter"
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

	byKind := map[string]Proc{}
	procs := map[string]Proc{}
	for name, st := range s.Steps {
		if st.Proc == "" {
			return nil, fmt.Errorf("%w: %q names no proc", ErrInvalidStep, name)
		}

		p, ok := byKind[st.Proc]
		if !ok {
			newProc, ok := reg.Lookup(st.Proc)
			if !ok {
				return nil, fmt.Errorf("%w: %q for step %q", ErrUnknownKind, st.Proc, name)
			}

			p = newProc()
			if err := checkProc(st.Proc, p); err != nil {
				return nil, err
			}
			byKind[st.Proc] = p
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

// checkProc checks that p, constructed for kind, declares usable types.
func checkProc(kind string, p Proc) error {
	if p == nil {
		return fmt.Errorf("%w: %q constructs nil", ErrInvalidProc, kind)
	}

	foundIn := opiter.WrapSeq(
		slices.Values(p.InTypes()),
	).
		FindFunc(isNilType)
	if foundIn.IsSome() {
		return fmt.Errorf("%w: %q: in %d is nil", ErrInvalidProc, kind, foundIn.Value().K)
	}

	foundOut := opiter.WrapSeq(
		slices.Values(p.OutTypes()),
	).
		FindFunc(isNilType)
	if foundOut.IsSome() {
		return fmt.Errorf("%w: %q: out %d is nil", ErrInvalidProc, kind, foundOut.Value().K)
	}

	return nil
}

func isNilType(t reflect.Type) bool { return t == nil }

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

	m, err := methods(inst, n.Proc)
	if err != nil {
		return err
	}

	var args []reflect.Value
	if n.Param.IsValid() {
		args = []reflect.Value{n.Param}
	}
	if err, _ := reflect.TypeAssert[error](m.param.Call(args)[0]); err != nil {
		return fmt.Errorf("%w: %w", ErrParam, err)
	}

	return nil
}

// instanceMethods are an instance's reflected Param, In, Out and AsInput.
// asInput is the zero Value when the proc has no AsInputType.
type instanceMethods struct {
	param   reflect.Value
	in      reflect.Value
	out     reflect.Value
	asInput reflect.Value
}

// methods looks up inst's methods and checks them against the types p
// declares.
func methods(inst Instance, p Proc) (instanceMethods, error) {
	var paramIn []reflect.Type
	if t := p.ParamType(); t != nil {
		paramIn = []reflect.Type{t}
	}

	var asInputOut []reflect.Type
	if t := p.AsInputType(); t != nil {
		asInputOut = []reflect.Type{t}
	}

	var (
		m   instanceMethods
		err error
	)

	if m.param, err = method(inst, "Param", paramIn, []reflect.Type{errorType}, false); err != nil {
		return instanceMethods{}, err
	}
	if m.in, err = method(inst, "In", p.InTypes(), nil, false); err != nil {
		return instanceMethods{}, err
	}
	if m.out, err = method(inst, "Out", p.OutTypes(), nil, false); err != nil {
		return instanceMethods{}, err
	}
	asInput, err := method(inst, "AsInput", nil, asInputOut, len(asInputOut) == 0)
	if err != nil {
		return instanceMethods{}, err
	}
	if len(asInputOut) > 0 {
		m.asInput = asInput
	}

	return m, nil
}

// method returns inst's method name if its signature takes in and returns
// out. With optional set, the method may be absent, and then the zero Value
// is returned.
func method(
	inst Instance,
	name string,
	in, out []reflect.Type,
	optional bool,
) (reflect.Value, error) {
	m := reflect.ValueOf(inst).MethodByName(name)
	if !m.IsValid() {
		if optional {
			return reflect.Value{}, nil
		}

		return reflect.Value{}, fmt.Errorf("%w: %T has no %s method", ErrInvalidProc, inst, name)
	}

	t := m.Type()
	if !t.IsVariadic() && slices.Equal(slices.Collect(t.Ins()), in) &&
		slices.Equal(slices.Collect(t.Outs()), out) {
		return m, nil
	}

	return reflect.Value{}, fmt.Errorf(
		"%w: %T.%s is %v, want %v",
		ErrInvalidProc, inst, name, t, reflect.FuncOf(in, out, false),
	)
}

// decodeParam decodes raw, as produced by a JSON, YAML or TOML decoder, into
// a value of type t with encoding/json/v2. Keys match the JSON names of the
// fields case-sensitively. Unknown fields are
// ignored, so a spec written for a newer or older version of a proc still
// decodes. A string holding invalid UTF-8 is an error.
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

	if err := json.Unmarshal(b, v.Interface()); err != nil {
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

	ready := opiter.WrapSeq2(
		maps.All(pending),
	).
		Filter(
			func(_ string, pending int) bool { return pending == 0 },
		).
		OmitL().
		Collect()

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
		stuck := slices.Sorted(
			opiter.WrapSeq2(
				maps.All(pending),
			).
				Filter(
					func(_ string, pending int) bool { return pending > 0 },
				).
				OmitL().
				Iter(),
		)

		return nil, fmt.Errorf("%w: among %v", ErrCycle, stuck)
	}

	return order, nil
}
