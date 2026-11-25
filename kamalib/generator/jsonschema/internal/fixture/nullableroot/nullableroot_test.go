package nullableroot

import (
	"encoding/json/v2"
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

func TestDecode(t *testing.T) {
	cases := []struct {
		in   string
		want Code
	}{
		{`null`, Code(opt.None[CodeValue]())},
		{`1.5`, Code(opt.Some[CodeValue](1.5))},
		{`18446744073709551616`, Code(opt.Some[CodeValue](18446744073709551616))},
		// The float64 of the value cannot tell 2^64+1 from 2^64.
		{`18446744073709551617`, Code(opt.Some[CodeValue](18446744073709551616))},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			var v Code
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))
			assert.Equal(t, v, tc.want)
		})
	}

	var v Code
	assert.ErrorIs(t, json.Unmarshal([]byte(`2`), &v), validator.ErrConst)
}
