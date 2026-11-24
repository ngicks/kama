package model

import (
	"time"

	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

// Color is a named string type the const validator compares.
type Color string

// Level is a named integer type the cmp validator compares and maps key
// by.
type Level int

// Rules holds a field for each built-in validator.
//
//kama:validate hook=off
type Rules struct {
	//kama:validate zero
	Zero int `json:"zero"`
	//kama:validate zero
	ZeroPtr *int `json:"zero_ptr"`
	//kama:validate nil
	Nil []int `json:"nil"`
	//kama:validate null
	Null und.Und[int] `json:"null"`
	//kama:validate null
	NullPtr *und.Und[int] `json:"null_ptr"`
	//kama:validate regexp="^[a-z]+$"
	Slug string `json:"slug"`
	//kama:validate regexp="^[a-z]+$"
	SlugBytes []byte `json:"slug_bytes"`
	//kama:validate length=">=1 <=3"
	Items []int `json:"items"`
	//kama:validate length="==2"
	Code opt.Option[string] `json:"code"`
	//kama:validate runes="==2"
	Kana string `json:"kana"`
	//kama:validate unique
	Tags []any `json:"tags"`
	//kama:validate unique
	Triple opt.Option[[3]int] `json:"triple"`
	//kama:validate unique
	Refs []*int `json:"refs"`
	//kama:validate cmp=">=0 <100"
	Level Level `json:"level"`
	//kama:validate cmp=`>"b"`
	Name *string `json:"name"`
	//kama:validate cmp=">=0.5"
	Ratio float64 `json:"ratio"`
	//kama:validate cmp=">10 !=5"
	Score int `json:"score"`
	//kama:validate multipleof=3
	Step int `json:"step"`
	//kama:validate multipleof=0.25
	Quarter *float32 `json:"quarter"`
	//kama:validate cmp="==true"
	Agreed bool `json:"agreed"`
	//kama:validate const=`"red" "dark blue"`
	Color Color `json:"color"`
	//kama:validate const="1 2 3"
	Pick uint8 `json:"pick"`
	//kama:validate format=date
	Date string `json:"date"`
	//kama:validate format=date-time
	At opt.Option[string] `json:"at"`
	//kama:validate format=url
	Url string `json:"url"`
	//kama:validate null
	Elastic elastic.Elastic[int] `json:"elastic"`
}

// MultipleOf holds a field per schema of multipleOf.json of the JSON
// Schema test suite. 1e308 overflows an integer type, so the field of the
// schema of type integer and multipleOf 0.123456789 is a float.
//
//kama:validate hook=off
type MultipleOf struct {
	//kama:validate multipleof=2
	ByInt int `json:"by_int"`
	//kama:validate multipleof=1.5
	ByNumber float64 `json:"by_number"`
	//kama:validate multipleof=0.0001
	BySmallNumber float64 `json:"by_small_number"`
	//kama:validate multipleof=0.0001
	BySmallNumber32 float32 `json:"by_small_number32"`
	//kama:validate multipleof=1e-8
	ByVerySmallNumber float64 `json:"by_very_small_number"`
	//kama:validate multipleof=0.123456789
	FloatDivisionInf float64 `json:"float_division_inf"`
	//kama:validate multipleof=1e-8
	SmallMultipleOfLargeInteger int64 `json:"small_multiple_of_large_integer"`
}

// Ordered holds maps whose keys the generated code visits in sorted
// order, and a map of float keys, which it ranges over directly. It holds
// Tag, which makes it a dependant.
type Ordered struct {
	ByName  map[string]Tag  `json:"by_name"`
	ByLevel map[Level]Tag   `json:"by_level"`
	ByRatio map[float64]Tag `json:"by_ratio"`
}

// Stamp promotes IsZero through an embedded pointer, which would panic on
// the zero value.
type Stamp struct{ *time.Time }

// Stamped holds Stamp, which required and zero compare with its zero value.
//
//kama:validate hook=off
type Stamped struct {
	//kama:validate required
	At Stamp `json:"at"`
	//kama:validate zero
	Zero Stamp `json:"zero"`
}
