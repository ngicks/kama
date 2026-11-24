package oneofoverlap

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"reflect"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema/jsonschemart"
)

// allFields compares the unexported field _x of the generated structs too.
var allFields = gocmp.Exporter(func(reflect.Type) bool { return true })

func TestDecode(t *testing.T) {
	var v Either
	assert.NilError(t, json.Unmarshal([]byte(`{"l":"a","r":1}`), &v))
	assert.DeepEqual(
		t,
		v.Kind,
		EitherKind(EitherLeft{Left: EitherLeftValue{
			L:  opt.Some("a"),
			_x: map[string]jsontext.Value{"r": jsontext.Value("1")},
		}}),
		allFields,
	)
}

// TestDecodeBothMatch checks a value both objects accept, which a oneOf
// rejects naming both.
func TestDecodeBothMatch(t *testing.T) {
	var v Either

	err := json.Unmarshal([]byte(`{}`), &v)

	se, ok := errors.AsType[*json.SemanticError](err)
	assert.Assert(t, ok, "%T: %v", err, err)
	assert.ErrorIs(t, se.Err, jsonschemart.ErrMultipleMatch)
	assert.ErrorContains(t, se.Err, "value matches Left, Right")
}
