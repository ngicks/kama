package opt

import (
	"fmt"
	"go/types"
	"strconv"
	"strings"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// Roles returns the roles of the values an either.Either, an
// either.Result, their untagged variants and a tuple.Tuple2 to
// tuple.Tuple16 hold.
func (Support) Roles(t types.Type) []string {
	switch {
	case isEither(t), isUntaggedEither(t):
		return []string{"left", "right"}
	case isResult(t), isUntaggedResult(t):
		return []string{"ok"}
	}

	if n := tupleLen(t); n > 0 {
		roles := make([]string, n)
		for i := range roles {
			roles[i] = "v" + strconv.Itoa(i+1)
		}

		return roles
	}

	return nil
}

// Part tells the value of role a value v of type t holds.
//
// An either.Either holds its left value, role "left", when IsLeft reports
// true and its right value, role "right", when IsRight does. It encodes as
// a JSON object tagging the value with the member left or right, so the
// value is located at that member. An either.Result holds its ok value,
// role "ok", when IsOk reports true, located at the member ok it encodes
// as. Its error is no value to validate. either.UntaggedEither and
// either.UntaggedResult encode the value alone, so their values add no
// segment. A tuple.TupleN holds its fields V1 to VN, roles "v1" to "vN",
// always. It encodes as a JSON object with the members V1 to VN, which
// locate the fields.
func (Support) Part(
	t types.Type,
	v ext.Gen,
	role string,
) (types.Type, ext.Gen, ext.Gen, ext.Segment, ext.Gen, bool) {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil, nil, nil, 0, nil, false
	}

	args := named.TypeArgs()
	untagged := isUntaggedEither(t) || isUntaggedResult(t)

	segment := ext.SegmentField
	if untagged {
		segment = ext.SegmentNone
	}

	switch {
	case (isEither(t) || isUntaggedEither(t)) && role == "left":
		return args.At(0), lang.Expr("%s.IsLeft()", v), lang.Expr("%s.UnwrapLeft()", v),
			segment, lang.Expr(`"left"`), true
	case (isEither(t) || isUntaggedEither(t)) && role == "right":
		return args.At(1), lang.Expr("%s.IsRight()", v), lang.Expr("%s.UnwrapRight()", v),
			segment, lang.Expr(`"right"`), true
	case (isResult(t) || isUntaggedResult(t)) && role == "ok":
		return args.At(0), lang.Expr("%s.IsOk()", v), lang.Expr("%s.Value()", v),
			segment, lang.Expr(`"ok"`), true
	}

	n := tupleLen(t)

	i, err := strconv.Atoi(strings.TrimPrefix(role, "v"))
	if n == 0 || !strings.HasPrefix(role, "v") || err != nil || i < 1 || i > n {
		return nil, nil, nil, 0, nil, false
	}

	field := fmt.Sprintf("V%d", i)

	return args.At(i - 1), nil, lang.Expr("%s.%s", v, field),
		ext.SegmentField, lang.Expr("%s", strconv.Quote(field)), true
}

const (
	eitherPath = "github.com/ngicks/option/either"
	tuplePath  = "github.com/ngicks/option/tuple"
)

func isEither(t types.Type) bool { return typequery.IsNamed(t, eitherPath, "Either") }
func isResult(t types.Type) bool { return typequery.IsNamed(t, eitherPath, "Result") }

func isUntaggedEither(t types.Type) bool {
	return typequery.IsNamed(t, eitherPath, "UntaggedEither")
}

func isUntaggedResult(t types.Type) bool {
	return typequery.IsNamed(t, eitherPath, "UntaggedResult")
}

// tupleLen returns N of a tuple.TupleN, 0 for any other type.
func tupleLen(t types.Type) int {
	for n := 2; n <= 16; n++ {
		if typequery.IsNamed(t, tuplePath, "Tuple"+strconv.Itoa(n)) {
			return n
		}
	}

	return 0
}
