package oneofoverlap

import (
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema/jsonschemart"
)

func TestDecode(t *testing.T) {
	var v Either
	assert.NilError(t, json.Unmarshal([]byte(`{"l":"a","r":1}`), &v))
	assert.Equal(t, v.Kind, EitherKind(EitherLeft{Left: EitherLeftValue{L: opt.Some("a")}}))
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
