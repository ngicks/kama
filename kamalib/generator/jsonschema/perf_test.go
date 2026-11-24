package jsonschema

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// TestGenerateTime generates the hostile shapes of the benchmarks within
// budgets far above what they take, so that a cost that grows faster than
// the document fails here rather than in a slow build.
func TestGenerateTime(t *testing.T) {
	if testing.Short() {
		t.Skip("generates large documents")
	}

	cases := []struct {
		name   string
		schema string
		budget time.Duration
	}{
		{"200 definitions", manyDefs(200), 5 * time.Second},
		{"union chain of 20", unionChain(20), 2 * time.Second},
		{"300 nested objects", nested(300), 5 * time.Second},
		{"reference chain of 500", refChain(500), 2 * time.Second},
		{"unevaluated chain of 20", unevaluatedChain(20), 2 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := decodeSchema(t, tc.schema)

			start := time.Now()
			_, _, err := Generate(doc, Options{Package: "p", TypeName: "Root"})
			elapsed := time.Since(start)

			assert.NilError(t, err)
			assert.Assert(t, elapsed < tc.budget, "took %v, budget %v", elapsed, tc.budget)
		})
	}
}

func BenchmarkGenerateDefs(b *testing.B) {
	for _, n := range []int{50, 100, 200, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) { benchGenerate(b, manyDefs(n)) })
	}
}

// BenchmarkGenerateDefsRing stops at 200 definitions: every field asks
// whether it closes a cycle, and on one cycle through every definition each
// answer walks all of them, so 1000 take minutes.
func BenchmarkGenerateDefsRing(b *testing.B) {
	for _, n := range []int{50, 100, 200} {
		b.Run(fmt.Sprint(n), func(b *testing.B) { benchGenerate(b, defsRing(n)) })
	}
}

func BenchmarkGenerateUnionChain(b *testing.B) {
	for _, k := range []int{8, 10, 12, 14, 20} {
		b.Run(fmt.Sprint(k), func(b *testing.B) { benchGenerate(b, unionChain(k)) })
	}
}

func BenchmarkGenerateNested(b *testing.B) {
	for _, d := range []int{100, 300} {
		b.Run(fmt.Sprint(d), func(b *testing.B) { benchGenerate(b, nested(d)) })
	}
}

// BenchmarkGenerateUnevaluatedChain doubles the paths to the last
// definition per definition, under an unevaluatedProperties that needs what
// every definition evaluated.
func BenchmarkGenerateUnevaluatedChain(b *testing.B) {
	for _, n := range []int{10, 20, 40} {
		b.Run(fmt.Sprint(n), func(b *testing.B) { benchGenerate(b, unevaluatedChain(n)) })
	}
}

func BenchmarkGenerateRefChain(b *testing.B) {
	for _, n := range []int{100, 500} {
		b.Run(fmt.Sprint(n), func(b *testing.B) { benchGenerate(b, refChain(n)) })
	}
}

func benchGenerate(b *testing.B, schema string) {
	doc := decodeSchema(b, schema)

	for b.Loop() {
		if _, _, err := Generate(doc, Options{Package: "p", TypeName: "Root"}); err != nil {
			b.Fatal(err)
		}
	}
}

func decodeSchema(t testing.TB, schema string) *jsonschema.Schema {
	t.Helper()

	var doc jsonschema.Schema
	assert.NilError(t, jsonv2.Unmarshal([]byte(schema), &doc))

	return &doc
}

// manyDefs returns a root object holding the first of n definitions. Each
// definition is an object of 7 properties, 4 of which reach other
// definitions: by value and through a nullable union within its block of
// 8, which makes cycles of pointers, and through a slice and a map across
// the document.
func manyDefs(n int) string {
	defs := make([]string, n)
	for i := range defs {
		block := i / 8 * 8
		defs[i] = def(i,
			min(block+(i+1)%8, n-1), (i+2)%n, (i+3)%n, min(block+(i+3)%8, n-1))
	}

	return defsDoc(defs)
}

// defsRing is manyDefs with every definition on one cycle of values held
// by value, so that every definition reaches every other by value.
func defsRing(n int) string {
	defs := make([]string, n)
	for i := range defs {
		defs[i] = def(i, (i+1)%n, (i+2)%n, (i+3)%n, (i+n/2)%n)
	}

	return defsDoc(defs)
}

func def(i, next, list, mapped, maybe int) string {
	return fmt.Sprintf(`"D%d":{"type":"object","required":["s","next"],"properties":{`+
		`"s":{"type":"string"},`+
		`"i":{"type":"integer"},`+
		`"next":{"$ref":"#/$defs/D%d"},`+
		`"list":{"type":"array","items":{"$ref":"#/$defs/D%d"}},`+
		`"map":{"additionalProperties":{"$ref":"#/$defs/D%d"}},`+
		`"maybe":{"anyOf":[{"$ref":"#/$defs/D%d"},{"type":"null"}]},`+
		`"b":{"type":"boolean"}}}`,
		i, next, list, mapped, maybe)
}

func defsDoc(defs []string) string {
	return `{"type":"object","properties":{"d":{"$ref":"#/$defs/D0"}},"$defs":{` +
		strings.Join(defs, ",") + `}}`
}

// unionChain returns a root holding U<k>, where U<i> is an anyOf of U<i-1>
// and an allOf of U<i-1> alone, so that the members of every union reach
// U0 along 2^i paths.
func unionChain(k int) string {
	defs := []string{`"U0":{"type":"object","properties":{"a":{"type":"string"}}}`}
	for i := 1; i <= k; i++ {
		defs = append(defs, fmt.Sprintf(
			`"U%d":{"anyOf":[{"$ref":"#/$defs/U%d"},{"allOf":[{"$ref":"#/$defs/U%d"}]}]}`,
			i, i-1, i-1,
		))
	}

	return fmt.Sprintf(
		`{"type":"object","properties":{"u":{"$ref":"#/$defs/U%d"}},"$defs":{%s}}`,
		k, strings.Join(defs, ","),
	)
}

// nested returns objects nested depth deep, each holding the next under
// the property c.
func nested(depth int) string {
	return strings.Repeat(`{"type":"object","properties":{"c":`, depth) +
		`{"type":"string"}` + strings.Repeat(`}}`, depth)
}

// refChain returns a root holding R<n-1>, where R<i> refers to R<i-1> and
// R0 is an object.
func refChain(n int) string {
	defs := []string{`"R0":{"type":"object","properties":{"a":{"type":"string"}}}`}
	for i := 1; i < n; i++ {
		defs = append(defs, fmt.Sprintf(`"R%d":{"$ref":"#/$defs/R%d"}`, i, i-1))
	}

	return fmt.Sprintf(
		`{"type":"object","properties":{"r":{"$ref":"#/$defs/R%d"}},"$defs":{%s}}`,
		n-1, strings.Join(defs, ","),
	)
}

// unevaluatedChain returns a root holding D0 and unevaluatedProperties
// false, where D<i> is an allOf of a reference to D<i+1> and of properties
// next to another reference to D<i+1>, so that D0 reaches D<n> along 2^n
// paths.
func unevaluatedChain(n int) string {
	defs := []string{fmt.Sprintf(`"D%d":{"properties":{"z":true}}`, n)}
	for i := range n {
		next := fmt.Sprintf(`"$ref":"#/$defs/D%d"`, i+1)
		defs = append(defs, fmt.Sprintf(
			`"D%d":{"allOf":[{%s},{"properties":{"p%d":true},%s}]}`, i, next, i, next,
		))
	}

	return `{"$ref":"#/$defs/D0","unevaluatedProperties":false,"$defs":{` +
		strings.Join(defs, ",") + `}}`
}
