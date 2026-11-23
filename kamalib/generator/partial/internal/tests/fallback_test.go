package tests

import (
	"encoding/json/v2"
	"testing"

	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/partial/internal/fixture/fallback"
	mergefallback "github.com/ngicks/kama/kamalib/generator/partial/internal/fixturemerge/fallback"
)

func TestFallbackDecode(t *testing.T) {
	var p fallback.FallbackPartial
	assert.NilError(t, json.Unmarshal([]byte(`{"name":"n","nmae":"x"}`), &p))
	equal(t, p, fallback.FallbackPartial{
		Name:    und.Defined("n"),
		Unknown: map[string]any{"nmae": "x"},
	})

	b, err := json.Marshal(p)
	assert.NilError(t, err)
	assert.Equal(t, string(b), `{"name":"n","nmae":"x"}`)
}

func TestFallbackRoundTrip(t *testing.T) {
	roundTrip[fallback.Fallback, fallback.FallbackPartial](
		t, fallback.Fallback{Name: "n", Unknown: map[string]any{"k": 1}},
	)
	roundTrip[fallback.Embedding, fallback.EmbeddingPartial](
		t, fallback.Embedding{Unknown: map[string]any{}, Id: 1},
	)
	roundTrip[fallback.PointerEmbedding, fallback.PointerEmbeddingPartial](
		t, fallback.PointerEmbedding{Fallback: &fallback.Fallback{Unknown: map[string]any{"k": 1}}},
	)
}

func TestFallbackMerge(t *testing.T) {
	l := fallback.FallbackPartial{Unknown: map[string]any{"a": 1, "b": 2}}

	equal(t, l.Merge(fallback.FallbackPartial{}), l)
	equal(
		t,
		l.Merge(fallback.FallbackPartial{Unknown: map[string]any{"b": 3}}),
		fallback.FallbackPartial{Unknown: map[string]any{"b": 3}},
	)
	equal(
		t,
		l.Merge(fallback.FallbackPartial{Unknown: map[string]any{}}),
		fallback.FallbackPartial{Unknown: map[string]any{}},
	)

	ml := mergefallback.FallbackPartial{
		Tags:    und.Defined([]string{"a"}),
		Unknown: map[string]any{"a": 1},
	}
	mr := mergefallback.FallbackPartial{
		Tags:    und.Defined([]string{"b"}),
		Unknown: map[string]any{"b": 2},
	}
	equal(t, ml.Merge(mr), mergefallback.FallbackPartial{
		Tags:    und.Defined([]string{"a", "b"}),
		Unknown: map[string]any{"b": 2},
	})
}

func TestFallbackEmbeddedPointer(t *testing.T) {
	var p fallback.PointerEmbeddingPartial
	assert.Assert(t, p.ToValue().Fallback == nil)

	p.Unknown = map[string]any{"k": 1}
	equal(t, p.ToValue().Fallback, &fallback.Fallback{Unknown: map[string]any{"k": 1}})
}

func TestFallbackFieldMask(t *testing.T) {
	p := fallback.MaskedPartial{Unknown: map[string]any{"k": 1}}
	assert.Assert(t, p.FieldMask() == nil)

	err := p.FromValueWithMask(fallback.Masked{}, []string{"Unknown"})
	assert.ErrorContains(t, err, "unknown field Unknown")
}
