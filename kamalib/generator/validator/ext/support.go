package ext

import (
	"errors"
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/genext"
)

// PresenceTester is a TypeSupport for types whose values may hold no
// value, as the required validator tests it.
type PresenceTester interface {
	genext.TypeSupport
	// Presence returns the condition of the value v of type t holding a
	// value and the words telling what v is when it holds none, as
	// "undefined". ok is false for a type the support does not claim.
	Presence(t types.Type, v Gen) (cond Gen, absent string, ok bool)
}

// ZeroTester is a TypeSupport for types telling their zero value, as the
// zero validator tests it.
type ZeroTester interface {
	genext.TypeSupport
	// Zero returns the condition of the value v of type t being zero and the
	// words telling what v is otherwise, as "a non-zero value". ok is false
	// for a type the support does not claim.
	Zero(t types.Type, v Gen) (cond Gen, nonZero string, ok bool)
}

// NullTester is a TypeSupport for types telling a null value, as the null
// validator tests it.
type NullTester interface {
	genext.TypeSupport
	// Null returns the condition of the value v of type t being null. ok is
	// false for a type the support does not claim.
	Null(t types.Type, v Gen) (cond Gen, ok bool)
}

// LengthTester is a TypeSupport for types telling the number of values
// they hold, as the length validator counts them.
type LengthTester interface {
	genext.TypeSupport
	// Length returns the expression of type int of the number of values the
	// value v of type t holds. ok is false for a type the support does not
	// claim.
	Length(t types.Type, v Gen) (n Gen, ok bool)
}

// OpMultiple is the operator by which multipleof asks a Comparer whether a
// value is a multiple of the operand.
const OpMultiple = "%"

// Comparer is a TypeSupport for types whose values compare with an operand
// written in a directive, as cmp(ge="1s") compares a time.Duration. cmp
// asks it with the operators >, >=, <, <=, == and !=, and multipleof with
// OpMultiple. Both ask it for each comparison before their own tests, and
// keep their own tests for a comparison it leaves to them, as std leaves a
// number compared with a time.Duration to the integer test of cmp.
type Comparer interface {
	genext.TypeSupport
	// Comparable reports whether a value of type t compares with operand by
	// op. operand is the Go literal the directive wrote: a quoted string, a
	// number as written, true or false. ok is false for a type the support
	// does not claim, and for an op or an operand it leaves to the rule, as
	// a number compared with a time.Duration, which compares as any
	// integer. err tells why the support does not take a claimed op or
	// operand. It wraps ErrArgs for an operand the support cannot read for
	// t, as an unparsable duration.
	Comparable(t types.Type, op, operand string) (ok bool, err error)
	// Compare returns the condition of the value v of type t satisfying the
	// comparison by op with operand, which Comparable accepted. It declares
	// through e what the condition needs, such as the operand parsed once at
	// generation into a package-level variable.
	Compare(e Emitter, t types.Type, v Gen, op, operand string) (cond Gen)
}

// Comparable returns what the first Comparer of r claiming t tells about
// the comparison by op with operand. r may be nil. Its error wraps
// ErrClaimed.
func Comparable(r Resolver, t types.Type, op, operand string) (ok bool, err error) {
	for c := range genext.SupportsOf[Comparer](r) {
		if ok, err := c.Comparable(t, op, operand); ok {
			if err != nil {
				err = claimedErr{err}
			}

			return true, err
		}
	}

	return false, nil
}

// ErrClaimed marks the error of a type support claiming a type and
// rejecting what a rule asks of it, as a Comparer rejects an operand. The
// type support decides for its type, so the generator reports the error
// alone and does not apply the rule to the value the type holds, as it
// does for a pointer.
var ErrClaimed = errors.New("validator: rejected by the type support claiming the type")

type claimedErr struct{ error }

func (e claimedErr) Unwrap() error { return e.error }

func (claimedErr) Is(target error) bool { return target == ErrClaimed }

// Compare returns the condition the first Comparer of r claiming t gives
// for the value v. r may be nil.
func Compare(r Resolver, e Emitter, t types.Type, v Gen, op, operand string) (cond Gen, ok bool) {
	for c := range genext.SupportsOf[Comparer](r) {
		if ok, _ := c.Comparable(t, op, operand); ok {
			return c.Compare(e, t, v, op, operand), true
		}
	}

	return nil, false
}

// Length returns what the first LengthTester of r claiming t tells about
// the value v of type t. r may be nil.
func Length(r Resolver, t types.Type, v Gen) (n Gen, ok bool) {
	for l := range genext.SupportsOf[LengthTester](r) {
		if n, ok := l.Length(t, v); ok {
			return n, true
		}
	}

	return nil, false
}

// Presence returns what the first PresenceTester of r claiming t tells
// about the value v of type t. r may be nil.
func Presence(r Resolver, t types.Type, v Gen) (cond Gen, absent string, ok bool) {
	for p := range genext.SupportsOf[PresenceTester](r) {
		if cond, absent, ok := p.Presence(t, v); ok {
			return cond, absent, true
		}
	}

	return nil, "", false
}

// Zero returns what the first ZeroTester of r claiming t tells about the
// value v of type t. r may be nil.
func Zero(r Resolver, t types.Type, v Gen) (cond Gen, nonZero string, ok bool) {
	for z := range genext.SupportsOf[ZeroTester](r) {
		if cond, nonZero, ok := z.Zero(t, v); ok {
			return cond, nonZero, true
		}
	}

	return nil, "", false
}

// Null returns what the first NullTester of r claiming t tells about the
// value v of type t. r may be nil.
func Null(r Resolver, t types.Type, v Gen) (cond Gen, ok bool) {
	for n := range genext.SupportsOf[NullTester](r) {
		if cond, ok := n.Null(t, v); ok {
			return cond, true
		}
	}

	return nil, false
}
