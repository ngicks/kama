package dircmnt

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestParseArgsKeyValue(t *testing.T) {
	key := func(k string) KeyValue { return KeyValue{Key: k} }
	pair := func(k, v string) KeyValue { return KeyValue{Key: k, Value: v, HasValue: true} }
	// quoted marks kv as written with a quoted key, a quoted value or both.
	quoted := func(kv KeyValue, key, value bool) KeyValue {
		kv.KeyQuoted, kv.ValueQuoted = key, value
		return kv
	}
	call := func(name string, args ...KeyValue) KeyValue {
		return KeyValue{Key: name, Call: true, Args: args}
	}

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
			[]KeyValue{
				pair("name", "addr"),
				pair("short", "a"),
				quoted(pair("usage", "listen address"), false, true),
			},
			"",
		},
		{"  a=1\tb  c ", []KeyValue{pair("a", "1"), key("b"), key("c")}, ""},
		{"a= b", []KeyValue{pair("a", ""), key("b")}, ""},
		{`a=""`, []KeyValue{quoted(pair("a", ""), false, true)}, ""},
		{"a=1,2 b)c", []KeyValue{pair("a", "1,2"), key("b)c")}, ""},
		{"regexp=^(a|b)$ >=f(x)", []KeyValue{pair("regexp", "^(a|b)$"), key(">=f(x)")}, ""},
		{"f()", []KeyValue{call("f")}, ""},
		{"f( ) g", []KeyValue{call("f"), key("g")}, ""},
		{
			`or(cmp=">=1", const="2") not(regexp="^x") if(a, b)`,
			[]KeyValue{
				call("or",
					quoted(pair("cmp", ">=1"), false, true),
					quoted(pair("const", "2"), false, true),
				),
				call("not", quoted(pair("regexp", "^x"), false, true)),
				call("if", key("a"), key("b")),
			},
			"",
		},
		{
			"and( or(a,not(b=1)) ,\t>=1 , c= ,\"d e\" )",
			[]KeyValue{call("and",
				call("or", key("a"), call("not", pair("b", "1"))),
				key(">=1"),
				pair("c", ""),
				quoted(key("d e"), true, false),
			)},
			"",
		},
		{`f("a,b"=")(", ==",")`, []KeyValue{call("f",
			quoted(pair("a,b", ")("), true, true),
			key(`==","`),
		)}, ""},
		{"f(a=1 b)", nil, `want , or ) after a=1, got "b)"`},
		{"f(a", nil, "missing ) of the call f"},
		{"f(a,", nil, "missing arg after , following a"},
		{"f(a, )", nil, "missing arg after , following a"},
		{"f(,a)", nil, "missing key"},
		{"f(a)b", nil, "text after the call f"},
		{`f(a="x"b)`, nil, "after closing quote"},
		{"f(a=(b))", nil, `want , or ) after a=, got "(b))"`},
		{"f(>=1(", nil, `want , or ) after >=1, got "("`},
		{`"f"(a)`, nil, "quoted name of a call"},
		{"(a)", nil, "missing key"},
		{"a=b=c", []KeyValue{pair("a", "b=c")}, ""},
		{"a=1 a a=2", []KeyValue{pair("a", "1"), key("a"), pair("a", "2")}, ""},
		{`a="x\"y\né"`, []KeyValue{quoted(pair("a", "x\"y\né"), false, true)}, ""},
		{`a="b c=d"`, []KeyValue{quoted(pair("a", "b c=d"), false, true)}, ""},
		{"a=`b\\ \"c\"`", []KeyValue{quoted(pair("a", `b\ "c"`), false, true)}, ""},
		{`"a b"=1`, []KeyValue{quoted(pair("a b", "1"), true, false)}, ""},
		{`"a=b"`, []KeyValue{quoted(key("a=b"), true, false)}, ""},
		{`"1" 1`, []KeyValue{quoted(key("1"), true, false), key("1")}, ""},
		{
			"`a b`=\"c d\" `e`",
			[]KeyValue{quoted(pair("a b", "c d"), true, true), quoted(key("e"), true, false)},
			"",
		},
		{
			"go run ./gen -o x.go",
			[]KeyValue{key("go"), key("run"), key("./gen"), key("-o"), key("x.go")},
			"",
		},
		{
			">=1 <=10 ==3 <5 !=0 >2",
			[]KeyValue{key(">=1"), key("<=10"), key("==3"), key("<5"), key("!=0"), key(">2")},
			"",
		},
		{"=1", []KeyValue{key("=1")}, ""},
		{"==a=b", []KeyValue{key("==a=b")}, ""},
		{">=", []KeyValue{key(">=")}, ""},
		{`=="a b" !=` + "`c`", []KeyValue{key(`=="a b"`), key("!=`c`")}, ""},
		{`a=>1`, []KeyValue{pair("a", ">1")}, ""},
		{`a ==1`, []KeyValue{key("a"), key("==1")}, ""},
		{`=="a"b`, nil, "after closing quote"},
		{`==a"b"`, nil, "quote inside unquoted text"},
		{`=="a`, nil, "unterminated"},
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

func TestSpellArgs(t *testing.T) {
	cases := []struct {
		args, spelled string
	}{
		{"", ""},
		{"append", "append"},
		{`"append"`, `"append"`},
		{`"a b"`, `"a b"`},
		{"name=addr short=a", "name=addr short=a"},
		{`usage="listen address"`, `usage="listen address"`},
		{"usage=`a \"b\"`", `usage="a \"b\""`},
		{`"a b"=1`, `"a b"=1`},
		{"a=", "a="},
		{`a=""`, `a=""`},
		{"a=b=c", "a=b=c"},
		{">=1 <=10 ==3", ">=1 <=10 ==3"},
		{`!="a b"`, `!="a b"`},
		{`"1" 1 a="1" a=1`, `"1" 1 a="1" a=1`},
		{"a=1,2 regexp=^(a)$", "a=1,2 regexp=^(a)$"},
		{"f()", "f()"},
		{
			"or( a ,not(b=1),c=`x,y` ) d",
			`or(a, not(b=1), c="x,y") d`,
		},
		{`f(a=")", "(b"=1, ==",")`, `f(a=")", "(b"=1, ==",")`},
	}

	for _, c := range cases {
		t.Run(c.args, func(t *testing.T) {
			kv, err := ParseArgsKeyValue(c.args)
			assert.NilError(t, err)
			assert.Equal(t, SpellArgs(kv), c.spelled)

			back, err := ParseArgsKeyValue(SpellArgs(kv))
			assert.NilError(t, err)
			assert.DeepEqual(t, back, kv)
		})
	}
}

func TestSpellToken(t *testing.T) {
	cases := []struct {
		kv              KeyValue
		top, insideCall string
	}{
		{KeyValue{Key: "a(b"}, `"a(b"`, `"a(b"`},
		{KeyValue{Key: "a=b"}, `"a=b"`, `"a=b"`},
		{KeyValue{Key: "a,b"}, "a,b", `"a,b"`},
		{KeyValue{Key: "a", Value: "(x)", HasValue: true}, "a=(x)", `a="(x)"`},
	}

	for _, c := range cases {
		t.Run(c.top, func(t *testing.T) {
			assert.Equal(t, SpellArgs([]KeyValue{c.kv}), c.top)
			assert.Equal(t, SpellCallArgs([]KeyValue{c.kv}), c.insideCall)

			back, err := ParseArgsKeyValue(c.top)
			assert.NilError(t, err)
			assert.Equal(t, len(back), 1)
			assert.Equal(t, back[0].Key, c.kv.Key)
			assert.Equal(t, back[0].Value, c.kv.Value)

			back, err = ParseCallArgs(c.insideCall)
			assert.NilError(t, err)
			assert.Equal(t, len(back), 1)
			assert.Equal(t, back[0].Key, c.kv.Key)
			assert.Equal(t, back[0].Value, c.kv.Value)
		})
	}
}

func TestParseCallArgs(t *testing.T) {
	cases := []struct {
		args string
		want []KeyValue
		err  string
	}{
		{"", nil, ""},
		{" ", nil, ""},
		{`cmp=">=10", const="0"`, []KeyValue{
			{Key: "cmp", Value: ">=10", HasValue: true, ValueQuoted: true},
			{Key: "const", Value: "0", HasValue: true, ValueQuoted: true},
		}, ""},
		{
			"a, f(b)",
			[]KeyValue{{Key: "a"}, {Key: "f", Call: true, Args: []KeyValue{{Key: "b"}}}},
			"",
		},
		{"a b", nil, `want , after a, got "b"`},
		{"a)", nil, `want , after a, got ")"`},
		{"a,", nil, "missing arg after ,"},
	}

	for _, c := range cases {
		t.Run(c.args, func(t *testing.T) {
			got, err := ParseCallArgs(c.args)
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
				return
			}

			assert.NilError(t, err)
			assert.DeepEqual(t, got, c.want)
			assert.DeepEqual(t, mustParseCallArgs(t, SpellCallArgs(got)), got)
		})
	}
}

func mustParseCallArgs(t *testing.T, args string) []KeyValue {
	t.Helper()

	kv, err := ParseCallArgs(args)
	assert.NilError(t, err)

	return kv
}

func TestArg(t *testing.T) {
	kv, err := ParseArgsKeyValue("a b=1 c=`x y` or(cmp=\">=1\",  not( d ))")
	assert.NilError(t, err)

	got := make([]string, 0, len(kv))
	for _, a := range kv {
		got = append(got, a.Arg())
	}

	assert.DeepEqual(t, got, []string{"", "1", "x y", `cmp=">=1", not(d)`})
}

func TestBareWords(t *testing.T) {
	cases := []struct {
		args string
		want []string
		ok   bool
	}{
		{"", nil, true},
		{"file package", []string{"file", "package"}, true},
		{`"a b" >=1`, []string{"a b", ">=1"}, true},
		{"file a=1", nil, false},
		{"a=", nil, false},
		{"file f()", nil, false},
	}

	for _, c := range cases {
		t.Run(c.args, func(t *testing.T) {
			kv, err := ParseArgsKeyValue(c.args)
			assert.NilError(t, err)

			got, ok := BareWords(kv)
			assert.Equal(t, ok, c.ok)
			assert.DeepEqual(t, got, c.want)
		})
	}
}

func TestKeyValueMap(t *testing.T) {
	cases := []struct {
		args string
		want map[string]string
		err  string
	}{
		{"", map[string]string{}, ""},
		{
			`name=addr usage="a b" append`,
			map[string]string{"name": "addr", "usage": "a b", "append": ""},
			"",
		},
		{"a= b", map[string]string{"a": "", "b": ""}, ""},
		{"a=1 a=2", nil, `duplicate name "a"`},
		{"a a=1", nil, `duplicate name "a"`},
		{"a f(b)", nil, "call f(b); want name or name=value"},
	}

	for _, c := range cases {
		t.Run(c.args, func(t *testing.T) {
			kv, err := ParseArgsKeyValue(c.args)
			assert.NilError(t, err)

			got, err := KeyValueMap(kv)
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
				return
			}

			assert.NilError(t, err)
			assert.DeepEqual(t, got, c.want)
		})
	}
}
