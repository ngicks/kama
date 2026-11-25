package validator

import (
	"errors"
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
)

var (
	errPlain    = errors.New("plain")
	errRootLeaf = New(ErrRequired, "root")
	errPath     = Field(Index(New(ErrLength, "len"), 5), "a")
	errSibling  = Field(New(ErrZero, "ab"), "ab")
	errKey      = Field(Key(New(ErrConst, "key"), "x/y"), "a")
	errField    = Field(New(ErrNil, "a"), "a")
	errJoined   = Join(errPlain, errPath, errSibling, errKey, errField)
	errForeign  = fmt.Errorf("ctx: %w", errPath)
	errNested   = Field(fmt.Errorf("ctx: %w", Index(errPlain, 3)), "a")
	// A join behind a foreign wrapper is one leaf, located by the paths above it.
	errHiddenJoin = Field(
		fmt.Errorf("ctx: %w", Join(Field(errPlain, "p"), Field(errPlain, "q"))),
		"h",
	)
	errStdJoin = Field(errors.Join(Field(errPlain, "p"), Field(errPlain, "q")), "s")
)

type lookupCase struct {
	pointer string
	at      []error
	under   []error
}

func TestLookups(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		pointers []string
		lookups  []lookupCase
	}{
		{"nil", nil, nil, []lookupCase{{"", nil, nil}, {"/a", nil, nil}}},
		{
			"plain", errPlain, []string{""},
			[]lookupCase{{"", []error{errPlain}, []error{errPlain}}, {"/a", nil, nil}},
		},
		{
			"root leaf", errRootLeaf, []string{""},
			[]lookupCase{{"", []error{errRootLeaf}, []error{errRootLeaf}}},
		},
		{
			"path", errPath, []string{"/a/5"},
			[]lookupCase{
				{"", nil, []error{errPath}},
				{"/a", nil, []error{errPath}},
				{"/a/5", []error{errPath}, []error{errPath}},
				{"/a/50", nil, nil},
				{"a/5", nil, nil},
			},
		},
		{
			"joined", errJoined, []string{"", "/a", "/a/5", "/a/x~1y", "/ab"},
			[]lookupCase{
				{"", []error{errPlain}, []error{errPlain, errPath, errSibling, errKey, errField}},
				{"/a", []error{errField}, []error{errPath, errKey, errField}},
				{"/a/x~1y", []error{errKey}, []error{errKey}},
				{"/ab", []error{errSibling}, []error{errSibling}},
			},
		},
		{
			"foreign wrapper", errForeign, []string{"/a/5"},
			[]lookupCase{
				{"/a/5", []error{errForeign}, []error{errForeign}},
				{"/a", nil, []error{errForeign}},
			},
		},
		{
			"foreign wrapper inside",
			errNested,
			[]string{"/a/3"},
			[]lookupCase{
				{"/a/3", []error{errNested}, []error{errNested}},
				{"/a", nil, []error{errNested}},
			},
		},
		{
			"hidden join",
			errHiddenJoin,
			[]string{"/h"},
			[]lookupCase{
				{"/h", []error{errHiddenJoin}, []error{errHiddenJoin}},
				{"/h/p", nil, nil},
			},
		},
		{
			"bare stdlib join",
			errors.Join(Field(errPlain, "p"), Field(errPlain, "q")),
			[]string{""},
			[]lookupCase{{"/p", nil, nil}},
		},
		{
			"two verb w",
			fmt.Errorf("%w %w", Field(errPlain, "p"), Field(errPlain, "q")),
			[]string{""},
			[]lookupCase{{"/q", nil, nil}},
		},
		{
			"stdlib join", errStdJoin, []string{"/s"},
			[]lookupCase{{"/s", []error{errStdJoin}, []error{errStdJoin}}, {"/s/q", nil, nil}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.DeepEqual(t, Pointers(tc.err), tc.pointers)

			for _, l := range tc.lookups {
				assertSameErrors(t, At(tc.err, l.pointer), l.at, "At %q", l.pointer)
				assertSameErrors(t, Under(tc.err, l.pointer), l.under, "Under %q", l.pointer)
			}
		})
	}
}

func TestLookupInvariants(t *testing.T) {
	for _, err := range []error{
		nil, errPlain, errRootLeaf, errPath, errJoined,
		errForeign, errNested, errHiddenJoin, errStdJoin,
	} {
		t.Run(fmt.Sprint(err), func(t *testing.T) {
			var total int

			for _, p := range Pointers(err) {
				at := At(err, p)
				assert.Assert(t, len(at) > 0, "pointer %q", p)

				total += len(at)

				for _, e := range at {
					if pe, ok := errors.AsType[*Error](e); ok {
						assert.Equal(t, pe.Pointer(), p)
					}
				}
			}

			assert.Equal(t, len(Under(err, "")), total)
		})
	}
}

func TestPointersSortLexically(t *testing.T) {
	err := Join(Index(errPlain, 10), Index(errPlain, 2), Index(errPlain, 2), Key(errPlain, ""))

	assert.DeepEqual(t, Pointers(err), []string{"/", "/10", "/2"})
}

func assertSameErrors(t *testing.T, got, want []error, msgAndArgs ...any) {
	t.Helper()

	assert.Equal(t, len(got), len(want), msgAndArgs...)

	for i := range got {
		assert.Equal(t, got[i], want[i], msgAndArgs...)
	}
}
