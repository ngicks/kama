package validator

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Sentinel errors name the kind of a failure. Leaf errors from [New] wrap one of them, so
// errors.Is reports the kind through any number of wraps.
var (
	ErrRequired = errors.New("required")
	ErrZero     = errors.New("zero")
	ErrNil      = errors.New("nil")
	ErrNull     = errors.New("null")
	ErrPattern  = errors.New("pattern")
	ErrLength   = errors.New("length")
	ErrCompare  = errors.New("compare")
	ErrConst    = errors.New("const")
	ErrFormat   = errors.New("format")
	ErrMultiple = errors.New("multiple")
	ErrUnique   = errors.New("unique")
	ErrOr       = errors.New("or")
	ErrOneOf    = errors.New("oneof")
	ErrNot      = errors.New("not")
	ErrClosed   = errors.New("closed")
	ErrKind     = errors.New("kind")
	ErrContains = errors.New("contains")
	ErrDecode   = errors.New("decode")
	ErrEncode   = errors.New("encode")
)

// Error is an error located by a path of segments from the outermost value to the failing one.
type Error struct {
	path []segment
	err  error
}

// New returns a leaf error at the root path. Its message is kind's message followed by the
// formatted message. errors.Is(err, kind) reports true. A nil kind leaves only the formatted
// message.
func New(kind error, format string, args ...any) error {
	return &Error{err: &leaf{kind: kind, msg: fmt.Sprintf(format, args...)}}
}

// Compose returns a leaf error at the root path, as New does, whose failure causes explain,
// such as the errors of the alternatives of which none holds. Its message is that of New
// followed by the message of each non-nil cause on lines of its own, indented by a tab.
// errors.Is(err, kind) reports true. The causes are not on its unwrap chain, so errors.Is
// does not report their kinds, and the lookups locate the leaf alone. [Causes] returns them.
func Compose(kind error, causes []error, format string, args ...any) error {
	var kept []error

	for _, c := range causes {
		if c != nil {
			kept = append(kept, c)
		}
	}

	return &Error{err: &leaf{kind: kind, msg: fmt.Sprintf(format, args...), causes: kept}}
}

// Causes returns the causes of the first leaf error made by [Compose] on the Unwrap() error
// chain of err, or nil when there is none.
func Causes(err error) []error {
	for err != nil {
		switch x := err.(type) {
		case *leaf:
			return slices.Clone(x.causes)
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		default:
			return nil
		}
	}

	return nil
}

// Field returns err with the struct field name prepended to the path of every leaf.
func Field(err error, name string) error {
	return wrap(err, segment{kind: fieldSegment, text: name})
}

// Index returns err with the slice or array index i prepended to the path of every leaf.
func Index(err error, i int) error {
	return wrap(err, segment{kind: indexSegment, text: strconv.Itoa(i)})
}

// Key returns err with the map key prepended to the path of every leaf. The key is formatted
// with %v.
func Key(err error, key any) error {
	return wrap(err, segment{kind: keySegment, text: fmt.Sprint(key)})
}

// Error formats the path of e itself, as in "validation failed at .a[5].b: message". A path
// recorded deeper on the unwrap chain shows up in the message of the wrapped error.
func (e *Error) Error() string {
	var b strings.Builder

	b.WriteString("validation failed")

	if len(e.path) > 0 {
		b.WriteString(" at ")

		for _, s := range e.path {
			switch s.kind {
			case fieldSegment:
				b.WriteByte('.')
				b.WriteString(s.text)
			case indexSegment, keySegment:
				b.WriteByte('[')
				b.WriteString(s.text)
				b.WriteByte(']')
			}
		}
	}

	if e.err != nil {
		b.WriteString(": ")
		b.WriteString(e.err.Error())
	}

	return b.String()
}

// Pointer returns the RFC 6901 JSON pointer of the location of e, such as "/a/5/b". It
// includes the paths of every *Error further down the Unwrap() error chain. The root is "".
func (e *Error) Pointer() string {
	return pointerOf(e)
}

// Unwrap returns the wrapped error.
func (e *Error) Unwrap() error {
	return e.err
}

type segmentKind int

const (
	fieldSegment segmentKind = iota
	indexSegment
	keySegment
)

type segment struct {
	kind segmentKind
	text string
}

// leaf keeps the formatted message apart from kind so that New stays a plain printf wrapper
// for vet and a %w in the caller's format cannot change what Unwrap returns.
type leaf struct {
	kind   error
	msg    string
	causes []error
}

func (l *leaf) Error() string {
	var b strings.Builder

	if l.kind != nil {
		b.WriteString(l.kind.Error())
		b.WriteString(": ")
	}

	b.WriteString(l.msg)

	for _, c := range l.causes {
		for line := range strings.SplitSeq(c.Error(), "\n") {
			b.WriteString("\n\t")
			b.WriteString(line)
		}
	}

	return b.String()
}

func (l *leaf) Unwrap() error {
	return l.kind
}

// wrap inspects only the dynamic type of err itself. Looking through foreign wrappers with
// errors.As would rewrap the inner error and drop the wrapper's own message.
func wrap(err error, s segment) error {
	switch x := err.(type) {
	case nil:
		return nil
	case *joinError:
		errs := make([]error, len(x.errs))
		for i, e := range x.errs {
			errs[i] = wrap(e, s)
		}

		return &joinError{errs: errs}
	case *Error:
		path := make([]segment, 0, len(x.path)+1)
		path = append(path, s)
		path = append(path, x.path...)

		return &Error{path: path, err: x.err}
	default:
		return &Error{path: []segment{s}, err: err}
	}
}

func pointerOf(err error) string {
	var b strings.Builder

	for err != nil {
		switch x := err.(type) {
		case *Error:
			for _, s := range x.path {
				b.WriteByte('/')
				b.WriteString(escapeToken(s.text))
			}

			err = x.err
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		default:
			err = nil
		}
	}

	return b.String()
}

var tokenEscaper = strings.NewReplacer("~", "~0", "/", "~1")

func escapeToken(s string) string {
	return tokenEscaper.Replace(s)
}
