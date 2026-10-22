// Package workflow runs a graph of steps described by a Spec.
//
// A step is a Go function registered under a kind; see Registry for its
// shape. The function's signature declares everything the executor needs, and
// Build checks it against the spec before anything runs.
//
// Inputs are matched to the step's input names by position. A loader's output
// argument is a pointer. Without an explicit output the executor allocates a
// fresh one for the step, its default slot. With output naming another step,
// the loader receives that step's slot and writes into it. A step's value, as
// seen by the steps that list it as input, is the slot it writes into.
//
// Order follows input, output and after edges. A step that reads a slot does
// not wait for other steps writing into the same slot unless it lists them in
// after. Steps with no ordering between them run in name order.
package workflow

import (
	"bytes"
	"context"
	"encoding/json"
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
	ErrParam              = errors.New("workflow: invalid param")
	ErrCycle              = errors.New("workflow: dependency cycle")
)

type planned struct {
	name string
	fn   reflect.Value
	// inputs are the names of the steps owning each input slot.
	inputs []string
	// output is the name of the step owning the output slot; empty for sinks.
	output string
	// slot is the element type of the default slot this step owns, or nil.
	slot  reflect.Type
	param reflect.Value
}

// Plan is a checked, ordered graph ready to run.
type Plan struct {
	steps []planned
	names []string
}

// Build checks s against the functions in reg and orders its steps.
func (s Spec) Build(reg *Registry) (*Plan, error) {
	if s.Version != SpecVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, s.Version)
	}

	fns := map[string]reflect.Value{}
	for name, st := range s.Steps {
		fn, err := resolve(reg, name, st)
		if err != nil {
			return nil, err
		}

		fns[name] = fn
	}

	order, err := sortSteps(s.Steps)
	if err != nil {
		return nil, err
	}

	owner := map[string]string{}
	// slotType maps a slot owner to the pointer type of its slot.
	slotType := map[string]reflect.Type{}
	steps := make([]planned, 0, len(order))

	for _, name := range order {
		st := s.Steps[name]
		fn := fns[name]
		t := fn.Type()
		p := planned{name: name, fn: fn}

		for i, in := range st.Input {
			if s.Steps[in].Sink != "" {
				return nil, fmt.Errorf("%w: %q takes sink %q as input", ErrInvalidStep, name, in)
			}

			o := owner[in]
			if want := t.In(1 + i); !slotType[o].AssignableTo(want) {
				return nil, fmt.Errorf(
					"%w: input %d of %q: %q holds %v, want %v",
					ErrTypeMismatch, i, name, in, slotType[o], want,
				)
			}

			p.inputs = append(p.inputs, o)
		}

		if st.Loader != "" {
			out := t.In(t.NumIn() - 2)

			if st.Output == "" {
				owner[name] = name
				slotType[name] = out
				p.output = name
				p.slot = out.Elem()
			} else {
				if s.Steps[st.Output].Sink != "" {
					return nil, fmt.Errorf(
						"%w: %q outputs to sink %q",
						ErrInvalidStep,
						name,
						st.Output,
					)
				}

				o := owner[st.Output]
				if slotType[o] != out {
					return nil, fmt.Errorf(
						"%w: output of %q: %q holds %v, want %v",
						ErrTypeMismatch, name, st.Output, slotType[o], out,
					)
				}

				owner[name] = o
				p.output = o
			}
		}

		param, err := decodeParam(st.Param, t.In(t.NumIn()-1))
		if err != nil {
			return nil, fmt.Errorf("%w: %q: %w", ErrParam, name, err)
		}
		p.param = param

		steps = append(steps, p)
	}

	return &Plan{steps: steps, names: order}, nil
}

// resolve looks up the function for st and checks its argument count.
func resolve(reg *Registry, name string, st Step) (reflect.Value, error) {
	var (
		fn    reflect.Value
		ok    bool
		fixed int
	)

	switch {
	case st.Loader != "" && st.Sink != "":
		return reflect.Value{}, fmt.Errorf("%w: %q sets both loader and sink", ErrInvalidStep, name)
	case st.Loader != "":
		fn, ok = reg.loaders[st.Loader]
		if !ok {
			return reflect.Value{}, fmt.Errorf(
				"%w: loader %q for %q",
				ErrUnknownKind,
				st.Loader,
				name,
			)
		}
		// ctx, out, param
		fixed = 3
	case st.Sink != "":
		if st.Output != "" {
			return reflect.Value{}, fmt.Errorf("%w: sink %q has an output", ErrInvalidStep, name)
		}

		fn, ok = reg.sinks[st.Sink]
		if !ok {
			return reflect.Value{}, fmt.Errorf("%w: sink %q for %q", ErrUnknownKind, st.Sink, name)
		}
		// ctx, param
		fixed = 2
	default:
		return reflect.Value{}, fmt.Errorf(
			"%w: %q sets neither loader nor sink",
			ErrInvalidStep,
			name,
		)
	}

	if got, want := fn.Type().NumIn()-fixed, len(st.Input); got != want {
		return reflect.Value{}, fmt.Errorf(
			"%w: %q lists %d inputs, function takes %d",
			ErrArity,
			name,
			want,
			got,
		)
	}

	return fn, nil
}

// sortSteps orders steps so each runs after its input, output and after
// steps. Among steps ready at the same time, the smaller name runs first.
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

// decodeParam decodes raw, as produced by a JSON, YAML or TOML decoder, into
// a value of type t. Unknown fields are rejected so a typo in a spec fails at
// build time.
func decodeParam(raw any, t reflect.Type) (reflect.Value, error) {
	v := reflect.New(t)
	if raw == nil {
		return v.Elem(), nil
	}

	// Re-encoding as JSON gives every format one decoding path into P.
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

// Names returns the step names in execution order. The slice is shared with
// the plan; callers must not modify it. It is clipped, so appending to it
// allocates instead of writing into the plan.
func (p *Plan) Names() []string { return slices.Clip(p.names) }

// Run executes every step once, in order.
func (p *Plan) Run(ctx context.Context) error {
	slots := map[string]reflect.Value{}

	for _, st := range p.steps {
		if st.slot != nil {
			slots[st.name] = reflect.New(st.slot)
		}

		args := make([]reflect.Value, 0, len(st.inputs)+3)
		args = append(args, reflect.ValueOf(&ctx).Elem())
		for _, in := range st.inputs {
			args = append(args, slots[in])
		}
		if st.output != "" {
			args = append(args, slots[st.output])
		}
		args = append(args, st.param)

		if err, _ := reflect.TypeAssert[error](st.fn.Call(args)[0]); err != nil {
			return fmt.Errorf("workflow: step %q: %w", st.name, err)
		}
	}

	return nil
}
