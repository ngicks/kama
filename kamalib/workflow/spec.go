package workflow

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"

	"github.com/ngicks/opiter"
)

// SpecVersion is the file format version this package reads.
const SpecVersion = 1

// Spec is the serialized form of a graph.
type Spec struct {
	Version int             `json:"version" yaml:"version" toml:"version"`
	Steps   map[string]Step `json:"steps" yaml:"steps" toml:"steps"`
	// Unknown captures the keys of the spec that name no field.
	Unknown map[string]any `json:",embed" yaml:",inline" toml:"-"`
}

// Step is one named step in a Spec.
//
// In and Out list step names. Each name stands for that step's AsInput value,
// and the lists are matched to the proc's InTypes and OutTypes by position.
//
//nolint:lll // triple json/yaml/toml tags; one field per line, never wrap tags
type Step struct {
	// Proc is the kind of a registered Proc.
	Proc string `json:"proc" yaml:"proc" toml:"proc"`
	// In names the steps whose AsInput values are passed to In.
	In Names `json:"in,omitzero" yaml:"in,omitempty" toml:"in,omitempty"`
	// Out names the steps whose AsInput values are passed to Out, for the
	// step to write into.
	Out Names `json:"out,omitzero" yaml:"out,omitempty" toml:"out,omitempty"`
	// After names steps that must run first without passing a value.
	After Names `json:"after,omitzero" yaml:"after,omitempty" toml:"after,omitempty"`
	// Param is decoded into the proc's ParamType and passed to Param.
	Param any `json:"param,omitzero" yaml:"param,omitempty" toml:"param,omitempty"`
	// Unknown captures the keys of the step that name no field.
	Unknown map[string]any `json:",embed" yaml:",inline" toml:"-"`
}

// unknownKeys returns the key paths of the unknown keys of s: those of s
// first, then those of each step in name order.
func (s Spec) unknownKeys() [][]string {
	keys := UnknownKeys(s.Unknown)
	for name, st := range opiter.MapsSorted(s.Steps) {
		keys = append(keys, UnknownKeys(st.Unknown, "steps", name)...)
	}

	return keys
}

// deps returns every step s must run after, without duplicates.
func (s Step) deps() []string {
	deps := slices.Concat(s.In, s.Out, s.After)
	slices.Sort(deps)

	return slices.Compact(deps)
}

// Names is a list of step names. A single name may be written as a scalar.
type Names []string

// UnmarshalJSONFrom decodes a string as a list of one name. A null is no
// names.
func (n *Names) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	switch dec.PeekKind() {
	case 'n':
		if _, err := dec.ReadToken(); err != nil {
			return err
		}

		*n = nil

		return nil
	case '"':
		var one string
		if err := json.UnmarshalDecode(dec, &one); err != nil {
			return err
		}

		*n = Names{one}

		return nil
	default:
		var many []string
		if err := json.UnmarshalDecode(dec, &many); err != nil {
			return err
		}

		*n = many

		return nil
	}
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
