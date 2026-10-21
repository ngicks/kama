// Package workflow runs a dependency graph of nodes that exchange typed
// values and route generated files to output nodes.
//
// The user names every node and declares its dependencies. A node's required
// value types are resolved among its declared dependencies only, so the graph
// states exactly when expensive work such as loading Go packages happens: a
// second load after generated output is a second node, depending on the node
// that wrote the output. Nothing is reloaded implicitly.
//
// A node that generates files names the output node they go to. The output
// node, typically the overlay filesystem, implicitly depends on every node
// routed to it and receives their files in that order.
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
	ErrNoOutputNode      = errors.New("workflow: no output node declared")
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

// File is generated content routed to an output node. Path is relative to
// the project root.
type File struct {
	Path    string
	Content []byte
}

// Inputs holds the values a node required and the files routed to it.
type Inputs struct {
	values  map[reflect.Type]any
	emitted []File
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

// Emitted returns the files every node routed to this one emitted, in
// dependency order. The slice is shared; callers must not modify it.
func (in *Inputs) Emitted() []File { return slices.Clip(in.emitted) }

// Outputs collects the values a node provides and the files it emits.
type Outputs struct {
	allowed   map[reflect.Type]bool
	values    map[reflect.Type]any
	hasOutput bool
	emitted   []File
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

// Emit routes a generated file to the node's declared output node.
func (out *Outputs) Emit(path string, content []byte) error {
	if !out.hasOutput {
		return fmt.Errorf("%w: emitting %q", ErrNoOutputNode, path)
	}

	out.emitted = append(out.emitted, File{Path: path, Content: content})
	return nil
}

// Edges declares how a node connects to the graph.
type Edges struct {
	// Deps are the nodes whose provided values this node reads.
	Deps []string
	// Output names the node this node's emitted files go to. It may be added
	// later than this node.
	Output string
}

type entry struct {
	name string
	node Node
	// providers maps each required type to the name of the dependency
	// providing it.
	providers map[reflect.Type]string
	output    string
	// sources are the nodes routed to this one, in insertion order.
	sources []string
}

// Graph is a set of named nodes with explicit dependencies.
type Graph struct {
	entries map[string]*entry
	// order is insertion order, which is a topological order because a
	// dependency must be added before the nodes that depend on it.
	order []string
	// pending maps an output node not yet added to the nodes routed to it.
	pending map[string][]string
}

func NewGraph() *Graph {
	return &Graph{entries: map[string]*entry{}, pending: map[string][]string{}}
}

// Add registers node n under name. Its dependencies must already be in the
// graph. Each type n requires must be provided by exactly one dependency; a
// type none of them provides is served by its default node, added under
// name/<type> with the same dependencies. Nodes that declared name as their
// output become dependencies as well, after e.Deps.
func (g *Graph) Add(name string, n Node, e Edges) error {
	if _, dup := g.entries[name]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateNode, name)
	}

	for _, d := range e.Deps {
		if _, ok := g.entries[d]; !ok {
			return fmt.Errorf("%w: %q depends on %q", ErrUnknownNode, name, d)
		}
	}

	if e.Output != "" {
		if _, ok := g.entries[e.Output]; ok {
			return fmt.Errorf(
				"%w: output %q of %q was added earlier",
				ErrUnknownNode,
				e.Output,
				name,
			)
		}
	}

	sources := g.pending[name]
	deps := slices.Concat(e.Deps, sources)

	en := &entry{
		name:      name,
		node:      n,
		providers: map[reflect.Type]string{},
		output:    e.Output,
		sources:   sources,
	}

	for _, k := range n.Requires() {
		t := k.Type()
		if _, dup := en.providers[t]; dup {
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
			if err := g.Add(found, def, Edges{Deps: e.Deps}); err != nil {
				return err
			}
		}

		en.providers[t] = found
	}

	delete(g.pending, name)
	if e.Output != "" {
		g.pending[e.Output] = append(g.pending[e.Output], name)
	}

	g.entries[name] = en
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
	for output, sources := range g.pending {
		return fmt.Errorf("%w: output %q declared by %q", ErrUnknownNode, output, sources[0])
	}

	values := map[string]map[reflect.Type]any{}
	emitted := map[string][]File{}

	for _, name := range g.order {
		e := g.entries[name]

		in := &Inputs{values: map[reflect.Type]any{}}
		for t, p := range e.providers {
			in.values[t] = values[p][t]
		}
		for _, s := range e.sources {
			in.emitted = append(in.emitted, emitted[s]...)
		}

		out := &Outputs{
			allowed:   map[reflect.Type]bool{},
			values:    map[reflect.Type]any{},
			hasOutput: e.output != "",
		}
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

		values[name] = out.values
		emitted[name] = out.emitted
	}

	return nil
}
