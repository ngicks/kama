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

type bufPrinter struct {
	buf *bytes.Buffer
}

// newBufPrinter appends to b in place. A step receives the text of the
// steps before it and hands on its own, so nothing reads b again, and
// copying it would make every step cost the length of the whole text.
func newBufPrinter(b []byte) *bufPrinter {
	return &bufPrinter{buf: bytes.NewBuffer(b)}
}

func (p *bufPrinter) Printf(format string, a ...any) {
	fmt.Fprintf(p.buf, format, a...)
}

func (p *bufPrinter) Bytes() []byte {
	return p.buf.Bytes()
}
