package model_test

import "github.com/ngicks/kama/kamalib/generator/partial/internal/fixturetest/model"

// Ext is a target of the external test package. It holds a target of the
// package and a target of a _test.go file of the package.
//
//kama:partial
type Ext struct {
	model.Base

	Item   model.Item   `json:"item"`
	Sample model.Sample `json:"sample"`
}
