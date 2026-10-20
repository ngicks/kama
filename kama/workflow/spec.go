package workflow

import (
	"errors"
	"fmt"
)

var (
	ErrUnsupportedVersion = errors.New("workflow: unsupported spec version")
	ErrUnknownKind        = errors.New("workflow: unknown node kind")
)

// SpecVersion is the file format version this package reads.
const SpecVersion = 1

// Spec is the serialized form of a graph. Nodes is a slice so the file order
// is the insertion order, which is the execution order.
type Spec struct {
	Version int        `json:"version" yaml:"version" toml:"version"`
	Nodes   []NodeSpec `json:"nodes" yaml:"nodes" toml:"nodes"`
}

// NodeSpec is one node in a Spec.
//
//nolint:lll // triple json/yaml/toml tags; one field per line, never wrap tags
type NodeSpec struct {
	Name string `json:"name" yaml:"name" toml:"name"`
	// Kind selects the constructor in the Registry.
	Kind string   `json:"kind" yaml:"kind" toml:"kind"`
	Deps []string `json:"deps,omitzero" yaml:"deps,omitempty" toml:"deps,omitempty"`
	// With holds kind-specific arguments the constructor decodes.
	With map[string]any `json:"with,omitzero" yaml:"with,omitempty" toml:"with,omitempty"`
}

// Constructor builds a node from its With arguments.
type Constructor func(with map[string]any) (Node, error)

// Registry maps a kind to its constructor.
type Registry map[string]Constructor

// Build constructs the graph a spec describes, resolving kinds through reg.
func (s Spec) Build(reg Registry) (*Graph, error) {
	if s.Version != SpecVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, s.Version)
	}

	g := NewGraph()

	for _, ns := range s.Nodes {
		ctor, ok := reg[ns.Kind]
		if !ok {
			return nil, fmt.Errorf("%w: %q for node %q", ErrUnknownKind, ns.Kind, ns.Name)
		}

		n, err := ctor(ns.With)
		if err != nil {
			return nil, fmt.Errorf("workflow: node %q: %w", ns.Name, err)
		}

		if err := g.Add(ns.Name, n, ns.Deps...); err != nil {
			return nil, err
		}
	}

	return g, nil
}
