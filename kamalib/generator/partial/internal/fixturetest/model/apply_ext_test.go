package model_test

import (
	"testing"

	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/partial/internal/fixturetest/model"
)

func TestExtPartial(t *testing.T) {
	var p ExtPartial

	p.Id = und.Defined("e")
	p.Sample = und.Defined(model.SamplePartial{Count: und.Defined(3)})

	got := p.ApplyTo(Ext{Item: model.Item{Name: "n"}})
	assert.DeepEqual(t, got, Ext{
		Id:     "e",
		Item:   model.Item{Name: "n"},
		Sample: model.Sample{Count: 3},
	})
}
