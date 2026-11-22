package model

import (
	"time"

	"github.com/ngicks/option/elastic"
)

// Shapes holds values to validate in every shape the generator walks.
//
//kama:validate
//kama:validate:hook off
type Shapes[_ any] struct {
	E  elastic.Elastic[Tag] `json:"e"`
	A  [2]Tag               `json:"a"`
	N  []map[string][]*Tag  `json:"n"`
	BP Box[*Tag]            `json:"bp"`
	Embedded
	//kama:validate:required
	At time.Time `json:"at"`
	S  struct {
		T Tag `json:"t"`
	} `json:"s"`
}

//kama:validate
//kama:validate:hook off
type Embedded struct {
	//kama:validate:required
	Code string `json:"code"`
}
