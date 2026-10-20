// Package workflow runs a dependency graph of nodes that exchange typed
// values.
//
// The user names every node and declares its dependencies. A node's required
// value types are resolved among its declared dependencies only, so the graph
// states exactly when expensive work such as loading Go packages happens: a
// second load after generated output is a second node, depending on the node
// that wrote the output. Nothing is reloaded implicitly.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
)

var (
	ErrNoProvider        = errors.New("workflow: no provider")
	ErrAmbiguousProvider = errors.New("workflow: ambiguous provider")
	ErrUnknownNode       = errors.New("workflow: unknown node")
	ErrDuplicateNode     = errors.New("workflow: duplicate node")
	ErrNotDeclared       = errors.New("workflow: type not declared")
	ErrMissingOutput     = errors.New("workflow: declared output not set")
	ErrNotInRequires     = errors.New("workflow: type not in Requires")
	ErrDuplicateInput    = errors.New("workflow: type required twice")
)

// Value is what flows through the graph. Its type is its identity, so two
// values with the same shape but different roles are two named types. A value
// type knows how to load itself when no dependency provides it.
type Value interface {
	// DefaultNode returns a node providing this value for a node whose
	// dependencies do not, or nil when it must be provided explicitly. It is
	// called on the zero value, so a pointer type must handle a nil receiver.
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

type entry struct {
	name string
	node Node
	// providers maps each required type to the name of the dependency
	// providing it.
	providers map[reflect.Type]string
}

// Graph is a set of named nodes with explicit dependencies.
type Graph struct {
	entries map[string]*entry
	// order is insertion order, which is a topological order because a
	// dependency must be added before the nodes that depend on it.
	order []string
}

func NewGraph() *Graph {
	return &Graph{entries: map[string]*entry{}}
}

// Add registers node n under name, depending on the named nodes deps, which
// must already be in the graph. Each type n requires must be provided by
// exactly one of deps; a type none of them provides is served by its default
// node, added under name/<type> with the same deps.
func (g *Graph) Add(name string, n Node, deps ...string) error {
	if _, dup := g.entries[name]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateNode, name)
	}

	for _, d := range deps {
		if _, ok := g.entries[d]; !ok {
			return fmt.Errorf("%w: %q depends on %q", ErrUnknownNode, name, d)
		}
	}

	e := &entry{name: name, node: n, providers: map[reflect.Type]string{}}

	for _, k := range n.Requires() {
		t := k.Type()
		if _, dup := e.providers[t]; dup {
			return fmt.Errorf("%w: %v in %q", ErrDuplicateInput, t, name)
		}

		found := ""
		for _, d := range deps {
			if !provides(g.entries[d].node, t) {
				continue
			}
			if found != "" {
				return fmt.Errorf(
					"%w: %v for %q from %q and %q",
					ErrAmbiguousProvider,
					t,
					name,
					found,
					d,
				)
			}
			found = d
		}

		if found == "" {
			def := k.DefaultNode()
			if def == nil {
				return fmt.Errorf("%w: %v required by %q", ErrNoProvider, t, name)
			}

			found = name + "/" + t.String()
			if err := g.Add(found, def, deps...); err != nil {
				return err
			}
		}

		e.providers[t] = found
	}

	g.entries[name] = e
	g.order = append(g.order, name)
	return nil
}

func provides(n Node, t reflect.Type) bool {
	for _, k := range n.Provides() {
		if k.Type() == t {
			return true
		}
	}

	return false
}

// Names returns the node names in execution order, including default nodes
// added by Add. The slice is shared with the graph; callers must not modify
// it. It is clipped, so appending to it allocates instead of writing into
// the graph.
func (g *Graph) Names() []string { return slices.Clip(g.order) }

// Run executes every node once, in dependency order.
func (g *Graph) Run(ctx context.Context) error {
	results := map[string]map[reflect.Type]any{}

	for _, name := range g.order {
		e := g.entries[name]

		in := &Inputs{values: map[reflect.Type]any{}}
		for t, p := range e.providers {
			in.values[t] = results[p][t]
		}

		out := &Outputs{allowed: map[reflect.Type]bool{}, values: map[reflect.Type]any{}}
		for _, k := range e.node.Provides() {
			out.allowed[k.Type()] = true
		}

		if err := e.node.Run(ctx, in, out); err != nil {
			return fmt.Errorf("workflow: node %q: %w", name, err)
		}

		for _, k := range e.node.Provides() {
			if _, ok := out.values[k.Type()]; !ok {
				return fmt.Errorf("%w: %v by %q", ErrMissingOutput, k.Type(), name)
			}
		}

		results[name] = out.values
	}

	return nil
}
