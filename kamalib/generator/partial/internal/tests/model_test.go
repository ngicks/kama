package tests

import (
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/partial/internal/fixture/model"
	"github.com/ngicks/kama/kamalib/generator/partial/internal/fixture/pkgmark"
)

func TestZeroPartialMarshalsEmpty(t *testing.T) {
	for _, v := range []any{model.UserPartial{}, model.ConfigPartial{}, pkgmark.OrderPartial{}} {
		b, err := jsonv2.Marshal(v)
		assert.NilError(t, err)
		assert.Equal(t, string(b), "{}")
	}
}

func TestPartialKeepsNull(t *testing.T) {
	var u model.UserPartial
	assert.NilError(t, jsonv2.Unmarshal(
		[]byte(`{"id":"i","name":null,"age":3,"address":{"city":"c"},"items":[{"name":"n"}]}`),
		&u,
	))

	assert.Equal(t, u.ID, und.Defined("i"))
	assert.Assert(t, u.Name.IsNull())
	assert.Equal(t, u.Age, und.Defined(3))
	assert.Assert(t, u.Nick.IsUndefined())
	assert.Equal(t, u.Address.Value().City, und.Defined("c"))
	assert.Equal(t, u.Items.Value()[0].Value().Name, und.Defined("n"))

	b, err := jsonv2.Marshal(u)
	assert.NilError(t, err)
	assert.Equal(
		t,
		string(b),
		`{"id":"i","name":null,"age":3,"address":{"city":"c"},"items":[{"name":"n"}]}`,
	)
}
