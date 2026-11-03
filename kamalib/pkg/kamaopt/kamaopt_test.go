package kamaopt

import (
	"go/token"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"
)

const fixturePath = "github.com/ngicks/kama/kamalib/pkg/kamaopt/internal/fixture"

func TestCollect(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedCompiledGoFiles | packages.NeedSyntax,
	}, "./internal/fixture")
	assert.NilError(t, err)
	assert.Equal(t, len(pkgs), 1)
	assert.Equal(t, len(pkgs[0].Errors), 0, "%v", pkgs[0].Errors)

	dir, err := filepath.Abs("internal/fixture")
	assert.NilError(t, err)
	a, b := filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")

	raw := func(key Key, directive, args, file string, line, column int) Raw {
		return Raw{
			Key:       key,
			Directive: directive,
			Args:      args,
			Origin: Origin{
				Source: SourceDirective,
				Pos:    token.Position{Filename: file, Line: line, Column: column},
			},
		}
	}
	fileA := Key{PkgPath: fixturePath, File: a}
	fileB := Key{PkgPath: fixturePath, File: b}
	typ := func(name string) Key { return Key{PkgPath: fixturePath, Type: name} }
	field := func(typ, name string) Key { return Key{PkgPath: fixturePath, Type: typ, Field: name} }

	want := []Raw{
		raw(fileA, "partial", "package", a, 3, 1),
		raw(fileA, "partial", "file", a, 11, 1),
		raw(typ("Single"), "partial", "", a, 17, 1),
		raw(field("Single", "A"), "partial:merge", "append", a, 21, 2),
		raw(
			field("Single", "B"),
			"partial:flag", `name=addr short=a usage="listen address"`,
			a, 23, 11,
		),
		raw(field("Single", "C"), "partial:x", "", a, 26, 2),
		raw(field("Single", "D"), "partial:x", "", a, 26, 2),
		raw(field("Single", "Buffer"), "partial:embedded", "", a, 28, 16),
		raw(typ("G1"), "partial", "grouped", a, 38, 2),
		raw(field("WithGeneric", "Generic"), "partial:gen", "", a, 46, 2),
		raw(fileB, "partial", "file", b, 5, 1),
		raw(fileB, "partial:other", "x=1", b, 6, 1),
	}

	got, err := Collect(pkgs, "kama")
	assert.NilError(t, err)
	for i := range got {
		got[i].Origin.Pos.Offset = 0
	}
	assert.DeepEqual(t, got, want)
}

func TestCollectWithoutSyntax(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName}, "./internal/fixture")
	assert.NilError(t, err)

	_, err = Collect(pkgs, "kama")
	assert.ErrorContains(t, err, "without syntax")
}

func TestParseArgs(t *testing.T) {
	cases := []struct {
		args string
		want map[string]string
		err  string
	}{
		{"", map[string]string{}, ""},
		{" \t ", map[string]string{}, ""},
		{"append", map[string]string{"append": ""}, ""},
		{
			`name=addr short=a usage="listen address"`,
			map[string]string{"name": "addr", "short": "a", "usage": "listen address"},
			"",
		},
		{"  a=1\tb  c ", map[string]string{"a": "1", "b": "", "c": ""}, ""},
		{"a=", map[string]string{"a": ""}, ""},
		{`a="" b`, map[string]string{"a": "", "b": ""}, ""},
		{`a="x\"y\né"`, map[string]string{"a": "x\"y\né"}, ""},
		{`a="b c=d"`, map[string]string{"a": "b c=d"}, ""},
		{"a=b=c", map[string]string{"a": "b=c"}, ""},
		{"a=1 a=2", nil, `duplicate name "a"`},
		{"a a", nil, `duplicate name "a"`},
		{"=1", nil, "missing name"},
		{`"a"=1`, nil, "missing name"},
		{`a"b"`, nil, "unexpected quote"},
		{`a="unterminated`, nil, "unterminated"},
		{`a="x\"`, nil, "unterminated"},
		{`a="x"y`, nil, "after closing quote"},
		{`a=x"y"`, nil, "quote inside unquoted value"},
		{`a="\q"`, nil, "invalid syntax"},
	}

	for _, c := range cases {
		t.Run(c.args, func(t *testing.T) {
			got, err := ParseArgs(c.args)
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
				return
			}
			assert.NilError(t, err)
			assert.DeepEqual(t, got, c.want)
		})
	}
}
