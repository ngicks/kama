package opt

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

func init() { ext.RegisterTypeSupport(Support{}) }

// Support is the ext.TypeSupport of opt.Option, und.Und, elastic.Elastic,
// either.Either, either.Result, their untagged variants and tuple.Tuple2 to
// tuple.Tuple16. It implements ext.Holder, ext.Container, ext.Composite,
// ext.PresenceTester, ext.ZeroTester and ext.NullTester.
type Support struct{}

var lang genhelper.Lang[ext.Context]

// Name returns "github.com/ngicks/option".
func (Support) Name() string { return "github.com/ngicks/option" }

// Held tells the value an opt.Option holds when it is some and the value an
// und.Und holds when it is defined, null included. The role of the value is
// "t".
func (Support) Held(t types.Type, v ext.Gen) (string, types.Type, ext.Gen, ext.Gen, bool) {
	var cond string

	switch {
	case isOption(t):
		cond = "%s.IsSome()"
	case isUnd(t):
		cond = "%s.IsDefined()"
	default:
		return "", nil, nil, nil, false
	}

	return "t", typequery.TypeArg(t), lang.Expr(cond, v), lang.Expr("%s.Value()", v), true
}

// Elements tells the elements of an elastic.Elastic: the opt.Option per
// element that Options returns, none for a null element, located by index.
// The role of an element is "t", the role of the value its opt.Option
// holds.
func (Support) Elements(
	t types.Type,
	v ext.Gen,
) (string, types.Type, ext.Gen, ext.Segment, bool) {
	if !isElastic(t) {
		return "", nil, nil, 0, false
	}

	// The element type comes from the result of Options, so that it is the
	// instance of opt.Option the method set of t holds.
	obj, _, _ := types.LookupFieldOrMethod(t, false, nil, "Options")

	fn, ok := obj.(*types.Func)
	if !ok {
		return "", nil, nil, 0, false
	}

	options, ok := fn.Signature().Results().At(0).Type().Underlying().(*types.Slice)
	if !ok {
		return "", nil, nil, 0, false
	}

	return "t", options.Elem(), lang.Expr("%s.Options()", v), ext.SegmentIndex, true
}

// Presence tells an opt.Option is present when some, an und.Und and an
// elastic.Elastic when not undefined, so null counts as a value, as a JSON
// member holding null is present.
func (Support) Presence(t types.Type, v ext.Gen) (ext.Gen, string, bool) {
	switch {
	case isOption(t):
		return lang.Expr("%s.IsSome()", v), "none", true
	case isUnd(t), isElastic(t):
		return lang.Expr("!%s.IsUndefined()", v), "undefined", true
	}

	return nil, "", false
}

// Zero tells the zero value by IsZero: a none opt.Option, an undefined
// und.Und or elastic.Elastic.
func (Support) Zero(t types.Type, v ext.Gen) (ext.Gen, string, bool) {
	if !isOption(t) && !isUnd(t) && !isElastic(t) {
		return nil, "", false
	}

	return lang.Expr("%s.IsZero()", v), "a non-zero value", true
}

// Length tells the number of values an elastic.Elastic holds: the length
// of its Options, none when it is undefined or null.
func (Support) Length(t types.Type, v ext.Gen) (ext.Gen, bool) {
	if !isElastic(t) {
		return nil, false
	}

	return lang.Expr("len(%s.Options())", v), true
}

// Null tells a null und.Und or elastic.Elastic by IsNull.
func (Support) Null(t types.Type, v ext.Gen) (ext.Gen, bool) {
	if !isUnd(t) && !isElastic(t) {
		return nil, false
	}

	return lang.Expr("%s.IsNull()", v), true
}

const (
	optPath     = "github.com/ngicks/option/opt"
	undPath     = "github.com/ngicks/option/und"
	elasticPath = "github.com/ngicks/option/elastic"
)

func isOption(t types.Type) bool  { return typequery.IsNamed(t, optPath, "Option") }
func isUnd(t types.Type) bool     { return typequery.IsNamed(t, undPath, "Und") }
func isElastic(t types.Type) bool { return typequery.IsNamed(t, elasticPath, "Elastic") }
