package std

import (
	"errors"
	"fmt"
	"go/types"
	"strconv"
	"time"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

const timePath = "time"

// Time is the genext.TypeSupport of time.Time and time.Duration. It
// implements ext.Comparer.
//
// A time.Time compares with an RFC 3339 string, as in
// cmp(ge="2020-01-01T00:00:00Z"), by Compare, so that two values naming
// the same instant in other locations are equal. A time.Duration compares
// with a string time.ParseDuration reads, as in cmp(ge="1s"). The support
// leaves a number operand and multipleof on a time.Duration to the rule,
// which takes them as an integer number of nanoseconds, as for any integer
// type.
type Time struct{}

// Name returns "time".
func (Time) Name() string { return timePath }

// Comparable accepts the operands the doc comment of Time lists.
func (Time) Comparable(t types.Type, op, operand string) (bool, error) {
	switch {
	case typequery.IsNamed(t, timePath, "Time"):
		if op == ext.OpMultiple {
			return true, errors.New("a time.Time has no multiple")
		}

		_, err := instant(operand)

		return true, err
	case typequery.IsNamed(t, timePath, "Duration"):
		s, err := strconv.Unquote(operand)
		if op == ext.OpMultiple || err != nil {
			return false, nil
		}

		_, err = time.ParseDuration(s)
		if err != nil {
			return true, fmt.Errorf("%w: %s is no duration: %w", ext.ErrArgs, operand, err)
		}

		return true, nil
	}

	return false, nil
}

// Compare compares a time.Time by Compare with the instant of operand,
// spelled by time.Unix so that it is not parsed on every call, and a
// time.Duration with its number of nanoseconds.
func (Time) Compare(e ext.Emitter, t types.Type, v genext.Gen, op, operand string) genext.Gen {
	if typequery.IsNamed(t, timePath, "Duration") {
		s, _ := strconv.Unquote(operand)
		d, _ := time.ParseDuration(s)

		return lang.Expr("%s %s %d", v, op, int64(d))
	}

	at, _ := instant(operand)

	return lang.Expr("%s.Compare(%s.Unix(%d, %d)) %s 0",
		v, e.Import(timePath), at.Unix(), at.Nanosecond(), op)
}

// instant returns the time operand, a quoted RFC 3339 string, names.
func instant(operand string) (time.Time, error) {
	s, err := strconv.Unquote(operand)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"%w: a time.Time compares with an RFC 3339 string, as \"2020-01-01T00:00:00Z\"; got %s",
			ext.ErrArgs, operand,
		)
	}

	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %s is no RFC 3339 time: %w", ext.ErrArgs, operand, err)
	}

	return at, nil
}
