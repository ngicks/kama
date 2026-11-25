package model

import (
	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

// Held applies validators of strings to the string an und.Und holds.
//
//kama:validate hook=off
type Held struct {
	//kama:validate length=">=1 <=3"
	Length und.Und[string] `json:"length"`
	//kama:validate runes="<=2"
	Runes und.Und[string] `json:"runes"`
	//kama:validate regexp="^[a-z]+$"
	Pattern und.Und[string] `json:"pattern"`
}

// Options holds every option type on its type parameter.
//
//kama:validate hook=off
type Options[T any] struct {
	O opt.Option[T]      `json:"o"`
	U und.Und[T]         `json:"u"`
	E elastic.Elastic[T] `json:"e"`
}

// OptionsOf instantiates Options with a type that has a Validate and with
// one that has not.
type OptionsOf struct {
	Tags Options[Tag] `json:"tags"`
	Ints Options[int] `json:"ints"`
}
