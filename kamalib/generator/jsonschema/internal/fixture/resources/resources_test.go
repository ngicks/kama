package resources

import (
	"encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

func TestOrder(t *testing.T) {
	for _, text := range []string{
		`{"customer": {"name": "a"}}`,
		`{"customer": {"name": "a", "address": {"zip": "12345"}}, "shipTo": "54321"}`,
		`{"customer": {"name": "a"}, "lines": [{"sku": "AB", "count": 2}], "note": "fragile"}`,
	} {
		var v Order
		assert.NilError(t, json.Unmarshal([]byte(text), &v), text)
	}

	for text, kind := range map[string]error{
		`{}`:                         validation.ErrRequired,
		`{"customer": {"name": ""}}`: validation.ErrLength,
		`{"customer": {"name": "a", "address": {"zip": "1"}}}`: validation.ErrPattern,
		`{"customer": {"name": "a"}, "shipTo": "x"}`:           validation.ErrPattern,
		// The anchor name of line.json names Sku, not CustomerName.
		`{"customer": {"name": "a"}, "lines": [{"sku": "ab"}]}`:           validation.ErrPattern,
		`{"customer": {"name": "a"}, "note": "longer than twenty runes"}`: validation.ErrLength,
	} {
		var v Order
		assert.ErrorIs(t, json.Unmarshal([]byte(text), &v), kind, text)
	}
}
