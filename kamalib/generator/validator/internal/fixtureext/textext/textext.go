// Package textext declares a container, a holder and a composite whose
// type supports come only from the text extension file ext.toml.
package textext

import (
	"iter"
	"maps"
	"slices"
)

// Bag holds elements, which Values yields alone.
type Bag[T any] struct{ items []T }

// NewBag returns a Bag of items.
func NewBag[T any](items ...T) Bag[T] { return Bag[T]{items: items} }

// Values yields the elements of b.
func (b Bag[T]) Values() iter.Seq[T] { return slices.Values(b.items) }

// List holds elements, which All yields by index.
type List[T any] struct{ items []T }

// NewList returns a List of items.
func NewList[T any](items ...T) List[T] { return List[T]{items: items} }

// All yields the elements of l by index.
func (l List[T]) All() iter.Seq2[int, T] { return slices.All(l.items) }

// Dict holds elements by key, which All yields.
type Dict[V any] struct{ m map[string]V }

// NewDict returns a Dict of m.
func NewDict[V any](m map[string]V) Dict[V] { return Dict[V]{m: m} }

// All yields the keys and the elements of d.
func (d Dict[V]) All() iter.Seq2[string, V] { return maps.All(d.m) }

// Maybe holds at most one value.
type Maybe[T any] struct {
	v  T
	ok bool
}

// Some returns a Maybe holding v.
func Some[T any](v T) Maybe[T] { return Maybe[T]{v: v, ok: true} }

// IsSome reports whether m holds a value.
func (m Maybe[T]) IsSome() bool { return m.ok }

// Get returns the value of m.
func (m Maybe[T]) Get() T { return m.v }

// Pair holds a key always and a value when Set is true.
type Pair[K, V any] struct {
	Key   K
	Value V
	Set   bool
}

// HasValue reports whether p holds its value.
func (p Pair[K, V]) HasValue() bool { return p.Set }

// Label is a text.
type Label struct{ s string }

// NewLabel returns the Label of s.
func NewLabel(s string) Label { return Label{s: s} }

func (l Label) String() string { return l.s }

//kama:validate
//kama:validate hook=False
type Order struct {
	//kama:validate length(ge=1), v=cmp(ge=0)
	Counts Bag[int]
	Items  Bag[Item]
	//kama:validate v=cmp(ge=0)
	Ranks List[int]
	Lists List[Item]
	//kama:validate v=length(le=3)
	Names Dict[string]
	Dicts Dict[Item]
	//kama:validate required, v=length(le=3)
	Note Maybe[string]
	Held Maybe[Item]
	//kama:validate k=length(ge=1), v=cmp(ge=1)
	Entry Pair[string, int]
	//kama:validate not zero, cmp(ne="none")
	Tag Label
}

//kama:validate
//kama:validate hook=False
type Item struct {
	//kama:validate required
	P *int
}
