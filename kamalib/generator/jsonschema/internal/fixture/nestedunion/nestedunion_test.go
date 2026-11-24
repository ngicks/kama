package nestedunion

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

// TestDecodeTime decodes unions nested 24 deep. The first member of Any
// accepts every level, and the members of Tagged are told apart by the
// const of kind, so each level decodes the rest once. A decoder trying
// every member would decode the innermost value 2^24 times.
func TestDecodeTime(t *testing.T) {
	const depth = 24

	cases := []struct {
		name string
		in   string
	}{
		{"anyOf", anyChain(depth, `{"l":"x"}`)},
		{"oneOf told apart by a const", taggedChain(depth)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()

			var v Chains
			assert.NilError(t, json.Unmarshal([]byte(tc.in), &v))

			elapsed := time.Since(start)
			assert.Assert(t, elapsed < time.Second, "took %v", elapsed)
		})
	}
}

// BenchmarkDecode decodes chains of unions. A level of Overlap passes the
// prefilter of both members, and the member whose l rejects the value
// fails only after decoding the rest of the chain under c, so the time
// doubles per level. So does an anyOf whose innermost value no member
// accepts, since every member of every level fails.
func BenchmarkDecode(b *testing.B) {
	cases := []struct {
		name  string
		in    func(depth int) string
		valid bool
	}{
		{"anyOf", func(d int) string { return anyChain(d, `{"l":"x"}`) }, true},
		{"anyOf failing", func(d int) string { return anyChain(d, `{"l":1,"r":1}`) }, false},
		{"oneOf told apart", taggedChain, true},
		{"oneOf overlapping", overlapChain, true},
	}
	for _, tc := range cases {
		for _, depth := range []int{8, 12, 16} {
			b.Run(fmt.Sprintf("%s/%d", tc.name, depth), func(b *testing.B) {
				in := []byte(tc.in(depth))

				for b.Loop() {
					var v Chains
					if err := json.Unmarshal(in, &v); (err == nil) != tc.valid {
						b.Fatalf("valid %t, got error %v", tc.valid, err != nil)
					}
				}
			})
		}
	}
}

func anyChain(depth int, innermost string) string {
	return `{"any":` + strings.Repeat(`{"c":`, depth) + innermost + strings.Repeat(`}`, depth) + `}`
}

func taggedChain(depth int) string {
	return `{"tagged":` + strings.Repeat(`{"kind":"left","c":`, depth) + `{"kind":"left"}` +
		strings.Repeat(`}`, depth) + `}`
}

// overlapChain puts l after c, so that OverlapRight decodes c before it
// rejects l.
func overlapChain(depth int) string {
	return `{"overlap":` + strings.Repeat(`{"c":`, depth) + `{"l":"x"}` +
		strings.Repeat(`,"l":"x"}`, depth) + `}`
}
