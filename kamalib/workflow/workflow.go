// Package workflow forms a graph of steps from a Spec.
//
// Each step names a registered Proc. Values flow between steps through their
// instances: a step named in another step's in or out list hands over its
// AsInput value. See Proc for the methods an instance has and when the
// executor calls them.
//
// A step also takes optional inputs when its proc implements OptionalTaker.
// Its optional list names steps whose AsInput values reach the proc in a map
// keyed by step name, so the proc takes any number of them. A spec side-loads
// a value this way, such as a text another step computed, into a proc whose
// in and out lists are fixed.
//
// Build decodes every step's param into the proc's ParamType with
// encoding/json/v2. It does so for specs in every format. Param keys match the
// JSON names of the fields case-sensitively.
//
// An unknown key of a spec, of a step or of a param fails nothing. Spec and
// Step capture their unknown keys, and a param type tells its own by
// implementing UnknownKeyer. Graph.Warnings returns a warning wrapping
// ErrUnknownKey for each of them, naming its key path from the top of the
// spec, as in steps.generate.param.merge. A param type that does not
// implement UnknownKeyer drops its unknown keys without a warning.
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
// Order follows in, out, optional and after edges. A step that reads a value does not
// wait for other steps writing into the same value unless it lists them in
// after. Steps with no ordering between them come in name order.
package workflow

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

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
	// ErrUnknownKey is wrapped by the warnings of Graph.Warnings.
	ErrUnknownKey = errors.New("workflow: unknown key")
)

var errorType = reflect.TypeFor[error]()

// Node is a step in a formed graph.
type Node struct {
	Name string
	Kind string
	Proc Proc
	// Param is the step's param decoded into the proc's ParamType. It is the
	// zero Value when the proc takes no param.
	Param    reflect.Value
	In       []string
	Out      []string
	Optional []string
	After    []string
}

// Graph is a formed, checked graph in execution order.
type Graph struct {
	nodes []Node
	// warnings are the unknown keys of the spec and of the params, which
	// the spec no longer holds once the params are decoded.
	warnings []error
}

// Nodes returns the nodes in execution order. The slice is shared with the
// graph; callers must not modify it. It is clipped, so appending to it
// allocates instead of writing into the graph.
func (g *Graph) Nodes() []Node { return slices.Clip(g.nodes) }

// Warnings returns what Build found in the spec without failing: a warning
// wrapping ErrUnknownKey for each unknown key, those of the spec and its
// steps first and then those of the params in execution order. Run does
// not report them. The slice is shared and clipped as for Nodes.
func (g *Graph) Warnings() []error { return slices.Clip(g.warnings) }

// UnknownKeyer is implemented by param types, or their pointer types, that
// capture the keys of a param they do not know, as a field
// Unknown map[string]any `json:",embed"` does.
type UnknownKeyer interface {
	// UnknownKeys returns the key paths of the unknown keys, relative to the
	// param. They include the unknown keys of nested tables.
	UnknownKeys() [][]string
}

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

	if err := checkOptionalNames(s.Steps); err != nil {
		return nil, err
	}

	order, err := sortSteps(s.Steps)
	if err != nil {
		return nil, err
	}

	g := &Graph{
		warnings: opiter.SlicesValues(s.unknownKeys()).
			Map(unknownKey).
			CollectSlice(),
	}

	for _, name := range order {
		st := s.Steps[name]
		p := procs[name]

		if err := matchRefs(name, "in", st.In, p.InTypes(), procs); err != nil {
			return nil, err
		}

		if err := matchRefs(name, "out", st.Out, p.OutTypes(), procs); err != nil {
			return nil, err
		}

		if err := matchOptional(name, st.Optional, p, procs); err != nil {
			return nil, err
		}

		g.nodes = append(g.nodes, Node{
			Name:     name,
			Kind:     st.Proc,
			Proc:     p,
			In:       st.In,
			Out:      st.Out,
			Optional: st.Optional,
			After:    st.After,
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

	foundIn := opiter.SlicesValues(p.InTypes()).
		FindFunc(isNilType)

	if foundIn.IsSome() {
		return fmt.Errorf("%w: %q: in %d is nil", ErrInvalidProc, kind, foundIn.Value().K)
	}

	foundOut := opiter.SlicesValues(p.OutTypes()).
		FindFunc(isNilType)

	if foundOut.IsSome() {
		return fmt.Errorf("%w: %q: out %d is nil", ErrInvalidProc, kind, foundOut.Value().K)
	}

	return nil
}

// checkOptionalNames checks the optional lists of steps against the other
// lists of their steps, before sortSteps would take a step naming itself
// for a cycle. It checks the steps in name order.
func checkOptionalNames(steps map[string]Step) error {
	for name, st := range opiter.MapsSorted(steps) {
		seen := map[string]bool{}

		for _, r := range st.Optional {
			switch {
			case r == name:
				return fmt.Errorf("%w: optional of %q names the step itself", ErrInvalidStep, name)
			case seen[r]:
				return fmt.Errorf("%w: optional of %q names %q twice", ErrInvalidStep, name, r)
			case slices.Contains(st.In, r):
				return fmt.Errorf(
					"%w: optional of %q names %q, which its in names too", ErrInvalidStep, name, r,
				)
			case slices.Contains(st.Out, r):
				return fmt.Errorf(
					"%w: optional of %q names %q, which its out names too", ErrInvalidStep, name, r,
				)
			}

			seen[r] = true
		}
	}

	return nil
}

// matchOptional checks that p takes optional inputs when refs names any,
// and that the AsInput value of each step refs names fits its OptionalType.
func matchOptional(name string, refs Names, p Proc, procs map[string]Proc) error {
	if len(refs) == 0 {
		return nil
	}

	want := optionalType(p)
	if want == nil {
		return fmt.Errorf(
			"%w: optional of %q names %d steps, but its proc takes no optional inputs",
			ErrInvalidStep, name, len(refs),
		)
	}

	for _, r := range refs {
		got := procs[r].AsInputType()
		if got == nil {
			return fmt.Errorf(
				"%w: optional of %q names %q, which has no AsInput", ErrInvalidStep, name, r,
			)
		}

		if !got.AssignableTo(want) {
			return fmt.Errorf(
				"%w: optional of %q: %q gives %v, want %v", ErrTypeMismatch, name, r, got, want,
			)
		}
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
// every failing step, and adds the unknown keys of the params to the
// warnings of g.
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

		g.warnings = opiter.SlicesValues(paramUnknownKeys(param)).
			Map(func(k []string) error {
				return unknownKey(slices.Concat([]string{"steps", n.Name, "param"}, k))
			}).
			AppendSeq(g.warnings)

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

// instanceMethods are an instance's reflected Param, In, Out, Optional and
// AsInput. optional is the zero Value when the proc takes no optional
// inputs, and asInput when the proc has no AsInputType.
type instanceMethods struct {
	param    reflect.Value
	in       reflect.Value
	out      reflect.Value
	optional reflect.Value
	asInput  reflect.Value
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

	if t := optionalType(p); t != nil {
		in := []reflect.Type{reflect.MapOf(reflect.TypeFor[string](), t)}
		if m.optional, err = method(inst, "Optional", in, nil, false); err != nil {
			return instanceMethods{}, err
		}
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
// fields case-sensitively. An unknown key is no error, so a spec written for
// a newer or older version of a proc still decodes; the param captures it
// when t has a fallback for unknown members. A string holding invalid UTF-8
// is an error. The returned value is addressable.
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

// paramUnknownKeys returns the unknown keys param tells through
// UnknownKeyer, sorted. param is the zero Value for a proc taking no param.
func paramUnknownKeys(param reflect.Value) [][]string {
	if !param.IsValid() {
		return nil
	}

	u, ok := reflect.TypeAssert[UnknownKeyer](param.Addr())
	if !ok {
		return nil
	}

	keys := u.UnknownKeys()
	slices.SortFunc(keys, slices.Compare)

	return keys
}

func unknownKey(path []string) error {
	return fmt.Errorf("%w: %s", ErrUnknownKey, strings.Join(path, "."))
}

// sortSteps orders steps so each comes after its in, out, optional and after
// steps.
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

	ready := opiter.MapsAll(pending).
		Filter(func(_ string, pending int) bool { return pending == 0 }).
		OmitL().
		CollectSlice()

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
		stuck := opiter.MapsAll(pending).
			Filter(func(_ string, pending int) bool { return pending > 0 }).
			OmitL().
			Collect(slices.Sorted)

		return nil, fmt.Errorf("%w: among %v", ErrCycle, stuck)
	}

	return order, nil
}
