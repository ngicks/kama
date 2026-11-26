// Package fixture holds directive comments for genopt tests.
//
//kama:partial package
package fixture

import (
	"bytes"
	"io"
)

//kama:partial file

// Single is a type.
//
// kama:space
//
//kama:partial
//go:generate echo
//other:partial
type Single struct {
	//kama:partial:merge append
	A []string
	B string //kama:partial:flag name="addr", short="a", usage="listen address"
	//kama:partial:skip

	//kama:partial:x
	C, D          int
	*bytes.Buffer //kama:partial:embedded
	io.Reader
	Nested struct {
		//kama:partial:nested
		E int
	}
}

//kama:partial ignore
type (
	//kama:partial grouped
	G1 int
	G2 string //kama:partial line
)

type Generic[T any] struct{}

type WithGeneric struct {
	//kama:partial:gen
	Generic[int]
}

//kama:partial func
func F() {
	//kama:partial body
	_ = 0
}

var V = 1 //kama:partial var
