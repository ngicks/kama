package dircmnt

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestParseArgsKeyValue(t *testing.T) {
	key := func(k string) KeyValue { return KeyValue{Key: k} }
	pair := func(k, v string) KeyValue { return KeyValue{Key: k, Value: v, HasValue: true} }
	quoted := func(k, v string) KeyValue {
		return KeyValue{Key: k, Value: v, HasValue: true, Quoted: true}
	}

	cases := []struct {
		args string
		want []KeyValue
		err  string
	}{
		{"", nil, ""},
		{" \t ", nil, ""},
		{"partial", []KeyValue{key("partial")}, ""},
		{
			`partial=a.toml validate="b c.yaml"`,
			[]KeyValue{pair("partial", "a.toml"), quoted("validate", "b c.yaml")},
			"",
		},
		{"  a=1\tb  c ", []KeyValue{pair("a", "1"), key("b"), key("c")}, ""},
		{"a= b", []KeyValue{pair("a", ""), key("b")}, ""},
		{`a=""`, []KeyValue{quoted("a", "")}, ""},
		{"a=1,2 b)c f(x)", []KeyValue{pair("a", "1,2"), key("b)c"), key("f(x)")}, ""},
		{"a=b=c", []KeyValue{pair("a", "b=c")}, ""},
		{">=1", []KeyValue{pair(">", "1")}, ""},
		{"=1", nil, "missing key"},
		{"a=1 a a=2", []KeyValue{pair("a", "1"), key("a"), pair("a", "2")}, ""},
		{`a="x\"y\né"`, []KeyValue{quoted("a", "x\"y\né")}, ""},
		{"a=`b\\ \"c\"`", []KeyValue{quoted("a", `b\ "c"`)}, ""},
		{`"a b"=1`, []KeyValue{pair("a b", "1")}, ""},
		{`"a=b"`, []KeyValue{key("a=b")}, ""},
		{`""=1`, []KeyValue{pair("", "1")}, ""},
		{`""`, []KeyValue{key("")}, ""},
		{
			"go run ./gen -o x.go",
			[]KeyValue{key("go"), key("run"), key("./gen"), key("-o"), key("x.go")},
			"",
		},
		{`a"b"`, nil, "quote inside unquoted text"},
		{`a=x"y"`, nil, "quote inside unquoted text"},
		{"a=x`y`", nil, "quote inside unquoted text"},
		{`a="unterminated`, nil, "unterminated"},
		{`a="x\"`, nil, "unterminated"},
		{"`a", nil, "unterminated"},
		{`a="x"y`, nil, "after closing quote"},
		{`"a"b`, nil, "after closing quote"},
		{`a="x"=y`, nil, "after closing quote"},
		{`a="\q"`, nil, "invalid syntax"},
	}

	for _, c := range cases {
		t.Run(c.args, func(t *testing.T) {
			got, err := ParseArgsKeyValue(c.args)
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
				return
			}

			assert.NilError(t, err)
			assert.DeepEqual(t, got, c.want)
		})
	}
}
