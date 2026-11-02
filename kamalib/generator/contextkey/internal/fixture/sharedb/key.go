// Package sharedb holds context keys generated from the template that
// shareda is also generated from, built by a custom key type func.
package sharedb

// Key is a key type exported so that tests can tell it apart from the key
// type of shareda.
type Key struct{ Type string }

func newKey(typ string) *Key { return &Key{Type: typ} }
