package model

import (
	"time"

	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

// Shapes holds values to validate in every shape the generator walks.
//
//kama:validate hook=off
type Shapes[_ any] struct {
	E  elastic.Elastic[Tag] `json:"e"`
	A  [2]Tag               `json:"a"`
	N  []map[string][]*Tag  `json:"n"`
	BP Box[*Tag]            `json:"bp"`
	Embedded
	//kama:validate required
	At time.Time `json:"at"`
	S  struct {
		T Tag `json:"t"`
	} `json:"s"`
}

// Elastics holds elastic.Elastic values as elements of containers.
//
//kama:validate hook=off
type Elastics struct {
	S []elastic.Elastic[Tag]          `json:"s"`
	M map[string]elastic.Elastic[Tag] `json:"m"`
	A [2]elastic.Elastic[Tag]         `json:"a"`
}

//kama:validate hook=off
type Embedded struct {
	//kama:validate required
	Code string `json:"code"`
}

// Rank is a scalar type with validators of its own.
//
//kama:validate cmp=">=1 <=5"
type Rank int

// Ranks is a slice type whose validators run before its elements are
// walked.
//
//kama:validate length="<=3" unique
type Ranks []Rank

// Values holds Rank in the shapes the generator walks.
type Values struct {
	L Rank             `json:"l"`
	P *Rank            `json:"p"`
	O opt.Option[Rank] `json:"o"`
	U und.Und[Rank]    `json:"u"`
	S Ranks            `json:"s"`
	M map[string]Rank  `json:"m"`
	B Box[Rank]        `json:"b"`
	//kama:validate cmp="!=6"
	F Rank `json:"f"`
}
