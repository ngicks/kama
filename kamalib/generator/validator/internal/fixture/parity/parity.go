// Package parity declares the types whose options internal/fixturefile
// gives in option files instead of directives.
package parity

import (
	"errors"

	"github.com/ngicks/option/opt"
)

// ErrNote is the error of the hook of Order.Note.
var ErrNote = errors.New("note too long")

//kama:validate hook=False, members(ge=4)
type Order struct {
	//kama:validate required, regexp("^[A-Z]{3}-[0-9]+$")
	Id string `json:"id"`
	//kama:validate length(ge=1, le=5)
	Lines []Line `json:"lines,omitzero"`
	//kama:validate const("eur", "usd")
	Currency string `json:"currency"`
	//kama:validate hook=True
	Note string `json:"note"`
}

func (o Order) validateNote(bool) error {
	if len(o.Note) > 10 {
		return ErrNote
	}

	return nil
}

//kama:validate hook=False
type Line struct {
	//kama:validate cmp(gt=0, le=100)
	Qty int `json:"qty"`
	//kama:validate format("url")
	Url opt.Option[string] `json:"url"`
	Sku Sku                `json:"sku"`
}

// Sku is a scalar type with validators of its own.
//
//kama:validate regexp("^[A-Z]+$"), runes(le=8)
type Sku string

// Discount holds combinators, a type-level one among them.
//
//kama:validate hook=False, members(0) or members(ge=2)
type Discount struct {
	//kama:validate cmp(ge=10) or const(0), not (const(5) or const(7))
	Percent opt.Option[int] `json:"percent,omitzero"`
	//kama:validate requires("amount") if const("fixed") else oneof(const("percent"), regexp("^p"))
	Kind   string `json:"kind"`
	Amount *int   `json:"amount,omitzero"`
}

// Batch holds orders, which makes it a dependant.
type Batch struct {
	Orders map[string]Order `json:"orders"`
}
