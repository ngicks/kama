// Package model holds types listed one by one in kama_partial.yaml.
package model

import (
	"time"

	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

// User is listed in kama_partial.yaml.
type User struct {
	// Base is an embedded target.
	Base
	// Audit is an embedded pointer to a target.
	*Audit
	// Status and Time are embedded types without partial variants.
	Status
	time.Time

	Name string `json:"name"`
	//ngignore:noomitempty the generator replaces omitempty with omitzero.
	Age int `json:"age,omitempty"`
	//ngignore:noomitempty the generator replaces omitempty with omitzero.
	Nick    *string `json:"nick,omitempty" yaml:"nick,omitzero" toml:"nick,omitempty"`
	Email   string  `json:"email" yaml:"email" toml:"email" xml:"email"`
	Skipped string  `json:"-"`

	Address Address `json:"address"`
	Geo     *Geo    `json:"geo"`

	Tags  []string       `json:"tags"`
	Attrs map[string]int `json:"attrs"`
	Pos   [2]float64     `json:"pos"`
	Geos  []Geo          `json:"geos"`

	Items Items `json:"items"`
	Index Index `json:"index"`

	Box  Box[int]          `json:"box"`
	Pair Pair[string, Geo] `json:"pair"`
	Stat *Stat[float64]    `json:"stat"`

	Opt     opt.Option[int]      `json:"opt"`
	Und     und.Und[string]      `json:"und"`
	Elastic elastic.Elastic[int] `json:"elastic"`

	Fn  func() error
	Ch  chan int
	Any any

	//nolint:unused // unexported fields are kept in partial variants.
	secret int
}

// Base is reachable through an embedded field.
type Base struct {
	ID string `json:"id"`
}

// Audit is reachable through an embedded pointer.
type Audit struct {
	CreatedAt time.Time `json:"created_at"`
}

// Status has a basic underlying type, so it gets no partial variant.
type Status string

// Address is reachable through a field.
type Address struct {
	City string `json:"city"`
	Geo  *Geo   `json:"geo"`
}

// Geo is reachable through pointers and a slice.
type Geo struct {
	Lat, Lng float64
}

// Items is reachable and has a slice underlying type.
type Items []Item

// Item is reachable through the element of Items.
type Item struct {
	Name  string `json:"name"`
	Count *int   `json:"count"`
}

// Index has a map underlying type.
type Index map[string]*Item

// Box is a generic target.
type Box[T any] struct {
	Value  T   `json:"value"`
	Values []T `json:"values"`
	Ptr    *T  `json:"ptr"`
}

// Pair is a generic target with several type parameters.
type Pair[K comparable, V any] struct {
	Key   K `json:"key"`
	Value V `json:"value"`
}

// Stat has a union constraint.
type Stat[N ~int | ~float64] struct {
	Total N `json:"total"`
}

// Unrelated is neither marked nor reachable.
type Unrelated struct {
	A int
}
