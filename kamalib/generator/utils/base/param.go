package base

import (
	"encoding/json/v2"
	"slices"
)

// ParamOf is the partial variant of a param P that needs no generated
// partial type: a step decodes its param onto the default of P, which
// P.Default returns, so a key the step leaves out keeps its default.
// Use ParamOf[P] as the Partial of a Proc or a SourceProc and leave its
// Default nil.
//
// The decoding is that of encoding/json/v2 into an existing value: a key
// replaces a field holding a slice or a scalar, and merges into a field
// holding a struct or a map, so a default map keeps its entries unless the
// step overrides them. P captures unknown keys as any param does, with a
// field Unknown map[string]any `json:",embed"` and an UnknownKeys method,
// which ParamOf reports to workflow.
type ParamOf[P Defaulter[P]] struct {
	value P
	// decoded tells a param a step gave from none, whose value is the
	// default.
	decoded bool
}

// Defaulter is a param type whose Default method, called on the zero
// value, returns the default param.
type Defaulter[P any] interface {
	Default() P
}

// UnmarshalJSON decodes b onto the default of P.
func (p *ParamOf[P]) UnmarshalJSON(b []byte) error {
	var zero P

	v := zero.Default()
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}

	*p = ParamOf[P]{value: v, decoded: true}

	return nil
}

// Merge returns r when a step gave r, and p otherwise.
func (p ParamOf[P]) Merge(r ParamOf[P]) ParamOf[P] {
	if r.decoded {
		return r
	}

	return p
}

// ToValue returns the param: the decoded one, or the default of P.
func (p ParamOf[P]) ToValue() P {
	if p.decoded {
		return p.value
	}

	var zero P

	return zero.Default()
}

// UnknownKeys returns the unknown keys the param reports, when P or *P
// has an UnknownKeys method.
func (p *ParamOf[P]) UnknownKeys() [][]string {
	if u, ok := any(&p.value).(unknownKeyer); ok {
		return slices.Clone(u.UnknownKeys())
	}

	return nil
}

type unknownKeyer interface {
	UnknownKeys() [][]string
}
