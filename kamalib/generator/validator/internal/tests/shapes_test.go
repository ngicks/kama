package tests

import (
	"testing"
	"time"

	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamaruntime/validator"
)

func TestShapes(t *testing.T) {
	valid := model.Shapes[int]{
		E:    elastic.FromOptions(opt.Some(model.Tag{Name: "e"}), opt.None[model.Tag]()),
		A:    [2]model.Tag{{Name: "a"}, {Name: "b"}},
		N:    []map[string][]*model.Tag{{"k": {{Name: "n"}, nil}}},
		BP:   model.Box[*model.Tag]{Items: []*model.Tag{{Name: "p"}, nil}},
		Code: "c",
		At:   time.Unix(1, 0),
	}
	valid.S.T = model.Tag{Name: "s"}

	for _, stop := range []bool{true, false} {
		assert.NilError(t, valid.Validate(stop, nil))
	}

	invalid := model.Shapes[int]{
		E:  elastic.FromOptions(opt.Some(model.Tag{Name: "e"}), opt.Some(model.Tag{})),
		N:  []map[string][]*model.Tag{{"k": {{Name: "n"}, {}}}},
		BP: model.Box[*model.Tag]{Items: []*model.Tag{{}}},
	}

	assert.DeepEqual(t, validator.Pointers(invalid.Validate(false, nil)), []string{
		"/a/0/name",
		"/a/1/name",
		"/at",
		"/bp/items/0/name",
		"/code",
		"/e/1/name",
		"/n/0/k/1/name",
		"/s/t/name",
	})
	assert.DeepEqual(t, validator.Pointers(invalid.Validate(true, nil)), []string{"/e/1/name"})
}

// TestElasticElements walks the options of elastic.Elastic elements of a
// slice, a map and an array.
func TestElasticElements(t *testing.T) {
	ok := elastic.FromOptions(opt.Some(model.Tag{Name: "x"}), opt.None[model.Tag]())
	bad := elastic.FromOptions(opt.None[model.Tag](), opt.Some(model.Tag{}))

	valid := model.Elastics{
		S: []elastic.Elastic[model.Tag]{ok},
		M: map[string]elastic.Elastic[model.Tag]{"k": ok},
		A: [2]elastic.Elastic[model.Tag]{ok, elastic.Null[model.Tag]()},
	}

	for _, stop := range []bool{true, false} {
		assert.NilError(t, valid.Validate(stop))
	}

	invalid := model.Elastics{
		S: []elastic.Elastic[model.Tag]{ok, bad},
		M: map[string]elastic.Elastic[model.Tag]{"k": bad},
		A: [2]elastic.Elastic[model.Tag]{bad, ok},
	}

	assert.DeepEqual(t, validator.Pointers(invalid.Validate(false)), []string{
		"/a/0/1/name",
		"/m/k/1/name",
		"/s/1/1/name",
	})
	assert.DeepEqual(t, validator.Pointers(invalid.Validate(true)), []string{"/s/1/1/name"})
}

// TestTypeValidators runs the validators of Rank and Ranks, written on
// the types, wherever Values holds them.
func TestTypeValidators(t *testing.T) {
	valid := model.Values{
		L: 1,
		P: new(model.Rank(2)),
		O: opt.Some(model.Rank(3)),
		U: und.Defined(model.Rank(4)),
		S: model.Ranks{1, 2},
		M: map[string]model.Rank{"k": 5},
		B: model.Box[model.Rank]{Items: []model.Rank{1}, One: und.Null[model.Rank]()},
		F: 1,
	}

	for _, stop := range []bool{true, false} {
		assert.NilError(t, valid.Validate(stop))
	}

	invalid := model.Values{
		P: new(model.Rank(6)),
		O: opt.Some(model.Rank(0)),
		U: und.Defined(model.Rank(9)),
		S: model.Ranks{1, 1, 7, 2},
		M: map[string]model.Rank{"k": 0},
		B: model.Box[model.Rank]{Items: []model.Rank{1, 8}, One: und.Defined(model.Rank(0))},
		F: 6,
	}

	err := invalid.Validate(false)
	assert.DeepEqual(t, validator.Pointers(err), []string{
		"/b/items/1",
		"/b/one",
		"/f",
		"/l",
		"/m/k",
		"/o",
		"/p",
		"/s",
		"/s/2",
		"/u",
	})
	// The validators of Ranks run before those of its elements.
	assert.Equal(t, len(validator.At(err, "/s")), 2)
	assert.ErrorIs(t, validator.At(err, "/s")[0], validator.ErrLength)
	assert.ErrorIs(t, validator.At(err, "/s")[1], validator.ErrUnique)
	assert.Equal(t, len(validator.At(err, "/f")), 2)
	assert.DeepEqual(t, validator.Pointers(invalid.Validate(true)), []string{"/l"})
}
