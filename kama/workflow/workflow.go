// Package workflow runs an ordered list of nodes that exchange typed values.
//
// A node declares the value types it requires and the value types it
// provides. The pipeline resolves each required type to the nearest earlier
// node providing it, or to the type's default node when there is none. Nodes
// run in list order.
//
// Every provided value carries a version. A node whose last run consumed an
// older version of some input is run again when its value is needed, and the
// nodes derived from it follow. The target filesystem is itself a value: a
// node that writes generated output into it provides it again, so the nodes
// that read the filesystem rerun on demand and see the generated code.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
)

var (
	ErrNoProvider     = errors.New("workflow: no provider")
	ErrNotDeclared    = errors.New("workflow: type not declared")
	ErrMissingOutput  = errors.New("workflow: declared output not set")
	ErrNotInRequires  = errors.New("workflow: type not in Requires")
	ErrDuplicateInput = errors.New("workflow: type required twice")
)

// Value is what flows through the pipeline. Its type is its identity, so two
// values with the same shape but different roles are two named types. A value
// type knows how to load itself from the project when no node provides it.
type Value interface {
	// DefaultNode returns a node providing this value for a pipeline that
	// has none, or nil when it must be provided explicitly. It is called on
	// the zero value, so a pointer type must handle a nil receiver.
	DefaultNode() Node
}

// KeyRef is the untyped view of a Key, used in declarations.
type KeyRef interface {
	Type() reflect.Type
	DefaultNode() Node
}

// Key names a value type in Requires and Provides.
type Key[T Value] struct{}

func (Key[T]) Type() reflect.Type { return reflect.TypeFor[T]() }

func (Key[T]) DefaultNode() Node {
	var zero T
	return zero.DefaultNode()
}

// Node is one unit of work.
type Node interface {
	Name() string
	Requires() []KeyRef
	Provides() []KeyRef
	Run(ctx context.Context, in *Inputs, out *Outputs) error
}

// Inputs holds the values a node required.
type Inputs struct {
	values map[reflect.Type]any
}

// Get returns the value of type T. T must be in the node's Requires.
func (in *Inputs) Get[T Value]() (T, error) {
	var zero T

	v, ok := in.values[reflect.TypeFor[T]()]
	if !ok {
		return zero, fmt.Errorf("%w: %v", ErrNotInRequires, reflect.TypeFor[T]())
	}

	return v.(T), nil
}

// Outputs collects the values a node provides.
type Outputs struct {
	allowed map[reflect.Type]bool
	values  map[reflect.Type]any
}

// Set records v. T must be in the node's Provides.
func (out *Outputs) Set[T Value](v T) error {
	t := reflect.TypeFor[T]()
	if !out.allowed[t] {
		return fmt.Errorf("%w: %v", ErrNotDeclared, t)
	}

	out.values[t] = v
	return nil
}

// Pipeline is a planned list of nodes.
type Pipeline struct {
	nodes []Node
	// providers[i] maps each type node i requires to the index of its
	// provider.
	providers []map[reflect.Type]int
}

// New plans nodes in the given order, inserting default nodes for types that
// have no earlier provider.
func New(nodes ...Node) (*Pipeline, error) {
	nodes = slices.Clone(nodes)

	latest := map[reflect.Type]int{}
	var providers []map[reflect.Type]int

	for i := 0; i < len(nodes); i++ {
		n := nodes[i]
		prov := map[reflect.Type]int{}
		inserted := false

		for _, k := range n.Requires() {
			if _, dup := prov[k.Type()]; dup {
				return nil, fmt.Errorf("%w: %v in %q", ErrDuplicateInput, k.Type(), n.Name())
			}

			if j, ok := latest[k.Type()]; ok {
				prov[k.Type()] = j
				continue
			}

			def := k.DefaultNode()
			if def == nil {
				return nil, fmt.Errorf("%w: %v required by %q", ErrNoProvider, k.Type(), n.Name())
			}

			// The default is planned at this position so its own requirements
			// resolve against the same earlier nodes; node i is revisited after.
			nodes = slices.Insert(nodes, i, def)
			inserted = true
			break
		}

		if inserted {
			i--
			continue
		}

		providers = append(providers, prov)
		for _, k := range n.Provides() {
			latest[k.Type()] = i
		}
	}

	return &Pipeline{nodes: nodes, providers: providers}, nil
}

// Nodes returns the planned order, including inserted defaults. The slice is
// shared with the pipeline; callers must not modify its elements. It is
// clipped, so appending to it allocates instead of writing into the plan.
func (p *Pipeline) Nodes() []Node { return slices.Clip(p.nodes) }

// Run executes the pipeline once.
func (p *Pipeline) Run(ctx context.Context) error {
	r := &run{
		p:       p,
		results: make([]*result, len(p.nodes)),
	}

	for i := range p.nodes {
		if err := r.ensure(ctx, i); err != nil {
			return err
		}
	}

	return nil
}

type versioned struct {
	value   any
	version int
}

type result struct {
	// inputs records the version of each consumed value.
	inputs  map[reflect.Type]int
	outputs map[reflect.Type]versioned
}

type run struct {
	p           *Pipeline
	results     []*result
	nextVersion int
}

// ensure runs node i unless its last result is still valid, refreshing its
// providers first.
func (r *run) ensure(ctx context.Context, i int) error {
	n := r.p.nodes[i]

	for _, k := range n.Requires() {
		if err := r.ensure(ctx, r.p.providers[i][k.Type()]); err != nil {
			return err
		}
	}

	if r.fresh(i) {
		return nil
	}

	return r.exec(ctx, i)
}

func (r *run) fresh(i int) bool {
	res := r.results[i]
	if res == nil {
		return false
	}

	n := r.p.nodes[i]
	for _, k := range n.Requires() {
		cur := r.results[r.p.providers[i][k.Type()]].outputs[k.Type()]
		if cur.version != res.inputs[k.Type()] {
			return false
		}
	}

	return true
}

func (r *run) exec(ctx context.Context, i int) error {
	n := r.p.nodes[i]

	in := &Inputs{values: map[reflect.Type]any{}}
	res := &result{
		inputs:  map[reflect.Type]int{},
		outputs: map[reflect.Type]versioned{},
	}
	for _, k := range n.Requires() {
		v := r.results[r.p.providers[i][k.Type()]].outputs[k.Type()]
		in.values[k.Type()] = v.value
		res.inputs[k.Type()] = v.version
	}

	out := &Outputs{allowed: map[reflect.Type]bool{}, values: map[reflect.Type]any{}}
	for _, k := range n.Provides() {
		out.allowed[k.Type()] = true
	}

	if err := n.Run(ctx, in, out); err != nil {
		return fmt.Errorf("workflow: node %q: %w", n.Name(), err)
	}

	for _, k := range n.Provides() {
		v, ok := out.values[k.Type()]
		if !ok {
			return fmt.Errorf("%w: %v by %q", ErrMissingOutput, k.Type(), n.Name())
		}
		r.nextVersion++
		res.outputs[k.Type()] = versioned{value: v, version: r.nextVersion}
	}

	r.results[i] = res
	return nil
}
