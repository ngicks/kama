package tests

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamaruntime/validator"
)

func TestNilReceiver(t *testing.T) {
	for _, stop := range []bool{true, false} {
		var l *model.Locked

		err := l.Validate(stop)
		assert.ErrorIs(t, err, validator.ErrNilReceiver)
		assert.DeepEqual(t, validator.Pointers(err), []string{""})

		var u *model.User

		assert.ErrorIs(t, u.Validate(stop), validator.ErrNilReceiver)
	}
}

func TestLockersInPlace(t *testing.T) {
	valid := &model.Lockers{
		One:   model.Locked{Name: "a"},
		Slice: []model.Locked{{Name: "a"}},
		Array: [2]model.Locked{{Name: "a"}, {Name: "b"}},
		// The walk skips a nil pointer, whose nilness the holder validates.
		Map: map[string]*model.Locked{"a": {Name: "a"}, "nil": nil},
	}
	assert.NilError(t, valid.Validate(false))

	invalid := &model.Lockers{
		Ptr:   &model.Locked{},
		Slice: []model.Locked{{Name: "a"}, {}},
		Array: [2]model.Locked{{}, {Name: "b"}},
		Map:   map[string]*model.Locked{"a": {}},
	}
	invalid.One.SetName("")

	assert.DeepEqual(
		t,
		validator.Pointers(invalid.Validate(false)),
		[]string{"/array/0/name", "/map/a/name", "/one/name", "/ptr/name", "/slice/1/name"},
	)
}
