package genhelper

import (
	"bytes"
	"fmt"
)

// Printer builds the text of one step. [Context.Printer] returns one.
//
// A Printer returns no error.
type Printer interface {
	// Printf formats as fmt.Printf does and adds the result to the text.
	Printf(format string, a ...any)
	// Bytes returns the text.
	Bytes() []byte
}

// NewPrinter returns a Printer whose text starts with b. It appends to b
// in place, so the caller does not use b afterwards.
//
// A step receives the text of the steps before it and hands on its own, so
// nothing reads b again, and copying it would make every step cost the
// length of the whole text.
func NewPrinter(b []byte) Printer {
	return &bufPrinter{buf: bytes.NewBuffer(b)}
}

type bufPrinter struct {
	buf *bytes.Buffer
}

func (p *bufPrinter) Printf(format string, a ...any) {
	fmt.Fprintf(p.buf, format, a...)
}

func (p *bufPrinter) Bytes() []byte {
	return p.buf.Bytes()
}
