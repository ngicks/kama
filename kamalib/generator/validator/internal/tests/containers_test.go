package tests

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamaruntime/validator"
)

func TestContainersValid(t *testing.T) {
	c := model.Containers{
		Names:  model.NewList("a", "abc"),
		Counts: model.NewList(0, 1),
		Items:  model.NewList(model.Item{Name: "a"}),
		Scores: model.NewSeq2Box([]string{"a"}, []int{1}),
		Points: model.NewSeq2Box([]float64{0.5}, []int{9}),
		Label:  model.Cell[string]{V: "abc"},
		Size:   model.Maybe[int]{V: 0, Ok: false},
	}

	for _, stop := range []bool{true, false} {
		assert.NilError(t, c.Validate(stop))
	}
}

func TestContainersInvalid(t *testing.T) {
	c := model.Containers{
		Names:  model.NewList("a", "abcd"),
		Counts: model.NewList(-1),
		Items:  model.NewList(model.Item{Name: "a"}, model.Item{}),
		Scores: model.NewSeq2Box([]string{"", "b"}, []int{0, -1}),
		Points: model.NewSeq2Box([]float64{0.5, 1.5}, []int{1, 10}),
		Label:  model.Cell[string]{V: "abcd"},
		Size:   model.Maybe[int]{V: 0, Ok: true},
	}

	err := c.Validate(false)

	for _, tc := range []struct {
		pointer string
		kind    error
	}{
		{"/names/1", validator.ErrLength},
		{"/counts/0", validator.ErrCompare},
		{"/items/1/name", validator.ErrLength},
		{"/scores/", validator.ErrLength},
		{"/scores/b", validator.ErrCompare},
		{"/points/1", validator.ErrCompare},
		{"/label", validator.ErrLength},
		{"/size", validator.ErrCompare},
	} {
		at := validator.At(err, tc.pointer)
		assert.Equal(t, len(at), 1, "%s in %v", tc.pointer, validator.Pointers(err))
		assert.ErrorIs(t, at[0], tc.kind, tc.pointer)
	}

	assert.Equal(t, len(validator.Pointers(err)), 8, "%v", validator.Pointers(err))
}

// TestWalksNested counts the calls of the nested Validate: one per held
// value, none where nested=False switches the walk off, and again below a
// group setting nested=True.
func TestWalksNested(t *testing.T) {
	var on, off, inner, back, ruled, listed, unlisted, held int

	ticks := func(n *int, k int) []model.Tick {
		out := make([]model.Tick, k)
		for i := range out {
			out[i] = model.Tick{Calls: n}
		}

		return out
	}

	w := model.Walks{
		On:       ticks(&on, 2),
		Off:      ticks(&off, 2),
		Inner:    [][]model.Tick{ticks(&inner, 2)},
		Back:     [][]model.Tick{ticks(&back, 2), ticks(&back, 1)},
		Ruled:    [][]model.Tick{ticks(&ruled, 1)},
		Listed:   model.NewList(ticks(&listed, 3)...),
		Unlisted: model.NewList(ticks(&unlisted, 3)...),
		Held:     &model.Tick{Calls: &held},
	}

	assert.NilError(t, w.Validate(false))
	assert.DeepEqual(t,
		[]int{on, off, inner, back, ruled, listed, unlisted, held},
		[]int{2, 0, 0, 3, 0, 3, 0, 1},
	)

	w.Ruled = [][]model.Tick{ticks(&ruled, 2)}
	assert.DeepEqual(t, validator.Pointers(w.Validate(false)), []string{"/ruled/0"})
	assert.Equal(t, ruled, 0)
}
