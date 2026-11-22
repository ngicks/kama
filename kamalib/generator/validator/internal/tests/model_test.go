package tests

import (
	"errors"
	"testing"

	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamalib/pkg/validation"
)

func validUser() model.User {
	email := "a@example.com"

	return model.User{
		Name:    "a",
		Email:   &email,
		Address: model.Address{City: "c"},
		Tags:    []model.Tag{{Name: "t"}},
		Pets:    map[string]*model.Pet{"cat": {Name: "tama"}, "none": nil},
		Nick:    opt.Some("nick"),
		Box:     model.Box[model.Tag]{Items: []model.Tag{{Name: "b"}}},
	}
}

// invalidUser breaks every check of User once.
func invalidUser() model.User {
	return model.User{
		Tags: []model.Tag{{Name: "t"}, {}},
		Pets: map[string]*model.Pet{"cat": {}},
		Box: model.Box[model.Tag]{
			Items: []model.Tag{{Name: "b"}, {}},
			One:   und.Defined(model.Tag{}),
		},
		Age: -1,
	}
}

func TestValid(t *testing.T) {
	for _, stop := range []bool{true, false} {
		assert.NilError(t, validUser().Validate(stop))
	}
}

func TestCollectEveryError(t *testing.T) {
	err := invalidUser().Validate(false)

	assert.DeepEqual(t, validation.Pointers(err), []string{
		"/Pets/cat/name",
		"/address/city",
		"/age",
		"/box/items/1/name",
		"/box/one/name",
		"/email",
		"/name",
		"/nick",
		"/tags/1/name",
	})
	assert.ErrorIs(t, err, validation.ErrRequired)
	assert.ErrorIs(t, err, model.ErrNegative)

	at := validation.At(err, "/age")
	assert.Equal(t, len(at), 1)
	assert.ErrorIs(t, at[0], model.ErrNegative)

	assert.Equal(t, len(validation.Under(err, "/box")), 2)
	assert.ErrorContains(t, validation.At(err, "/email")[0], "want a value, got nil")
	assert.ErrorContains(t, validation.At(err, "/name")[0], `want a non-zero value, got ""`)
}

func TestStopOnFirstError(t *testing.T) {
	err := invalidUser().Validate(true)

	// The first error is the one of the first field.
	assert.DeepEqual(t, validation.Pointers(err), []string{"/name"})
	assert.ErrorIs(t, err, validation.ErrRequired)

	u := validUser()
	u.Tags = append(u.Tags, model.Tag{}, model.Tag{})
	assert.DeepEqual(t, validation.Pointers(u.Validate(true)), []string{"/tags/1/name"})
	assert.DeepEqual(
		t,
		validation.Pointers(u.Validate(false)),
		[]string{"/tags/1/name", "/tags/2/name"},
	)
}

func TestGeneric(t *testing.T) {
	box := model.Box[model.Tag]{Items: []model.Tag{{Name: "a"}, {}}}
	validateTag := func(v model.Tag, stop bool) error { return v.Validate(stop) }

	assert.DeepEqual(
		t,
		validation.Pointers(box.Validate(false, validateTag)),
		[]string{"/items/1/name"},
	)
	// A nil function leaves the values of the type parameter unchecked.
	assert.NilError(t, box.Validate(false, nil))

	ints := model.Box[int]{Items: []int{1, -1}}
	notNegative := func(v int, _ bool) error {
		if v < 0 {
			return errors.New("negative")
		}

		return nil
	}
	assert.DeepEqual(
		t,
		validation.Pointers(ints.Validate(false, notNegative)),
		[]string{"/items/1"},
	)
}

func TestDependants(t *testing.T) {
	bad := invalidUser()
	team := model.Team{Members: []model.User{validUser(), bad}, Lead: &bad}

	err := team.Validate(false)
	assert.Equal(t, len(validation.Under(err, "/members/1")), 9)
	assert.Equal(t, len(validation.Under(err, "/lead")), 9)
	assert.Equal(t, len(validation.Under(err, "/members/0")), 0)

	assert.NilError(t, model.Team{}.Validate(false))
	assert.DeepEqual(
		t,
		validation.Pointers(model.Roster{validUser(), {Name: "x"}}.Validate(true)),
		[]string{"/1/email"},
	)
}
