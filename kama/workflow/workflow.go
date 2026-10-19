// Package workflow runs an ordered list of nodes that exchange typed values
// and emit files into a staging area.
//
// A node declares the keys it requires and the keys it provides. The pipeline
// resolves each required key to the nearest earlier node providing it, or to
// the key's default node when there is none. Nodes run in list order.
//
// A node with no required keys is a source: it reads the staged project. Its
// result is memoized against the emit generation, so when a later node needs
// its key after something was emitted, the source runs again and every node
// derived from it follows. This is how generated Go code becomes visible to
// the nodes that run after the one that produced it.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

var (
	ErrNoProvider     = errors.New("workflow: no provider")
	ErrNotDeclared    = errors.New("workflow: key not declared")
	ErrMissingOutput  = errors.New("workflow: declared output not set")
	ErrTypeMismatch   = errors.New("workflow: value type mismatch")
	ErrNotInRequires  = errors.New("workflow: key not in Requires")
	ErrDuplicateInput = errors.New("workflow: key required twice")
)

// KeyRef is the untyped view of a Key, used in declarations.
type KeyRef interface {
	Name() string
	// DefaultNode returns a node providing this key for a pipeline that has
	// none, or nil when the key must be provided explicitly.
	DefaultNode() Node
}

// Key identifies a value of type T. Keys compare by name.
type Key[T any] struct {
	name string
	def  func() Node
}

// NewKey declares a key. def may be nil.
func NewKey[T any](name string, def func() Node) Key[T] {
	return Key[T]{name: name, def: def}
}

func (k Key[T]) Name() string { return k.name }

func (k Key[T]) DefaultNode() Node {
	if k.def == nil {
		return nil
	}
	return k.def()
}

// Emitter receives generated files. The pipeline wraps the one passed to Run
// so it can count emits.
type Emitter interface {
	Emit(ctx context.Context, name string, src []byte) error
}

// Node is one unit of work.
type Node interface {
	Name() string
	Requires() []KeyRef
	Provides() []KeyRef
	Run(ctx context.Context, in *Inputs, out *Outputs, emit Emitter) error
}

// Inputs holds the values a node required.
type Inputs struct {
	values map[string]any
}

// Get returns the value for k. k must be in the node's Requires.
func Get[T any](in *Inputs, k Key[T]) (T, error) {
	var zero T
	v, ok := in.values[k.name]
	if !ok {
		return zero, fmt.Errorf("%w: %q", ErrNotInRequires, k.name)
	}
	t, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("%w: %q holds %T", ErrTypeMismatch, k.name, v)
	}
	return t, nil
}

// Outputs collects the values a node provides.
type Outputs struct {
	allowed map[string]bool
	values  map[string]any
}

// Set records v for k. k must be in the node's Provides.
func Set[T any](out *Outputs, k Key[T], v T) error {
	if !out.allowed[k.name] {
		return fmt.Errorf("%w: %q", ErrNotDeclared, k.name)
	}
	out.values[k.name] = v
	return nil
}

// Pipeline is a planned list of nodes.
type Pipeline struct {
	nodes []Node
	// providers[i] maps each key node i requires to the index of its provider.
	providers []map[string]int
}

// New plans nodes in the given order, inserting default nodes for keys that
// have no earlier provider.
func New(nodes ...Node) (*Pipeline, error) {
	nodes = slices.Clone(nodes)
	latest := map[string]int{}
	var providers []map[string]int
	for i := 0; i < len(nodes); i++ {
		n := nodes[i]
		prov := map[string]int{}
		inserted := false
		for _, k := range n.Requires() {
			if _, dup := prov[k.Name()]; dup {
				return nil, fmt.Errorf("%w: %q in %q", ErrDuplicateInput, k.Name(), n.Name())
			}
			if j, ok := latest[k.Name()]; ok {
				prov[k.Name()] = j
				continue
			}
			def := k.DefaultNode()
			if def == nil {
				return nil, fmt.Errorf("%w: %q required by %q", ErrNoProvider, k.Name(), n.Name())
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
			latest[k.Name()] = i
		}
	}
	return &Pipeline{nodes: nodes, providers: providers}, nil
}

// Nodes returns the planned order, including inserted defaults.
func (p *Pipeline) Nodes() []Node { return slices.Clone(p.nodes) }

// Run executes the pipeline once.
func (p *Pipeline) Run(ctx context.Context, emit Emitter) error {
	r := &run{
		p:       p,
		emit:    &countingEmitter{Emitter: emit},
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
	// gen is the emit generation the node ran at.
	gen int
	// inputs records the version of each consumed value.
	inputs  map[string]int
	outputs map[string]versioned
}

type run struct {
	p           *Pipeline
	emit        *countingEmitter
	results     []*result
	nextVersion int
}

// ensure runs node i unless its last result is still valid, refreshing its
// providers first.
func (r *run) ensure(ctx context.Context, i int) error {
	n := r.p.nodes[i]
	for _, k := range n.Requires() {
		if err := r.ensure(ctx, r.p.providers[i][k.Name()]); err != nil {
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
	if len(n.Requires()) == 0 {
		return res.gen == r.emit.gen
	}
	for _, k := range n.Requires() {
		cur := r.results[r.p.providers[i][k.Name()]].outputs[k.Name()]
		if cur.version != res.inputs[k.Name()] {
			return false
		}
	}
	return true
}

func (r *run) exec(ctx context.Context, i int) error {
	n := r.p.nodes[i]
	in := &Inputs{values: map[string]any{}}
	res := &result{
		gen:     r.emit.gen,
		inputs:  map[string]int{},
		outputs: map[string]versioned{},
	}
	for _, k := range n.Requires() {
		v := r.results[r.p.providers[i][k.Name()]].outputs[k.Name()]
		in.values[k.Name()] = v.value
		res.inputs[k.Name()] = v.version
	}
	out := &Outputs{allowed: map[string]bool{}, values: map[string]any{}}
	for _, k := range n.Provides() {
		out.allowed[k.Name()] = true
	}
	if err := n.Run(ctx, in, out, r.emit); err != nil {
		return fmt.Errorf("workflow: node %q: %w", n.Name(), err)
	}
	for _, k := range n.Provides() {
		v, ok := out.values[k.Name()]
		if !ok {
			return fmt.Errorf("%w: %q by %q", ErrMissingOutput, k.Name(), n.Name())
		}
		r.nextVersion++
		res.outputs[k.Name()] = versioned{value: v, version: r.nextVersion}
	}
	r.results[i] = res
	return nil
}

type countingEmitter struct {
	Emitter
	gen int
}

func (e *countingEmitter) Emit(ctx context.Context, name string, src []byte) error {
	if err := e.Emitter.Emit(ctx, name, src); err != nil {
		return err
	}
	e.gen++
	return nil
}
