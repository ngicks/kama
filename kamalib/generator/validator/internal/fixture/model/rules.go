package model

import (
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
//kama:validate
//kama:validate:hook off
type Rules struct {
	//kama:validate:zero
	Zero int `json:"zero"`
	//kama:validate:zero
	ZeroPtr *int `json:"zero_ptr"`
	//kama:validate:nil
	Nil []int `json:"nil"`
	//kama:validate:null
	Null und.Und[int] `json:"null"`
	//kama:validate:null
	NullPtr *und.Und[int] `json:"null_ptr"`
	//kama:validate:regexp "^[a-z]+$"
	Slug string `json:"slug"`
	//kama:validate:regexp "^[a-z]+$"
	SlugBytes []byte `json:"slug_bytes"`
	//kama:validate:length >=1 <=3
	Items []int `json:"items"`
	//kama:validate:length ==2
	Code opt.Option[string] `json:"code"`
	//kama:validate:cmp >=0 <100
	Level Level `json:"level"`
	//kama:validate:cmp >"b"
	Name *string `json:"name"`
	//kama:validate:cmp >=0.5
	Ratio float64 `json:"ratio"`
	//kama:validate:cmp ==true
	Agreed bool `json:"agreed"`
	//kama:validate:const red "dark blue"
	Color Color `json:"color"`
	//kama:validate:const 1 2 3
	Pick uint8 `json:"pick"`
	//kama:validate:format date
	Date string `json:"date"`
	//kama:validate:format date-time
	At opt.Option[string] `json:"at"`
	//kama:validate:format url
	Url string `json:"url"`
	//kama:validate:null
	Elastic elastic.Elastic[int] `json:"elastic"`
}

// Ordered holds maps whose keys the generated code visits in sorted
// order. It holds Tag, which makes it a dependant.
type Ordered struct {
	ByName  map[string]Tag `json:"by_name"`
	ByLevel map[Level]Tag  `json:"by_level"`
}
