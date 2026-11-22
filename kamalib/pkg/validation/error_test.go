package validation

import (
	"errors"
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
)

func TestPathOrderAndText(t *testing.T) {
	inner := New(ErrLength, "want %s, got %d", "<=3", 5)
	err := Field(Index(Field(inner, "b"), 5), "a")

	assert.Equal(t, err.Error(), "validation failed at .a[5].b: length: want <=3, got 5")
	assert.Equal(t, mustError(t, err).Pointer(), "/a/5/b")
}

func TestErrorText(t *testing.T) {
	plain := errors.New("boom")

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"root leaf", New(ErrRequired, "missing"), "validation failed: required: missing"},
		{"nil kind", New(nil, "bad %q", "x"), `validation failed: bad "x"`},
		{"plain wrapped", Field(plain, "a"), "validation failed at .a: boom"},
		{"key", Key(plain, "k"), "validation failed at [k]: boom"},
		{"int key", Key(plain, 7), "validation failed at [7]: boom"},
		{"zero value", &Error{}, "validation failed"},
		{
			"foreign wrapper inside",
			Field(fmt.Errorf("ctx: %w", Index(plain, 3)), "a"),
			"validation failed at .a: ctx: validation failed at [3]: boom",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.err.Error(), tc.want)
		})
	}
}

func TestPointer(t *testing.T) {
	plain := errors.New("boom")

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"root", New(ErrZero, "x"), ""},
		{"field", Field(plain, "a"), "/a"},
		{"index", Index(plain, 5), "/5"},
		{"empty key", Key(plain, ""), "/"},
		{"escape", Key(plain, "a/b~c"), "/a~1b~0c"},
		{"escape order", Field(plain, "~1"), "/~01"},
		{"chain", Field(fmt.Errorf("ctx: %w", Index(plain, 3)), "a"), "/a/3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, mustError(t, tc.err).Pointer(), tc.want)
		})
	}
}

func TestWrapNil(t *testing.T) {
	assert.NilError(t, Field(nil, "a"))
	assert.NilError(t, Index(nil, 1))
	assert.NilError(t, Key(nil, "k"))
}

func TestWrapDoesNotMutate(t *testing.T) {
	inner := Field(New(ErrNil, "x"), "c")
	a := Field(inner, "a")
	b := Index(inner, 1)

	assert.Equal(t, mustError(t, inner).Pointer(), "/c")
	assert.Equal(t, mustError(t, a).Pointer(), "/a/c")
	assert.Equal(t, mustError(t, b).Pointer(), "/1/c")

	joined := Join(inner, Key(New(ErrNull, "y"), "k"))
	ja := Field(joined, "a")
	jb := Field(joined, "b")

	assert.DeepEqual(t, Pointers(joined), []string{"/c", "/k"})
	assert.DeepEqual(t, Pointers(ja), []string{"/a/c", "/a/k"})
	assert.DeepEqual(t, Pointers(jb), []string{"/b/c", "/b/k"})
}

func TestWrapDistributesOverJoin(t *testing.T) {
	err := Field(Join(Field(New(ErrZero, "x"), "a"), errors.New("plain")), "top")

	assert.DeepEqual(t, Pointers(err), []string{"/top", "/top/a"})
	assert.Equal(t, len(err.(interface{ Unwrap() []error }).Unwrap()), 2)
}

func TestErrorsIs(t *testing.T) {
	leaf := New(ErrRequired, "missing")

	cases := []struct {
		name string
		err  error
	}{
		{"leaf", leaf},
		{"field", Field(leaf, "a")},
		{"index", Index(Field(leaf, "a"), 2)},
		{"key", Key(Index(leaf, 2), "k")},
		{"join", Join(errors.New("other"), Key(leaf, "k"))},
		{"wrapped join", Field(Join(errors.New("other"), leaf), "a")},
		{"foreign", Field(fmt.Errorf("ctx: %w", Field(leaf, "a")), "b")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Assert(t, errors.Is(tc.err, ErrRequired))
			assert.Assert(t, !errors.Is(tc.err, ErrZero))

			_, ok := errors.AsType[*Error](tc.err)
			assert.Assert(t, ok)
		})
	}
}

func TestJoin(t *testing.T) {
	a := New(ErrZero, "a")
	b := New(ErrNil, "b")
	c := New(ErrNull, "c")

	assert.NilError(t, Join())
	assert.NilError(t, Join(nil, nil))
	assert.Equal(t, Join(nil, a, nil), a)

	j := Join(a, nil, b)
	assert.Equal(t, j.Error(), a.Error()+"\n"+b.Error())
	assertSameErrors(t, unwrapAll(t, j), []error{a, b})

	assert.Equal(t, Join(nil, j), j)

	nested := Join(c, j, Join(nil, Join(a)))
	assertSameErrors(t, unwrapAll(t, nested), []error{c, a, b, a})

	x := Join(j, c)
	y := Join(j, a)

	assertSameErrors(t, unwrapAll(t, j), []error{a, b})
	assertSameErrors(t, unwrapAll(t, x), []error{a, b, c})
	assertSameErrors(t, unwrapAll(t, y), []error{a, b, a})
}

func TestJoinKeepsForeignJoinOpaque(t *testing.T) {
	foreign := errors.Join(New(ErrZero, "a"), New(ErrNil, "b"))
	j := Join(foreign, New(ErrNull, "c"))

	assert.Equal(t, len(unwrapAll(t, j)), 2)
}

func mustError(t *testing.T, err error) *Error {
	t.Helper()

	e, ok := errors.AsType[*Error](err)
	assert.Assert(t, ok, "%T is not *Error", err)

	return e
}

func unwrapAll(t *testing.T, err error) []error {
	t.Helper()

	u, ok := err.(interface{ Unwrap() []error })
	assert.Assert(t, ok, "%T has no Unwrap() []error", err)

	return u.Unwrap()
}
