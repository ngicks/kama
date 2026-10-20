// Package workflow runs an ordered list of nodes that exchange typed values.
//
// A node declares the value types it requires and the value types it
// provides. The list order is time: a value seen at some position comes from
// the latest earlier node providing it.
//
// Nodes come in two kinds. An anchored node is an event: it consumes nothing,
// or it provides a type it also requires, as an output node that writes into
// the target filesystem and provides it again does. It runs once, at its own
// position, with the inputs visible there. Every other node is a function of
// its inputs: when a later node needs its value, its inputs are resolved at
// that later position and it runs again if they changed. The target
// filesystem is itself a value, so a node that reads it reruns on demand after
// an output node and sees the generated code.
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
	ErrCycle          = errors.New("workflow: dependency cycle")
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
	// providers lists, per type, the indices of the nodes providing it in
	// ascending order.
	providers map[reflect.Type][]int
	anchored  []bool
}

// New plans nodes in the given order, inserting default nodes for types that
// have no earlier provider.
func New(nodes ...Node) (*Pipeline, error) {
	nodes = slices.Clone(nodes)

	providers := map[reflect.Type][]int{}
	var anchored []bool

	for i := 0; i < len(nodes); i++ {
		n := nodes[i]
		seen := map[reflect.Type]bool{}
		inserted := false

		for _, k := range n.Requires() {
			if seen[k.Type()] {
				return nil, fmt.Errorf("%w: %v in %q", ErrDuplicateInput, k.Type(), n.Name())
			}
			seen[k.Type()] = true

			if len(providers[k.Type()]) > 0 {
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

		anchored = append(anchored, isAnchored(n))
		for _, k := range n.Provides() {
			providers[k.Type()] = append(providers[k.Type()], i)
		}
	}

	return &Pipeline{nodes: nodes, providers: providers, anchored: anchored}, nil
}

// isAnchored reports whether n is an event rather than a function of its
// inputs: it provides nothing, or it provides a type it also requires.
func isAnchored(n Node) bool {
	if len(n.Provides()) == 0 {
		return true
	}

	for _, p := range n.Provides() {
		for _, r := range n.Requires() {
			if p.Type() == r.Type() {
				return true
			}
		}
	}

	return false
}

// provider returns the latest node before position h providing t.
func (p *Pipeline) provider(t reflect.Type, h int) (int, bool) {
	idx := p.providers[t]
	j, _ := slices.BinarySearch(idx, h)
	if j == 0 {
		return 0, false
	}

	return idx[j-1], true
}

// Nodes returns the planned order, including inserted defaults. The slice is
// shared with the pipeline; callers must not modify its elements. It is
// clipped, so appending to it allocates instead of writing into the plan.
func (p *Pipeline) Nodes() []Node { return slices.Clip(p.nodes) }

// Run executes the pipeline once.
func (p *Pipeline) Run(ctx context.Context) error {
	r := &run{
		p:       p,
		results: make([]map[string]*result, len(p.nodes)),
		active:  map[[2]int]bool{},
	}
	for i := range r.results {
		r.results[i] = map[string]*result{}
	}

	for i := range p.nodes {
		if _, err := r.eval(ctx, i, i); err != nil {
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
	outputs map[reflect.Type]versioned
}

type run struct {
	p *Pipeline
	// results memoizes each node's runs by the versions of its inputs. A
	// reader is evaluated at several positions during one run, before and
	// after an output node, and must not thrash between them.
	results []map[string]*result
	// active holds the (node, position) evaluations in progress. The same
	// node may legitimately be evaluated at two positions at once: an output
	// node resolves its inputs at its own position while a later consumer
	// resolves the same reader after it.
	active      map[[2]int]bool
	nextVersion int
}

// value resolves type t as seen at position h.
func (r *run) value(ctx context.Context, t reflect.Type, h int) (versioned, error) {
	i, ok := r.p.provider(t, h)
	if !ok {
		return versioned{}, fmt.Errorf("%w: %v at %d", ErrNoProvider, t, h)
	}

	if r.p.anchored[i] {
		h = i
	}

	res, err := r.eval(ctx, i, h)
	if err != nil {
		return versioned{}, err
	}

	return res.outputs[t], nil
}

// eval returns node i's result with its inputs resolved at position h,
// running it unless the last result was computed from the same input
// versions.
func (r *run) eval(ctx context.Context, i, h int) (*result, error) {
	n := r.p.nodes[i]

	at := [2]int{i, h}
	if r.active[at] {
		return nil, fmt.Errorf("%w: through %q", ErrCycle, n.Name())
	}
	r.active[at] = true
	defer delete(r.active, at)

	in := &Inputs{values: map[reflect.Type]any{}}
	var key []byte
	for _, k := range n.Requires() {
		v, err := r.value(ctx, k.Type(), h)
		if err != nil {
			return nil, err
		}

		in.values[k.Type()] = v.value
		key = fmt.Appendf(key, "%d,", v.version)
	}

	if res, ok := r.results[i][string(key)]; ok {
		return res, nil
	}

	out := &Outputs{allowed: map[reflect.Type]bool{}, values: map[reflect.Type]any{}}
	for _, k := range n.Provides() {
		out.allowed[k.Type()] = true
	}

	if err := n.Run(ctx, in, out); err != nil {
		return nil, fmt.Errorf("workflow: node %q: %w", n.Name(), err)
	}

	res := &result{outputs: map[reflect.Type]versioned{}}
	for _, k := range n.Provides() {
		v, ok := out.values[k.Type()]
		if !ok {
			return nil, fmt.Errorf("%w: %v by %q", ErrMissingOutput, k.Type(), n.Name())
		}

		r.nextVersion++
		res.outputs[k.Type()] = versioned{value: v, version: r.nextVersion}
	}

	r.results[i][string(key)] = res
	return res, nil
}
