// Package other declares types the typeprint tests qualify with the
// package identifier.
package other

import "bytes"

type Named struct{}

type Pair[K comparable, V any] struct {
	Key   K
	Value V
}

type Holder struct {
	Buf bytes.Buffer
}

type unexported int

var Unexported unexported

// The names below are unexported, so they denote other names when spelled
// in another package.
var (
	UnexportedField    struct{ x int }
	UnexportedEmbedded struct{ int }
	UnexportedMethod   interface{ seal() }
)
