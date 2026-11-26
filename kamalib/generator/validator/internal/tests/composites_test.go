package tests

import (
	"errors"
	"testing"

	"github.com/ngicks/option/either"
	"github.com/ngicks/option/tuple"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamaruntime/validator"
)

func validComposites() model.Composites[model.Tag] {
	ok := model.Tag{Name: "ok"}

	return model.Composites[model.Tag]{
		E:  either.Left[model.Tag, int](ok),
		R:  either.Ok(ok),
		UE: either.UntaggedEither[int, model.Tag]{Either: either.Right[int](ok)},
		UR: either.UntaggedResult[model.Tag]{Result: either.Ok(ok)},
		T2: tuple.Tuple2[model.Tag, string]{V1: ok},
		T3: tuple.Tuple3[int, model.Tag, model.Tag]{V2: ok, V3: ok},
	}
}

func TestCompositesOnTypeParameter(t *testing.T) {
	bad := model.Tag{}
	failed := errors.New("failed")

	cases := []struct {
		name string
		edit func(c *model.Composites[model.Tag])
		want []string
	}{
		{"valid", func(*model.Composites[model.Tag]) {}, nil},
		{
			// The other side and an error hold nothing to validate.
			"other side",
			func(c *model.Composites[model.Tag]) {
				c.E = either.Right[model.Tag](-1)
				c.R = either.Err[model.Tag](failed)
				c.UE = either.UntaggedEither[int, model.Tag]{
					Either: either.Left[int, model.Tag](-1),
				}
				c.UR = either.UntaggedResult[model.Tag]{Result: either.Err[model.Tag](failed)}
			},
			nil,
		},
		{
			"invalid",
			func(c *model.Composites[model.Tag]) {
				c.E = either.Left[model.Tag, int](bad)
				c.R = either.Ok(bad)
				c.UE = either.UntaggedEither[int, model.Tag]{Either: either.Right[int](bad)}
				c.UR = either.UntaggedResult[model.Tag]{Result: either.Ok(bad)}
				c.T2.V1 = bad
				c.T3.V2 = bad
				c.T3.V3 = bad
			},
			[]string{
				"/tags/e/left/name",
				"/tags/r/ok/name",
				"/tags/t2/V1/name",
				"/tags/t3/V2/name",
				"/tags/t3/V3/name",
				"/tags/ue/name",
				"/tags/ur/name",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := validComposites()
			c.edit(&v)
			got := validator.Pointers(model.CompositesOf{Tags: v}.Validate(false))
			assert.DeepEqual(t, got, c.want)
		})
	}
}

func TestCompositesOnTypeParameterWithoutValidate(t *testing.T) {
	ints := model.Composites[int]{
		E:  either.Left[int, int](-1),
		R:  either.Ok(-1),
		UE: either.UntaggedEither[int, int]{Either: either.Right[int](-1)},
		UR: either.UntaggedResult[int]{Result: either.Ok(-1)},
		T2: tuple.Tuple2[int, string]{V1: -1},
		T3: tuple.Tuple3[int, int, int]{V1: -1, V2: -1, V3: 1},
	}
	// Generated code passes nil for a type without Validate. A zero
	// Composites[Tag] would be left and ok holding a zero Tag.
	assert.NilError(t, model.CompositesOf{Tags: validComposites(), Ints: ints}.Validate(false))

	notNegative := func(v int, _ bool) error {
		if v < 0 {
			return errors.New("negative")
		}

		return nil
	}
	assert.DeepEqual(
		t,
		validator.Pointers(ints.Validate(false, notNegative)),
		[]string{"/e/left", "/r/ok", "/t2/V1", "/t3/V2", "/ue", "/ur"},
	)
	assert.Equal(t, len(validator.Pointers(ints.Validate(true, notNegative))), 1)
}

func TestConcreteComposites(t *testing.T) {
	ok := model.Tag{Name: "ok"}
	bad := model.Tag{}

	v := model.ConcreteComposites{
		E: either.Right[model.Tag](ok),
		T: tuple.Tuple2[int, []model.Tag]{V1: 1, V2: []model.Tag{ok}},
	}
	assert.NilError(t, v.Validate(false))

	v = model.ConcreteComposites{
		E: either.Right[model.Tag](bad),
		T: tuple.Tuple2[int, []model.Tag]{V2: []model.Tag{ok, bad}},
	}
	assert.DeepEqual(
		t,
		validator.Pointers(v.Validate(false)),
		[]string{"/e/right/name", "/t/V2/1/name"},
	)
}
