package model

import (
	"context"
	"errors"

	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

// errBad is the error the implementors of this file report.
var errBad = errors.New("bad")

// Ptr declares Validate on its pointer receiver.
type Ptr struct{ Bad bool }

// Validate reports errBad when p is bad.
func (p *Ptr) Validate(bool) error {
	if p.Bad {
		return errBad
	}

	return nil
}

// Bare declares Validate taking no argument on its value receiver.
type Bare struct{ Bad bool }

// Validate reports errBad when b is bad.
func (b Bare) Validate() error {
	if b.Bad {
		return errBad
	}

	return nil
}

// Variadic declares Validate taking only a variadic argument on its pointer
// receiver.
type Variadic struct{ Bad bool }

// Validate reports errBad when v is bad.
func (v *Variadic) Validate(...string) error {
	if v.Bad {
		return errBad
	}

	return nil
}

// Ctx declares a Validate generated code cannot call without an argument,
// which makes no implementor.
type Ctx struct{ Bad bool }

// Validate reports errBad when c is bad.
func (c Ctx) Validate(context.Context) error {
	if c.Bad {
		return errBad
	}

	return nil
}

// Implementors holds implementors of every receiver and signature generated
// code calls, in every place the walk reaches.
//
//kama:validate hook=False
type Implementors struct {
	Field    Ptr              `json:"field"`
	Pointer  *Ptr             `json:"pointer"`
	Slice    []Ptr            `json:"slice"`
	Pointers []*Ptr           `json:"pointers"`
	Map      map[string]Ptr   `json:"map"`
	Option   opt.Option[Ptr]  `json:"option"`
	Und      und.Und[Ptr]     `json:"und"`
	Bare     Bare             `json:"bare"`
	Bares    []Bare           `json:"bares"`
	BareMap  map[string]Bare  `json:"bareMap"`
	BareOpt  opt.Option[Bare] `json:"bareOpt"`
	Variadic Variadic         `json:"variadic"`
	VarOpt   opt.Option[*Ptr] `json:"varOpt"`
	Ctx      Ctx              `json:"ctx"`
	Ctxs     []Ctx            `json:"ctxs"`
	//kama:validate iter="All"
	Listed List[Ptr] `json:"listed"`
	//kama:validate nested=False
	Off Bare `json:"off"`
}
