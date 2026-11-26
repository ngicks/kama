package tests

import (
	"slices"
	"testing"

	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamaruntime/validator"
)

// TestImplementors calls a Validate on the pointer receiver and a Validate
// taking no argument wherever the walk reaches them, and never a Validate
// taking another argument or one nested=False switches off.
func TestImplementors(t *testing.T) {
	good := model.Implementors{
		Pointer: &model.Ptr{},
		Ctx:     model.Ctx{Bad: true},
		Ctxs:    []model.Ctx{{Bad: true}},
		Off:     model.Bare{Bad: true},
	}
	assert.NilError(t, good.Validate(false))

	bad := model.Ptr{Bad: true}
	v := model.Implementors{
		Field:    bad,
		Pointer:  &bad,
		Slice:    []model.Ptr{{}, bad},
		Pointers: []*model.Ptr{nil, &bad},
		Map:      map[string]model.Ptr{"a": bad},
		Option:   opt.Some(bad),
		Und:      und.Defined(bad),
		Bare:     model.Bare{Bad: true},
		Bares:    []model.Bare{{Bad: true}},
		BareMap:  map[string]model.Bare{"b": {Bad: true}},
		BareOpt:  opt.Some(model.Bare{Bad: true}),
		Variadic: model.Variadic{Bad: true},
		VarOpt:   opt.Some(&bad),
		Ctx:      model.Ctx{Bad: true},
		Listed:   model.NewList(model.Ptr{}, bad),
		Off:      model.Bare{Bad: true},
	}

	err := v.Validate(false)

	want := []string{
		"/bare", "/bareMap/b", "/bareOpt", "/bares/0", "/field", "/listed/1", "/map/a",
		"/option", "/pointer", "/pointers/1", "/slice/1", "/und", "/varOpt", "/variadic",
	}
	assert.DeepEqual(t, validator.Pointers(err), want)

	for _, p := range want {
		assert.ErrorContains(t, validator.At(err, p)[0], "bad", p)
	}

	assert.Assert(t, slices.Equal(validator.Pointers(v.Validate(true)), []string{"/field"}))
}
