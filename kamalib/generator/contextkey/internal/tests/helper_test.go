package tests

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
)

type accessors[T any] struct {
	key        any
	with       func(context.Context, T) context.Context
	value      func(context.Context) (T, bool)
	fallback   func(context.Context, T) T
	fallbackFn func(context.Context, func() T) T
	def        func(context.Context) T
	// set is a value differing from both the zero value and the default.
	set T
	// isDefault reports whether v is a result of the entry's default expr.
	isDefault func(v T) bool
	equal     func(a, b T) bool
}

func testAccessors[T any](t *testing.T, a accessors[T]) {
	t.Helper()

	var zero T

	empty := context.Background()
	other := context.WithValue(empty, a.key, struct{}{})
	ctx := a.with(empty, a.set)

	t.Run("with", func(t *testing.T) {
		assert.Assert(t, a.equal(ctx.Value(a.key).(T), a.set))
		assert.Assert(t, empty.Value(a.key) == nil, "parent must not change")
	})

	t.Run("value", func(t *testing.T) {
		v, ok := a.value(ctx)
		assert.Assert(t, ok)
		assert.Assert(t, a.equal(v, a.set))

		for _, c := range []context.Context{empty, other} {
			v, ok := a.value(c)
			assert.Assert(t, !ok)
			assert.Assert(t, a.equal(v, zero))
		}
	})

	t.Run("fallback", func(t *testing.T) {
		assert.Assert(t, a.equal(a.fallback(ctx, zero), a.set))
		assert.Assert(t, a.equal(a.fallback(empty, a.set), a.set))
		assert.Assert(t, a.equal(a.fallback(other, a.set), a.set))
	})

	t.Run("fallback fn", func(t *testing.T) {
		called := false
		fn := func() T {
			called = true
			return a.set
		}

		assert.Assert(t, a.equal(a.fallbackFn(ctx, fn), a.set))
		assert.Assert(t, !called, "fallbackFn must not be called when a value is associated")

		assert.Assert(t, a.equal(a.fallbackFn(empty, fn), a.set))
		assert.Assert(t, called)
	})

	t.Run("default", func(t *testing.T) {
		assert.Assert(t, a.equal(a.def(ctx), a.set))
		assert.Assert(t, a.isDefault(a.def(empty)))
		assert.Assert(t, a.isDefault(a.def(other)))
	})
}

func eq[T comparable](a, b T) bool { return a == b }
