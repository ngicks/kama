package tests

import (
	"database/sql"
	"math/big"
	"net/netip"
	"testing"
	"time"
	"unique"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/internal/fixture/model"
	"github.com/ngicks/kama/kamaruntime/validator"
)

func validStd() *model.Std {
	return &model.Std{
		Name:    sql.NullString{String: "a", Valid: true},
		Count:   sql.NullInt64{Int64: 0, Valid: true},
		Small:   sql.NullInt32{Int32: 99, Valid: true},
		Short:   sql.NullInt16{Int16: -1, Valid: true},
		Octet:   sql.NullByte{Byte: 1, Valid: true},
		Real:    sql.NullFloat64{Float64: 1.25, Valid: true},
		Seen:    sql.NullTime{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		Until:   new(time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC)),
		Tag:     sql.Null[model.Tag]{V: model.Tag{Name: "t"}, Valid: true},
		Code:    unique.Make("abc"),
		Kind:    unique.Make(model.Tag{Name: "k"}),
		At:      time.Date(2029, 12, 31, 23, 59, 59, 0, time.UTC),
		Since:   time.Unix(1, 0),
		Timeout: 5 * time.Minute,
		Step:    3 * time.Millisecond,
		Int:     big.NewInt(1),
		Triple:  big.NewInt(-9),
		Ratio:   big.NewRat(1, 2),
		Float:   big.NewFloat(0.09375),
		Addr:    netip.MustParseAddr("192.0.2.1"),
		Prefix:  netip.MustParsePrefix("192.0.2.0/24"),
	}
}

func TestStdValid(t *testing.T) {
	assert.NilError(t, validStd().Validate(false))

	// A null value, the zero Handle and nil pointers of math/big hold no
	// value the rules on the value test.
	s := validStd()
	s.Count, s.Small, s.Tag = sql.NullInt64{}, sql.NullInt32{}, sql.Null[model.Tag]{}
	s.Short, s.Octet, s.Real = sql.NullInt16{}, sql.NullByte{}, sql.NullFloat64{}
	s.Seen, s.Until = sql.NullTime{}, nil
	s.Code, s.Kind = unique.Handle[string]{}, unique.Handle[model.Tag]{}
	s.Int, s.Triple, s.Ratio, s.Float = nil, nil, nil, nil
	assert.NilError(t, s.Validate(false))
}

func TestStdInvalid(t *testing.T) {
	huge, _ := new(big.Int).SetString("1000000000000000000000000000000", 10)
	inf := new(big.Float).SetInf(false)

	s := &model.Std{
		Name:    sql.NullString{String: "a"},
		Count:   sql.NullInt64{Int64: -1, Valid: true},
		Small:   sql.NullInt32{Int32: 100, Valid: true},
		Gone:    sql.NullBool{Valid: true},
		Short:   sql.NullInt16{Int16: -2, Valid: true},
		Octet:   sql.NullByte{Valid: true},
		Real:    sql.NullFloat64{Float64: 1.5, Valid: true},
		Seen:    sql.NullTime{Time: time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		Until:   new(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)),
		Never:   time.Unix(0, 0),
		Tag:     sql.Null[model.Tag]{Valid: true},
		Code:    unique.Make("abcd"),
		Kind:    unique.Make(model.Tag{}),
		At:      time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		Timeout: time.Second - 1,
		Step:    time.Millisecond + 1,
		Int:     huge,
		Triple:  big.NewInt(4),
		Ratio:   big.NewRat(3, 2),
		Float:   inf,
		Unset:   netip.MustParseAddr("::1"),
	}

	err := s.Validate(false)
	assert.DeepEqual(t, validator.Pointers(err), []string{
		"/addr",
		"/at",
		"/code",
		"/count",
		"/float",
		"/gone",
		"/int",
		"/kind/name",
		"/name",
		"/never",
		"/octet",
		"/prefix",
		"/ratio",
		"/real",
		"/seen",
		"/short",
		"/since",
		"/small",
		"/step",
		"/tag/name",
		"/timeout",
		"/triple",
		"/unset",
		"/until",
	})
	assert.ErrorIs(t, err, validator.ErrCompare)
	assert.ErrorIs(t, err, validator.ErrMultiple)
	assert.ErrorIs(t, err, validator.ErrRequired)
	assert.ErrorIs(t, err, validator.ErrNull)
	assert.ErrorIs(t, err, validator.ErrZero)
}

// TestStdTimeInstant compares a time.Time by its instant, whatever its
// location.
func TestStdTimeInstant(t *testing.T) {
	s := validStd()
	// 2030-01-01T09:00:00+09:00 is the bound, excluded, in another location.
	s.At = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).In(time.FixedZone("", -5*3600))
	assert.DeepEqual(t, validator.Pointers(s.Validate(false)), []string{"/at"})

	s.At = time.Date(2019, 12, 31, 23, 59, 59, 999999999, time.UTC)
	assert.ErrorContains(t, s.Validate(false), "want >= 2020-01-01T00:00:00Z")
}
