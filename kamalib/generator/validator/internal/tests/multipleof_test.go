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
	"github.com/ngicks/kama/kamalib/internal/suitetest"
	"github.com/ngicks/kama/kamaruntime/validator"
)

// TestMultipleOfSuite runs the numeric cases of multipleOf.json of the
// JSON Schema test suite against the field of model.MultipleOf standing
// for the schema of each group.
func TestMultipleOfSuite(t *testing.T) {
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
		"multipleOf int": func(m *model.MultipleOf, n json.Number) { m.ByInt = int(integer(n)) },
		"multipleOf number": func(m *model.MultipleOf, n json.Number) {
			m.ByNumber = float(n)
		},
		"multipleOf small number": func(m *model.MultipleOf, n json.Number) {
			m.BySmallNumber = float(n)
		},
		"multipleOf very small number": func(m *model.MultipleOf, n json.Number) {
			m.ByVerySmallNumber = float(n)
		},
		"multipleOf when float division = inf": func(m *model.MultipleOf, n json.Number) {
			m.FloatDivisionInf = float(n)
		},
		"small multiple of large integer": func(m *model.MultipleOf, n json.Number) {
			m.SmallMultipleOfLargeInteger = integer(n)
		},
	}

	var ran int

	for _, g := range suitetest.Groups(t, os.DirFS(filepath.FromSlash("../../../.."))) {
		if g.File != "multipleOf.json" {
			continue
		}

		setter, ok := set[g.Description]
		assert.Assert(t, ok, "no field for group %q", g.Description)

		for _, c := range g.Tests {
			dec := json.NewDecoder(bytes.NewReader(c.Data))
			dec.UseNumber()

			var data any

			assert.NilError(t, dec.Decode(&data))

			n, isNumber := data.(json.Number)
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
					validator.ErrMultiple,
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
		assert.ErrorIs(t, err, validator.ErrMultiple, f)
	}
}
