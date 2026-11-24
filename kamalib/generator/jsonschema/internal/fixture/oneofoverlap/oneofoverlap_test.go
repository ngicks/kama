package oneofoverlap

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"reflect"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema/jsonschemart"
)

// allFields compares unexported fields too, such as those of opt.Option.
var allFields = gocmp.Exporter(func(reflect.Type) bool { return true })

func TestDecode(t *testing.T) {
	var v Either
	assert.NilError(t, json.Unmarshal([]byte(`{"l":"a","r":1}`), &v))
	assert.DeepEqual(
		t,
		v.Kind,
		EitherKind(EitherLeft{Left: EitherLeftValue{
			L:                    opt.Some("a"),
			AdditionalProperties: map[string]jsontext.Value{"r": jsontext.Value("1")},
		}}),
		allFields,
	)
}

// TestDecodeBothMatch checks a value both objects accept, which a oneOf
// holds as unknown and Validate rejects naming both.
func TestDecodeBothMatch(t *testing.T) {
	var v Either
	assert.NilError(t, json.Unmarshal([]byte(`{}`), &v))

	_, ok := v.Kind.(EitherUnknown)
	assert.Assert(t, ok, "%T", v.Kind)

	err := v.Validate(false)
	assert.ErrorIs(t, err, jsonschemart.ErrMultipleMatch)
	assert.ErrorContains(t, err, "value matches Left, Right")

	out, err := json.Marshal(v)
	assert.NilError(t, err)
	assert.Equal(t, string(out), `{}`)
}
