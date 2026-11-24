// Package parity declares the types whose options internal/fixturefile
// gives in option files instead of directives.
package parity

import (
	"errors"

	"github.com/ngicks/option/opt"
)

// ErrNote is the error of the hook of Order.Note.
var ErrNote = errors.New("note too long")

//kama:validate hook=off members=">=4"
type Order struct {
	//kama:validate required regexp="^[A-Z]{3}-[0-9]+$"
	Id string `json:"id"`
	//kama:validate length=">=1 <=5"
	Lines []Line `json:"lines"`
	//kama:validate const=`"eur" "usd"`
	Currency string `json:"currency"`
	//kama:validate hook=on
	Note string `json:"note"`
}

func (o Order) validateNote(bool) error {
	if len(o.Note) > 10 {
		return ErrNote
	}

	return nil
}

//kama:validate hook=off
type Line struct {
	//kama:validate cmp=">0 <=100"
	Qty int `json:"qty"`
	//kama:validate format=url
	Url opt.Option[string] `json:"url"`
	Sku Sku                `json:"sku"`
}

// Sku is a scalar type with validators of its own.
//
//kama:validate regexp="^[A-Z]+$" runes="<=8"
type Sku string

// Batch holds orders, which makes it a dependant.
type Batch struct {
	Orders map[string]Order `json:"orders"`
}
