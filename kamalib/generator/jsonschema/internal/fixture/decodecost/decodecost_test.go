package decodecost

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

// TestDecodeTime decodes values nested deep in each recursive shape. A
// decoder validating every level in full, or a union decoding the rest of
// the value once per member, costs time growing as the cube of the depth or
// as two to the power of it, which the budgets catch.
func TestDecodeTime(t *testing.T) {
	if testing.Short() {
		t.Skip("decodes deep values")
	}

	cases := []struct {
		name   string
		member string
		level  string
		inner  string
		depth  int
	}{
		{"nested objects", "plain", `{"v":1,"next":`, `{"v":1}`, 256},
		{"not over each level", "negated", `{"v":1,"next":`, `{"v":1}`, 800},
		{"oneOf told apart by a property", "chosen", `{"a":-1,"next":`, `{"a":-1}`, 64},
		{"unevaluatedProperties", "closed", `{"v":1,"next":`, `{"v":1}`, 256},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := `{"` + tc.member + `":` + strings.Repeat(tc.level, tc.depth) + tc.inner +
				strings.Repeat(`}`, tc.depth) + `}`

			start := time.Now()

			var v Costs
			assert.NilError(t, json.Unmarshal([]byte(in), &v))

			elapsed := time.Since(start)
			assert.Assert(t, elapsed < time.Second, "depth %d took %v", tc.depth, elapsed)
		})
	}
}

// TestDecodeRejects decodes values whose innermost level the schema
// rejects, so that skipping the levels below the one being converted loses
// no error.
func TestDecodeRejects(t *testing.T) {
	for _, in := range []string{
		`{"negated":{"next":{"next":{"bad":1}}}}`,
		`{"chosen":{"a":1,"next":{"a":-1,"next":{"a":"x"}}}}`,
		`{"closed":{"next":{"next":{"other":1}}}}`,
		`{"plain":{"next":{"next":{"v":"x"}}}}`,
	} {
		var v Costs
		assert.Assert(t, json.Unmarshal([]byte(in), &v) != nil, "%s", in)
	}
}
