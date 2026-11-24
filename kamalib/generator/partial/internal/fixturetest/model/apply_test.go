package model

import (
	"testing"

	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"
)

func TestSamplePartial(t *testing.T) {
	var p SamplePartial

	p.Name = und.Defined("n")
	p.Owner = und.Defined(BasePartial{Id: und.Defined("o")})
	p.Parent = und.Defined(ItemPartial{Name: und.Defined("p")})

	got := p.ApplyTo(Sample{Id: "i", Count: 1})
	assert.DeepEqual(t, got, Sample{
		Id:     "i",
		Name:   "n",
		Count:  1,
		Owner:  Base{Id: "o"},
		Parent: Item{Name: "p"},
	})
}
