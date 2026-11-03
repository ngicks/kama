package pkgmark

import (
	"github.com/ngicks/kama/kamalib/generator/partial/internal/fixture/model"
	"github.com/ngicks/kama/kamalib/generator/partial/internal/fixture/shared"
)

// Order refers to targets of other packages.
type Order struct {
	ID       string        `json:"id"`
	Total    shared.Money  `json:"total"`
	Lines    Lines         `json:"lines"`
	Customer *model.User   `json:"customer"`
	Draft    Draft         `json:"draft"`
	Hidden   shared.Public `json:"hidden"`
}

// Lines is a slice of a target of the package.
type Lines []Line

// Line is marked by the package mark.
type Line struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

// Draft opts out of the package mark.
//
//kama:partial ignore
type Draft struct {
	Note string
}
