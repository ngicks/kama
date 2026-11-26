package model

import (
	"github.com/ngicks/option/either"
	"github.com/ngicks/option/tuple"
)

// Composites holds the types of github.com/ngicks/option that hold several
// values on its type parameter.
//
//kama:validate hook=off
type Composites[T any] struct {
	E  either.Either[T, int]         `json:"e"`
	R  either.Result[T]              `json:"r"`
	UE either.UntaggedEither[int, T] `json:"ue"`
	UR either.UntaggedResult[T]      `json:"ur"`
	T2 tuple.Tuple2[T, string]       `json:"t2"`
	T3 tuple.Tuple3[int, T, T]       `json:"t3"`
}

// CompositesOf instantiates Composites with a type that has a Validate and
// with one that has not.
type CompositesOf struct {
	Tags Composites[Tag] `json:"tags"`
	Ints Composites[int] `json:"ints"`
}

// ConcreteComposites holds the composite types with concrete type
// arguments, a type with a Validate among them.
type ConcreteComposites struct {
	E either.Either[Tag, Tag]  `json:"e"`
	T tuple.Tuple2[int, []Tag] `json:"t"`
}
