package dircmnt

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestParseArgsKeyValue(t *testing.T) {
	key := func(k string) KeyValue { return KeyValue{Key: k} }
	pair := func(k, v string) KeyValue { return KeyValue{Key: k, Value: v, HasValue: true} }

	cases := []struct {
		args string
		want []KeyValue
		err  string
	}{
		{"", nil, ""},
		{" \t ", nil, ""},
		{"append", []KeyValue{key("append")}, ""},
		{
			`name=addr short=a usage="listen address"`,
			[]KeyValue{pair("name", "addr"), pair("short", "a"), pair("usage", "listen address")},
			"",
		},
		{"  a=1\tb  c ", []KeyValue{pair("a", "1"), key("b"), key("c")}, ""},
		{"a= b", []KeyValue{pair("a", ""), key("b")}, ""},
		{`a=""`, []KeyValue{pair("a", "")}, ""},
		{"a=b=c", []KeyValue{pair("a", "b=c")}, ""},
		{"a=1 a a=2", []KeyValue{pair("a", "1"), key("a"), pair("a", "2")}, ""},
		{`a="x\"y\né"`, []KeyValue{pair("a", "x\"y\né")}, ""},
		{`a="b c=d"`, []KeyValue{pair("a", "b c=d")}, ""},
		{"a=`b\\ \"c\"`", []KeyValue{pair("a", `b\ "c"`)}, ""},
		{`"a b"=1`, []KeyValue{pair("a b", "1")}, ""},
		{`"a=b"`, []KeyValue{key("a=b")}, ""},
		{"`a b`=\"c d\" `e`", []KeyValue{pair("a b", "c d"), key("e")}, ""},
		{
			"go run ./gen -o x.go",
			[]KeyValue{key("go"), key("run"), key("./gen"), key("-o"), key("x.go")},
			"",
		},
		{"=1", nil, "missing key"},
		{`""=1`, nil, "missing key"},
		{`""`, nil, "missing key"},
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
