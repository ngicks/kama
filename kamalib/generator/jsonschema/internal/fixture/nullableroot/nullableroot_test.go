package nullableroot

import (
	"encoding/json/v2"
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema/jsonschemart"
)

func TestDecode(t *testing.T) {
	cases := []struct {
		in   string
		want Code
	}{
		{`null`, Code(opt.None[float64]())},
		{`1.5`, Code(opt.Some(1.5))},
		{`18446744073709551616`, Code(opt.Some[float64](18446744073709551616))},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			var v Code
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))
			assert.Equal(t, v, tc.want)
		})
	}

	for _, in := range []string{`2`, `18446744073709551617`} {
		t.Run(in, func(t *testing.T) {
			var v Code
			assert.ErrorIs(t, json.Unmarshal([]byte(in), &v), jsonschemart.ErrEnum)
		})
	}
}
