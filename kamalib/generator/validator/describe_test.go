package validator

import (
	"testing"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"gotest.tools/v3/assert"
)

// TestDescribedContainers reaches the values a generic container of the
// run holds through the method a description names, for its role items and
// for the nested walk, and switches the nested walk off by nested=False.
func TestDescribedContainers(t *testing.T) {
	out := generateAndBuild(t, map[string]string{
		"a.go": `package x

import (
	"iter"

	"github.com/ngicks/opiter"
)

//kama:validate hook=False
type Item struct {
	//kama:validate length(ge=1)
	Name string
}

type List[T any] struct{ items []T }

func (l List[T]) All() iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, v := range l.items {
			if !yield(v) {
				return
			}
		}
	}
}

func (l List[T]) Seq() opiter.Seq[T] { return opiter.Seq[T](l.All()) }

type Dict[K comparable, V any] struct{ m map[K]V }

func (d Dict[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range d.m {
			if !yield(k, v) {
				return
			}
		}
	}
}

type Box[T any] struct{ v T }

func (b Box[T]) Value() T { return b.v }

type Maybe[T any] struct {
	v  T
	ok bool
}

func (m Maybe[T]) Get() (T, bool) { return m.v, m.ok }

//kama:validate hook=False
type A struct {
	//kama:validate iter="All", v=length(le=3)
	L List[string]
	//kama:validate iter="All"
	I List[Item]
	//kama:validate iter="Seq", v=length(le=3)
	S List[string]
	//kama:validate iter2="All", k=length(ge=1), v=cmp(ge=0)
	D Dict[string, int]
	//kama:validate unwrap="Value", v=length(le=3)
	B Box[string]
	//kama:validate unwrap="Get", v=cmp(ge=1)
	M Maybe[int]
	//kama:validate nested=False
	N []Item
	//kama:validate e=g(nested=False)
	G [][]Item
}
`,
	})
	fn := gentest.Funcs(t, out["a.kama_validate.go"])["A.Validate"]

	assertContains(t, fn,
		"{\n\t\ti := 0\n\t\tfor e := range v.L.All() {\n\t\t\t"+
			check(`validator.Field(validator.Index(kamaValidateLength(len(e), "<=", 3), i), "L")`),
		"for e := range v.I.All() {\n\t\t\t"+
			check(`validator.Field(validator.Index(e.Validate(stopOnFirstError), i), "I")`),
		"for e := range v.S.Seq() {",
		"for k := range v.D.All() {\n\t\t"+
			check(`validator.Field(validator.Key(kamaValidateLength(len(k), ">=", 1), k), "D")`),
		"for k, e := range v.D.All() {\n\t\t"+
			check(`validator.Field(validator.Key(kamaValidateCmp(e, ">=", 0), k), "D")`),
		check(`validator.Field(kamaValidateLength(len(v.B.Value()), "<=", 3), "B")`),
		"u, ok := v.M.Get()\n\t\tif ok {\n\t\t\t"+
			check(`validator.Field(kamaValidateCmp(u, ">=", 1), "M")`),
	)
	assertNotContains(t, fn, "v.N", "v.G")
}

func TestDescribedErrors(t *testing.T) {
	types := "type L struct{}\n\nfunc (L) All() []int { return nil }\n\n" +
		"func (L) Pair() (int, int) { return 0, 0 }\n\n"

	cases := []struct {
		name string
		src  string
		msgs []string
	}{
		{
			name: "missing method",
			src:  "\t//kama:validate iter=\"Each\"\n\tF L\n",
			msgs: []string{`iter="Each": `, "L has no method Each;" +
				" want Each func() S, S a function func(yield func(T) bool)"},
		},
		{
			name: "wrong signature",
			src:  "\t//kama:validate iter=\"All\", v=cmp(ge=0)\n\tF L\n",
			msgs: []string{`iter="All": method All of`, "has signature func() []int;" +
				" want func() S, S a function func(yield func(T) bool)"},
		},
		{
			name: "unwrap of two values",
			src:  "\t//kama:validate unwrap=\"Pair\", v=cmp(ge=0)\n\tF L\n",
			msgs: []string{"has signature func() (int, int); want func() T or func() (T, bool)"},
		},
		{
			name: "two descriptions",
			src:  "\t//kama:validate iter=\"All\", unwrap=\"All\"\n\tF L\n",
			msgs: []string{"iter and unwrap describe one value; write one of them"},
		},
		{
			name: "method name no string",
			src:  "\t//kama:validate iter=All\n\tF L\n",
			msgs: []string{`iter=All; want iter="<method name>"`},
		},
		{
			name: "nested no bool",
			src:  "\t//kama:validate nested=\"no\"\n\tF []L\n",
			msgs: []string{`nested="no"; want nested=True or nested=False`},
		},
		{
			name: "description on a type",
			src:  "\tF int\n}\n\n//kama:validate iter=\"All\"\ntype M []int\n\ntype Z struct {\n",
			msgs: []string{"iter is written on a field or inside g(...)"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{
				"a.go": "package x\n\n" + types + "//kama:validate hook=False\ntype A struct {\n" +
					tc.src + "}\n",
			}, testRun{})
			assert.Assert(t, err != nil)

			for _, msg := range tc.msgs {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}
