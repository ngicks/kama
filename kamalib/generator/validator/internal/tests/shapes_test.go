package tests

import (
	"testing"
	"time"

	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamalib/pkg/validation"
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

	assert.DeepEqual(t, validation.Pointers(invalid.Validate(false, nil)), []string{
		"/a/0/name",
		"/a/1/name",
		"/at",
		"/bp/items/0/name",
		"/code",
		"/e/1/name",
		"/n/0/k/1/name",
		"/s/t/name",
	})
	assert.DeepEqual(t, validation.Pointers(invalid.Validate(true, nil)), []string{"/e/1/name"})
}
