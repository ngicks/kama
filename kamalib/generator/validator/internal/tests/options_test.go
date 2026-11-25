package tests

import (
	"errors"
	"testing"

	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamaruntime/validator"
)

func TestHeldString(t *testing.T) {
	cases := []struct {
		name string
		v    und.Und[string]
		want []string
	}{
		{"undefined", und.Undefined[string](), nil},
		{"null", und.Null[string](), nil},
		{"valid", und.Defined("ab"), nil},
		{"empty", und.Defined(""), []string{"/length", "/pattern"}},
		{"long", und.Defined("abcd"), []string{"/length", "/runes"}},
		{"upper", und.Defined("AB"), []string{"/pattern"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := model.Held{Length: c.v, Runes: c.v, Pattern: c.v}
			assert.DeepEqual(t, validator.Pointers(h.Validate(false)), c.want)
		})
	}

	err := model.Held{Length: und.Defined("")}.Validate(false)
	assert.Assert(t, errors.Is(err, validator.ErrLength))
	err = model.Held{Runes: und.Defined("あいう")}.Validate(false)
	assert.Assert(t, errors.Is(err, validator.ErrLength))
	err = model.Held{Pattern: und.Defined("A")}.Validate(false)
	assert.Assert(t, errors.Is(err, validator.ErrPattern))
}

func TestOptionsOnTypeParameter(t *testing.T) {
	bad := model.Tag{}

	cases := []struct {
		name string
		v    model.Options[model.Tag]
		want []string
	}{
		{"zero", model.Options[model.Tag]{}, nil},
		{
			"none undefined",
			model.Options[model.Tag]{
				O: opt.None[model.Tag](),
				U: und.Undefined[model.Tag](),
				E: elastic.Undefined[model.Tag](),
			},
			nil,
		},
		{
			"null",
			model.Options[model.Tag]{U: und.Null[model.Tag](), E: elastic.Null[model.Tag]()},
			nil,
		},
		{
			"valid",
			model.Options[model.Tag]{
				O: opt.Some(model.Tag{Name: "o"}),
				U: und.Defined(model.Tag{Name: "u"}),
				E: elastic.FromValues(model.Tag{Name: "e"}),
			},
			nil,
		},
		{
			"invalid",
			model.Options[model.Tag]{
				O: opt.Some(bad),
				U: und.Defined(bad),
				E: elastic.FromOptions(
					opt.Some(model.Tag{Name: "e"}),
					opt.None[model.Tag](),
					opt.Some(bad),
				),
			},
			[]string{"/tags/e/2/name", "/tags/o/name", "/tags/u/name"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := model.OptionsOf{Tags: c.v}
			assert.DeepEqual(t, validator.Pointers(v.Validate(false)), c.want)
		})
	}
}

func TestOptionsOnTypeParameterWithoutValidate(t *testing.T) {
	ints := model.Options[int]{
		O: opt.Some(-1),
		U: und.Defined(-1),
		E: elastic.FromOptions(opt.Some(1), opt.None[int](), opt.Some(-1)),
	}
	// Generated code passes nil for a type without Validate.
	assert.NilError(t, model.OptionsOf{Ints: ints}.Validate(false))

	notNegative := func(v int, _ bool) error {
		if v < 0 {
			return errors.New("negative")
		}

		return nil
	}
	assert.DeepEqual(
		t,
		validator.Pointers(ints.Validate(false, notNegative)),
		[]string{"/e/2", "/o", "/u"},
	)
	assert.Equal(t, len(validator.Pointers(ints.Validate(true, notNegative))), 1)

	empty := model.Options[int]{U: und.Null[int](), E: elastic.Null[int]()}
	assert.NilError(t, empty.Validate(false, notNegative))
}
