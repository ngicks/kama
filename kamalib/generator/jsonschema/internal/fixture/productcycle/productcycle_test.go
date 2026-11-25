package productcycle

import (
	"encoding/json/v2"
	"reflect"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamaruntime/validator"
)

// allFields compares unexported fields too, such as those of opt.Option.
var allFields = gocmp.Exporter(func(reflect.Type) bool { return true })

func TestMarshalNilRequiredPointer(t *testing.T) {
	_, err := json.Marshal(Product{})
	assert.ErrorIs(t, err, validator.ErrNil)
	assert.DeepEqual(t, validator.Pointers(err), []string{"/req"})
}

// TestSharedMember checks a property two members declare: both decode it,
// and the first member writes it on marshaling.
func TestSharedMember(t *testing.T) {
	var v Product
	assert.NilError(t, json.Unmarshal([]byte(`{"shared":"s","a":1,"b":2,"req":{}}`), &v))
	assert.DeepEqual(t, v.A, A{Shared: opt.Some("s"), A: opt.Some[int64](1)}, allFields)
	assert.DeepEqual(t, v.B, B{Shared: opt.Some("s"), B: opt.Some[int64](2)}, allFields)
	assert.DeepEqual(t, *v.Req, Back{}, allFields)

	out, err := json.Marshal(Product{
		A:   A{Shared: opt.Some("from a")},
		B:   B{Shared: opt.Some("from b")},
		Req: &Back{},
	})
	assert.NilError(t, err)
	assert.Equal(t, string(out), `{"shared":"from a","req":{}}`)
}
