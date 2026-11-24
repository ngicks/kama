package remote

import (
	"encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/validation"
)

func TestInvoice(t *testing.T) {
	for _, text := range []string{
		`{"total": {"amount": 1, "currency": "EUR"}}`,
		`{"total": {"amount": 1, "currency": "EUR"}, "currency": "USD",
			"payer": {"name": "a", "balance": {"amount": 0, "currency": "JPY"},
				"partner": {"name": "b"}}}`,
	} {
		var v Invoice
		assert.NilError(t, json.Unmarshal([]byte(text), &v), text)
	}

	total := `"total": {"amount": 1, "currency": "EUR"}`

	for text, kind := range map[string]error{
		`{}`: validation.ErrRequired,
		`{"total": {"amount": -1, "currency": "EUR"}}`:           validation.ErrCompare,
		`{"total": {"amount": 1, "currency": "eur"}}`:            validation.ErrPattern,
		`{` + total + `, "currency": "x"}`:                       validation.ErrPattern,
		`{` + total + `, "payer": {"name": "a", "partner": {}}}`: validation.ErrRequired,
	} {
		var v Invoice
		assert.ErrorIs(t, json.Unmarshal([]byte(text), &v), kind, text)
	}
}
