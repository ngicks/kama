package workflow

import (
	"encoding/json"
	"slices"
)

// SpecVersion is the file format version this package reads.
const SpecVersion = 1

// Spec is the serialized form of a graph.
type Spec struct {
	Version int             `json:"version" yaml:"version" toml:"version"`
	Steps   map[string]Step `json:"steps" yaml:"steps" toml:"steps"`
}

// Step is one named step in a Spec. Exactly one of Loader and Sink is set.
//
//nolint:lll // triple json/yaml/toml tags; one field per line, never wrap tags
type Step struct {
	// Loader is the kind of a registered loader function.
	Loader string `json:"loader,omitzero" yaml:"loader,omitempty" toml:"loader,omitempty"`
	// Sink is the kind of a registered sink function.
	Sink string `json:"sink,omitzero" yaml:"sink,omitempty" toml:"sink,omitempty"`
	// Input names the steps whose slots are passed as the function's input
	// arguments, by position.
	Input Names `json:"input,omitzero" yaml:"input,omitempty" toml:"input,omitempty"`
	// Output names the step whose slot a loader writes into. Empty means the
	// loader gets a fresh slot of its own.
	Output string `json:"output,omitzero" yaml:"output,omitempty" toml:"output,omitempty"`
	// After names steps that must run first without passing a value.
	After Names `json:"after,omitzero" yaml:"after,omitempty" toml:"after,omitempty"`
	// Param is decoded into the function's param argument.
	Param any `json:"param,omitzero" yaml:"param,omitempty" toml:"param,omitempty"`
}

// deps returns every step s must run after, without duplicates.
func (s Step) deps() []string {
	deps := slices.Concat(s.Input, s.After)
	if s.Output != "" {
		deps = append(deps, s.Output)
	}

	slices.Sort(deps)
	return slices.Compact(deps)
}

// Names is a list of step names. A single name may be written as a scalar.
type Names []string

func (n *Names) UnmarshalJSON(b []byte) error {
	return n.decode(func(v any) error { return json.Unmarshal(b, v) })
}

func (n *Names) UnmarshalYAML(unmarshal func(any) error) error {
	return n.decode(unmarshal)
}

func (n *Names) decode(unmarshal func(any) error) error {
	var one string
	if err := unmarshal(&one); err == nil {
		*n = Names{one}
		return nil
	}

	var many []string
	if err := unmarshal(&many); err != nil {
		return err
	}

	*n = many
	return nil
}
