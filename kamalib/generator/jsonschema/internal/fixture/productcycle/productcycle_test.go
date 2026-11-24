package productcycle

import (
	"encoding/json/v2"
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

func TestMarshalNilRequiredPointer(t *testing.T) {
	_, err := json.Marshal(Product{})
	assert.ErrorIs(t, err, validation.ErrNil)
	assert.DeepEqual(t, validation.Pointers(err), []string{"/req"})
}

// TestSharedMember checks a property two members declare: both decode it,
// and the first member writes it on marshaling.
func TestSharedMember(t *testing.T) {
	var v Product
	assert.NilError(t, json.Unmarshal([]byte(`{"shared":"s","a":1,"b":2,"req":{}}`), &v))
	assert.Equal(t, v.A, A{Shared: opt.Some("s"), A: opt.Some[int64](1)})
	assert.Equal(t, v.B, B{Shared: opt.Some("s"), B: opt.Some[int64](2)})
	assert.Equal(t, *v.Req, Back{})

	out, err := json.Marshal(Product{
		A:   A{Shared: opt.Some("from a")},
		B:   B{Shared: opt.Some("from b")},
		Req: &Back{},
	})
	assert.NilError(t, err)
	assert.Equal(t, string(out), `{"shared":"from a","req":{}}`)
}
