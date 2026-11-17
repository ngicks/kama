package tests

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"sync"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/contextkey/internal/fixture/inline"
)

func TestSlogLogger(t *testing.T) {
	testAccessors(t, accessors[*slog.Logger]{
		key:        inline.KeySlogLogger,
		with:       inline.WithSlogLogger,
		value:      inline.ValueSlogLogger,
		fallback:   inline.ValueSlogLoggerFallback,
		fallbackFn: inline.ValueSlogLoggerFallbackFn,
		def:        inline.ValueSlogLoggerDefault,
		set:        slog.New(slog.DiscardHandler),
		isDefault:  func(v *slog.Logger) bool { return v == slog.Default() },
		equal:      eq[*slog.Logger],
	})
}

func TestSyncMap(t *testing.T) {
	set := new(sync.Map)
	set.Store("foo", "bar")

	testAccessors(t, accessors[*sync.Map]{
		key:        inline.KeySyncMap,
		with:       inline.WithSyncMap,
		value:      inline.ValueSyncMap,
		fallback:   inline.ValueSyncMapFallback,
		fallbackFn: inline.ValueSyncMapFallbackFn,
		def:        inline.ValueSyncMapDefault,
		set:        set,
		isDefault: func(v *sync.Map) bool {
			if v == nil || v == set {
				return false
			}

			empty := true

			v.Range(func(any, any) bool {
				empty = false
				return false
			})

			return empty
		},
		equal: eq[*sync.Map],
	})
}

func TestSyncMapDefaultIsFresh(t *testing.T) {
	ctx := context.Background()
	first := inline.ValueSyncMapDefault(ctx)
	assert.Assert(t, first != inline.ValueSyncMapDefault(ctx))
}

func TestRequestId(t *testing.T) {
	testAccessors(t, accessors[string]{
		key:        inline.KeyRequestId,
		with:       inline.WithRequestId,
		value:      inline.ValueRequestId,
		fallback:   inline.ValueRequestIdFallback,
		fallbackFn: inline.ValueRequestIdFallbackFn,
		def:        inline.ValueRequestIdDefault,
		set:        "foo",
		isDefault:  func(v string) bool { return v == "" },
		equal:      eq[string],
	})
}

func TestTraceId(t *testing.T) {
	testAccessors(t, accessors[string]{
		key:        inline.KeyTraceId,
		with:       inline.WithTraceId,
		value:      inline.ValueTraceId,
		fallback:   inline.ValueTraceIdFallback,
		fallbackFn: inline.ValueTraceIdFallbackFn,
		def:        inline.ValueTraceIdDefault,
		set:        "foo",
		isDefault:  func(v string) bool { return v == "none" },
		equal:      eq[string],
	})
}

func TestSlogAttrs(t *testing.T) {
	testAccessors(t, accessors[[]slog.Attr]{
		key:        inline.KeySlogAttrs,
		with:       inline.WithSlogAttrs,
		value:      inline.ValueSlogAttrs,
		fallback:   inline.ValueSlogAttrsFallback,
		fallbackFn: inline.ValueSlogAttrsFallbackFn,
		def:        inline.ValueSlogAttrsDefault,
		set:        []slog.Attr{slog.String("foo", "bar")},
		isDefault:  func(v []slog.Attr) bool { return v == nil },
		equal: func(a, b []slog.Attr) bool {
			return (a == nil) == (b == nil) && slices.EqualFunc(a, b, slog.Attr.Equal)
		},
	})
}

func TestSignalChan(t *testing.T) {
	testAccessors(t, accessors[chan os.Signal]{
		key:        inline.KeySignalChan,
		with:       inline.WithSignalChan,
		value:      inline.ValueSignalChan,
		fallback:   inline.ValueSignalChanFallback,
		fallbackFn: inline.ValueSignalChanFallbackFn,
		def:        inline.ValueSignalChanDefault,
		set:        make(chan os.Signal, 1),
		isDefault:  func(v chan os.Signal) bool { return v == nil },
		equal:      eq[chan os.Signal],
	})
}

func TestKeysOfSameTypeDiffer(t *testing.T) {
	assert.Assert(t, inline.KeyRequestId != inline.KeyTraceId)

	ctx := inline.WithRequestId(context.Background(), "request")
	_, ok := inline.ValueTraceId(ctx)
	assert.Assert(t, !ok)

	ctx = inline.WithTraceId(ctx, "trace")
	assert.Equal(t, inline.ValueRequestIdDefault(ctx), "request")
	assert.Equal(t, inline.ValueTraceIdDefault(ctx), "trace")
}
