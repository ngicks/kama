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
	buf bytes.Buffer
}

func newBufPrinter(b []byte) *bufPrinter {
	buf := bytes.Buffer{}
	buf.Write(b)
	return &bufPrinter{buf: buf}
}

func (p *bufPrinter) Printf(format string, a ...any) {
	fmt.Fprintf(&p.buf, format, a...)
}

func (p *bufPrinter) Bytes() []byte {
	return p.buf.Bytes()
}
