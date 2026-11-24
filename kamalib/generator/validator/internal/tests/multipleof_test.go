package tests

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamalib/pkg/validation"
)

type suiteGroup struct {
	Description string      `json:"description"`
	Tests       []suiteCase `json:"tests"`
}

type suiteCase struct {
	Description string `json:"description"`
	Data        any    `json:"data"`
	Valid       bool   `json:"valid"`
}

// TestMultipleOfSuite runs the numeric cases of multipleOf.json of the
// JSON Schema test suite against the field of model.MultipleOf standing
// for the schema of each group.
func TestMultipleOfSuite(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash(
		"../../../../pkg/jsonschema/testdata/suite/tests/draft2020-12/multipleOf.json",
	))
	assert.NilError(t, err)

	dec := json.NewDecoder(bytes.NewReader(src))
	dec.UseNumber()

	var groups []suiteGroup

	assert.NilError(t, dec.Decode(&groups))

	float := func(n json.Number) float64 {
		f, err := strconv.ParseFloat(string(n), 64)
		assert.NilError(t, err)

		return f
	}
	integer := func(n json.Number) int64 {
		i, err := strconv.ParseInt(string(n), 10, 64)
		assert.NilError(t, err)

		return i
	}

	set := map[string]func(m *model.MultipleOf, n json.Number){
		"by int":          func(m *model.MultipleOf, n json.Number) { m.ByInt = int(integer(n)) },
		"by number":       func(m *model.MultipleOf, n json.Number) { m.ByNumber = float(n) },
		"by small number": func(m *model.MultipleOf, n json.Number) { m.BySmallNumber = float(n) },
		"by very small number": func(m *model.MultipleOf, n json.Number) {
			m.ByVerySmallNumber = float(n)
		},
		"float division = inf": func(m *model.MultipleOf, n json.Number) {
			m.FloatDivisionInf = float(n)
		},
		"small multiple of large integer": func(m *model.MultipleOf, n json.Number) {
			m.SmallMultipleOfLargeInteger = integer(n)
		},
	}

	var ran int

	for _, g := range groups {
		setter, ok := set[g.Description]
		assert.Assert(t, ok, "no field for group %q", g.Description)

		for _, c := range g.Tests {
			n, isNumber := c.Data.(json.Number)
			if !isNumber {
				continue
			}

			var m model.MultipleOf

			setter(&m, n)

			err := m.Validate(false)
			if c.Valid {
				assert.NilError(t, err, "%s: %s", g.Description, c.Description)
			} else {
				assert.ErrorIs(
					t,
					err,
					validation.ErrMultiple,
					"%s: %s",
					g.Description,
					c.Description,
				)
			}

			ran++
		}
	}

	assert.Equal(t, ran, 12)
}

// TestMultipleOfFloat32 reads a float32 as its own shortest decimal, which
// the float64 it widens to does not share.
func TestMultipleOfFloat32(t *testing.T) {
	assert.NilError(t, model.MultipleOf{BySmallNumber32: 0.0075}.Validate(false))

	err := model.MultipleOf{BySmallNumber32: 0.00751}.Validate(false)
	assert.ErrorContains(t, err, "want a multiple of 0.0001, got 0.00751")
}

// TestMultipleOfNonFinite counts NaN and the infinities as multiples of
// nothing.
func TestMultipleOfNonFinite(t *testing.T) {
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		err := model.MultipleOf{ByNumber: f}.Validate(false)
		assert.ErrorIs(t, err, validation.ErrMultiple, f)
	}
}
