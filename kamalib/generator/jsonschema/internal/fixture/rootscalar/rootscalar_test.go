package rootscalar

import (
	"encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

// TestDecode checks the validators of the root type run on decoding.
func TestDecode(t *testing.T) {
	var p Port
	assert.NilError(t, json.Unmarshal([]byte(`8080`), &p))
	assert.Equal(t, p, Port(8080))

	for _, in := range []string{`0`, `65536`} {
		assert.ErrorIs(t, json.Unmarshal([]byte(in), &p), validator.ErrCompare, in)
	}

	assert.ErrorIs(t, Port(0).Validate(false), validator.ErrCompare)
}
