package model

import (
	"iter"

	"github.com/ngicks/opiter"
)

// List is a generic container no type support knows, which yields its
// elements by All and Seq.
type List[T any] struct{ items []T }

// NewList returns a List of items.
func NewList[T any](items ...T) List[T] { return List[T]{items: items} }

// All yields the elements of l.
func (l List[T]) All() iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, v := range l.items {
			if !yield(v) {
				return
			}
		}
	}
}

// Seq is All as an opiter.Seq.
func (l List[T]) Seq() opiter.Seq[T] { return opiter.Seq[T](l.All()) }

// Seq2Box is a generic container of key-value pairs in order, which yields
// them by All and Seq.
type Seq2Box[K comparable, V any] struct {
	keys   []K
	values []V
}

// NewSeq2Box returns Seq2Box holding v under k, in order.
func NewSeq2Box[K comparable, V any](k []K, v []V) Seq2Box[K, V] {
	return Seq2Box[K, V]{keys: k, values: v}
}

// All yields the pairs of p.
func (p Seq2Box[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for i, k := range p.keys {
			if !yield(k, p.values[i]) {
				return
			}
		}
	}
}

// Seq is All as an opiter.Seq2.
func (p Seq2Box[K, V]) Seq() opiter.Seq2[K, V] { return opiter.Seq2[K, V](p.All()) }

// Cell holds one value, which Value returns.
type Cell[T any] struct{ V T }

// Value returns the value of c.
func (c Cell[T]) Value() T { return c.V }

// Maybe may hold a value, which Get returns with whether it holds one.
type Maybe[T any] struct {
	V  T
	Ok bool
}

// Get returns the value of m and whether m holds one.
func (m Maybe[T]) Get() (T, bool) { return m.V, m.Ok }

// Containers validates the values generic containers hold through the
// methods its directives name.
//
//kama:validate hook=False
type Containers struct {
	//kama:validate iter="All", v=length(le=3)
	Names List[string] `json:"names"`
	//kama:validate iter="Seq", v=cmp(ge=0)
	Counts List[int] `json:"counts"`
	//kama:validate iter="All"
	Items List[Item] `json:"items"`
	//kama:validate iter2="All", k=length(ge=1), v=cmp(ge=0)
	Scores Seq2Box[string, int] `json:"scores"`
	//kama:validate iter2="Seq", v=cmp(lt=10)
	Points Seq2Box[float64, int] `json:"points"`
	//kama:validate unwrap="Value", v=length(le=3)
	Label Cell[string] `json:"label"`
	//kama:validate unwrap="Get", v=cmp(ge=1)
	Size Maybe[int] `json:"size"`
}

// Item is an element with a Validate of its own.
//
//kama:validate hook=False
type Item struct {
	//kama:validate length(ge=1)
	Name string `json:"name"`
}

// Tick counts the calls of its Validate.
type Tick struct{ Calls *int }

// Validate counts the call.
func (t Tick) Validate(bool) error {
	if t.Calls != nil {
		*t.Calls++
	}

	return nil
}

// Walks holds Ticks, whose Validate the nested walk calls unless
// nested=False switches it off.
//
//kama:validate hook=False
type Walks struct {
	On []Tick `json:"on"`
	//kama:validate nested=False
	Off []Tick `json:"off"`
	//kama:validate e=g(nested=False)
	Inner [][]Tick `json:"inner"`
	//kama:validate nested=False, e=g(nested=True)
	Back [][]Tick `json:"back"`
	//kama:validate nested=False, e=length(le=1)
	Ruled [][]Tick `json:"ruled"`
	//kama:validate iter="All"
	Listed List[Tick] `json:"listed"`
	//kama:validate iter="All", nested=False
	Unlisted List[Tick] `json:"unlisted"`
	Held     *Tick      `json:"held"`
}
