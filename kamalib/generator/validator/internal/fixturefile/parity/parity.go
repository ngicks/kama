// Package parity declares the types of internal/fixture/parity without
// directives. validate.yaml and kama_validate.yaml give their options.
package parity

import (
	"errors"

	"github.com/ngicks/option/opt"
)

// ErrNote is the error of the hook of Order.Note.
var ErrNote = errors.New("note too long")

type Order struct {
	Id       string `json:"id"`
	Lines    []Line `json:"lines,omitzero"`
	Currency string `json:"currency"`
	Note     string `json:"note"`
}

func (o Order) validateNote(bool) error {
	if len(o.Note) > 10 {
		return ErrNote
	}

	return nil
}

type Line struct {
	Qty int                `json:"qty"`
	Url opt.Option[string] `json:"url"`
	Sku Sku                `json:"sku"`
}

type Sku string

type Discount struct {
	Percent opt.Option[int] `json:"percent,omitzero"`
	Kind    string          `json:"kind"`
	Amount  *int            `json:"amount,omitzero"`
}

// Batch holds orders, which makes it a dependant.
type Batch struct {
	Orders map[string]Order `json:"orders"`
}
