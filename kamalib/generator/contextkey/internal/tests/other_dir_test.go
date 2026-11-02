package tests

import (
	"context"
	"maps"
	"net/http"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/contextkey/internal/fixture/jsontemplate"
	deepkeys "github.com/ngicks/kama/kamalib/generator/contextkey/internal/fixture/nested/deep"
	"github.com/ngicks/kama/kamalib/generator/contextkey/internal/fixture/shareda"
	"github.com/ngicks/kama/kamalib/generator/contextkey/internal/fixture/sharedb"
	"github.com/ngicks/kama/kamalib/generator/contextkey/internal/fixture/tomltemplate"
)

func TestJsonTemplate(t *testing.T) {
	t.Run("UserName", func(t *testing.T) {
		testAccessors(t, accessors[string]{
			key:        jsontemplate.KeyUserName,
			with:       jsontemplate.WithUserName,
			value:      jsontemplate.ValueUserName,
			fallback:   jsontemplate.ValueUserNameFallback,
			fallbackFn: jsontemplate.ValueUserNameFallbackFn,
			def:        jsontemplate.ValueUserNameDefault,
			set:        "alice",
			isDefault:  func(v string) bool { return v == "anonymous" },
			equal:      eq[string],
		})
	})

	t.Run("Deadline", func(t *testing.T) {
		testAccessors(t, accessors[time.Time]{
			key:        jsontemplate.KeyDeadline,
			with:       jsontemplate.WithDeadline,
			value:      jsontemplate.ValueDeadline,
			fallback:   jsontemplate.ValueDeadlineFallback,
			fallbackFn: jsontemplate.ValueDeadlineFallbackFn,
			def:        jsontemplate.ValueDeadlineDefault,
			set:        time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			isDefault:  time.Time.IsZero,
			equal:      time.Time.Equal,
		})
	})
}

func TestTomlTemplate(t *testing.T) {
	t.Run("RetryCount", func(t *testing.T) {
		testAccessors(t, accessors[int]{
			key:        tomltemplate.KeyRetryCount,
			with:       tomltemplate.WithRetryCount,
			value:      tomltemplate.ValueRetryCount,
			fallback:   tomltemplate.ValueRetryCountFallback,
			fallbackFn: tomltemplate.ValueRetryCountFallbackFn,
			def:        tomltemplate.ValueRetryCountDefault,
			set:        5,
			isDefault:  func(v int) bool { return v == 3 },
			equal:      eq[int],
		})
	})

	t.Run("Tags", func(t *testing.T) {
		testAccessors(t, accessors[map[string]string]{
			key:        tomltemplate.KeyTags,
			with:       tomltemplate.WithTags,
			value:      tomltemplate.ValueTags,
			fallback:   tomltemplate.ValueTagsFallback,
			fallbackFn: tomltemplate.ValueTagsFallbackFn,
			def:        tomltemplate.ValueTagsDefault,
			set:        map[string]string{"foo": "bar"},
			isDefault:  func(v map[string]string) bool { return v != nil && len(v) == 0 },
			equal: func(a, b map[string]string) bool {
				return (a == nil) == (b == nil) && maps.Equal(a, b)
			},
		})
	})
}

func TestNestedDir(t *testing.T) {
	t.Run("Verbose", func(t *testing.T) {
		testAccessors(t, accessors[bool]{
			key:        deepkeys.KeyVerbose,
			with:       deepkeys.WithVerbose,
			value:      deepkeys.ValueVerbose,
			fallback:   deepkeys.ValueVerboseFallback,
			fallbackFn: deepkeys.ValueVerboseFallbackFn,
			def:        deepkeys.ValueVerboseDefault,
			set:        true,
			isDefault:  func(v bool) bool { return !v },
			equal:      eq[bool],
		})
	})

	t.Run("HttpHeader", func(t *testing.T) {
		set := http.Header{"X-Foo": {"bar"}}
		testAccessors(t, accessors[http.Header]{
			key:        deepkeys.KeyHttpHeader,
			with:       deepkeys.WithHttpHeader,
			value:      deepkeys.ValueHttpHeader,
			fallback:   deepkeys.ValueHttpHeaderFallback,
			fallbackFn: deepkeys.ValueHttpHeaderFallbackFn,
			def:        deepkeys.ValueHttpHeaderDefault,
			set:        set,
			isDefault:  func(v http.Header) bool { return v == nil },
			equal: func(a, b http.Header) bool {
				return (a == nil) == (b == nil) && a.Get("X-Foo") == b.Get("X-Foo")
			},
		})
	})
}

func TestSharedTemplate(t *testing.T) {
	t.Run("shareda", func(t *testing.T) {
		testAccessors(t, accessors[string]{
			key:        shareda.KeyTenant,
			with:       shareda.WithTenant,
			value:      shareda.ValueTenant,
			fallback:   shareda.ValueTenantFallback,
			fallbackFn: shareda.ValueTenantFallbackFn,
			def:        shareda.ValueTenantDefault,
			set:        "a",
			isDefault:  func(v string) bool { return v == "default" },
			equal:      eq[string],
		})
	})

	t.Run("sharedb", func(t *testing.T) {
		testAccessors(t, accessors[string]{
			key:        sharedb.KeyTenant,
			with:       sharedb.WithTenant,
			value:      sharedb.ValueTenant,
			fallback:   sharedb.ValueTenantFallback,
			fallbackFn: sharedb.ValueTenantFallbackFn,
			def:        sharedb.ValueTenantDefault,
			set:        "b",
			isDefault:  func(v string) bool { return v == "default" },
			equal:      eq[string],
		})
	})

	t.Run("packages do not share keys", func(t *testing.T) {
		ctx := shareda.WithTenant(context.Background(), "a")
		_, ok := sharedb.ValueTenant(ctx)
		assert.Assert(t, !ok)

		ctx = sharedb.WithTenant(ctx, "b")
		assert.Equal(t, shareda.ValueTenantDefault(ctx), "a")
		assert.Equal(t, sharedb.ValueTenantDefault(ctx), "b")
	})
}

func TestKeyTypeFunc(t *testing.T) {
	// key_type_func names the function building the key, and the generator
	// passes it the entry's type.
	assert.DeepEqual(t, sharedb.KeyTenant, &sharedb.Key{Type: "string"})
}
