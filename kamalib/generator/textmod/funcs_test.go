package textmod

import (
	"math/big"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/ngicks/go-common/exver"
	"gotest.tools/v3/assert"
)

func execute(t *testing.T, src string, data any) (string, error) {
	t.Helper()

	funcs, err := Funcs(Run{})
	assert.NilError(t, err)

	tmpl, err := ParseMod("t", src, funcs)
	assert.NilError(t, err)

	var b strings.Builder

	err = tmpl.Execute(&b, data)

	return b.String(), err
}

func TestBuiltinFuncs(t *testing.T) {
	huge, _ := new(big.Int).SetString("1180591620717411303424", 10)
	data := map[string]any{
		"ver":  exver.MustParse("v1.2.3-rc.1+b5"),
		"dev":  exver.MustParse("v0.0.0-devel"),
		"at":   time.Date(2026, 1, 2, 3, 4, 5, 6, time.FixedZone("x", 3600)),
		"huge": huge,
		"text": "  a\n    b\n\n  c\n",
		"help": "usage\n# begin\nold\n# end\nrest\n",
		"none": "",
	}

	for _, c := range []struct{ src, want string }{
		{`{{ parseVer "1.2.3" }}`, "1.2.3"},
		{`{{ verCompare .ver "v1.2.3" }}`, "-1"},
		{`{{ .dev | withPre "" }}`, "v0.0.0"},
		{`{{ .ver | withPre "" }}`, "v1.2.3+b5"},
		{`{{ .ver | withBuild "" }}`, "v1.2.3-rc.1"},
		{`{{ .dev | withPre "" | withCore 0 1 0 | withPre "devel" }}`, "v0.1.0-devel"},
		{`{{ .ver | withoutV }}`, "1.2.3-rc.1+b5"},
		{`{{ "1.2.3" | withV }}`, "v1.2.3"},
		{`{{ verMajor .ver }}.{{ verMinor .ver }}.{{ verPatch .ver }}.{{ verExtra .ver }}`, "1.2.3.0"},
		{`{{ verPre .ver }} {{ verBuild .ver }}`, "rc.1 b5"},
		{`{{ "v1.2.3" | withCore 4 5 6 7 }}`, "v4.5.6.7"},
		{`{{ .at | rfc3339 }}`, "2026-01-02T03:04:05.000000006+01:00"},
		{`{{ .at | utc | rfc3339 }}`, "2026-01-02T02:04:05.000000006Z"},
		{`{{ .at | addDuration "1h30m" | formatTime "15:04" }}`, "04:34"},
		{`{{ .at | addDate 0 1 1 | formatTime "2006-01-02" }}`, "2026-02-03"},
		{`{{ .at | truncate "1h" | utc | rfc3339 }}`, "2026-01-02T02:00:00Z"},
		{`{{ parseTime "2006-01-02" "2026-03-04" | rfc3339 }}`, "2026-03-04T00:00:00Z"},
		{`{{ "2026-03-04T00:00:00Z" | addDuration "24h" | rfc3339 }}`, "2026-03-05T00:00:00Z"},
		{`{{ add 1 2 }} {{ sub 1 2 }} {{ mul 3 4 }} {{ div 7 2 }} {{ mod 7 2 }}`, "3 -1 12 3 1"},
		{`{{ add 1 0.5 }} {{ div 7.0 2 }}`, "1.5 3.5"},
		{`{{ add .huge 1 }}`, "1180591620717411303425"},
		{`{{ min 3 1 2 }} {{ max 3 1 2.5 }}`, "1 3"},
		{`{{ toInt 2.9 }} {{ toInt "0x10" }} {{ toFloat 2 }}`, "2 16 2"},
		{`{{ upper "a" }}{{ lower "B" }}{{ trimSpace " c " }}`, "Abc"},
		{`{{ trim "x" "xax" }}{{ trimLeft "x" "xb" }}{{ trimRight "x" "cx" }}`, "abc"},
		{`{{ trimPrefix "p" "pa" }}{{ trimSuffix "s" "bs" }}`, "ab"},
		{`{{ replace "a" "b" "aa" }}`, "bb"},
		{`{{ hasPrefix "a" "ab" }} {{ hasSuffix "a" "ab" }}`, "true false"},
		{`{{ contains "b" "abc" }}`, "true"},
		{`{{ split "," "a,b" | join "+" }}`, "a+b"},
		{`{{ repeat 3 "a" }}`, "aaa"},
		{`{{ quote "a\"b" }}`, `"a\"b"`},
		{`{{ indent "> " "a\n\nb\n" }}`, "> a\n\n> b\n"},
		{`{{ dedent .text }}`, "a\n  b\n\nc\n"},
		{`{{ .help | replaceBetween "# begin" "# end" "new" }}`, "usage\n# begin\nnew\n# end\nrest\n"},
		{`{{ .help | replaceBetween "# begin" "# end" "" }}`, "usage\n# begin\n# end\nrest\n"},
		{`{{ default "d" .none }} {{ default "d" "v" }} {{ default 5 0 }}`, "d v 5"},
		{`{{ coalesce .none 0 "x" }}`, "x"},
		{`{{ ternary "a" "b" true }}{{ ternary "a" "b" false }}`, "ab"},
	} {
		got, err := execute(t, c.src, data)
		assert.NilError(t, err, c.src)
		assert.Equal(t, got, c.want, c.src)
	}
}

func TestBuiltinFuncsErrors(t *testing.T) {
	for _, c := range []struct{ src, err string }{
		{`{{ parseVer "x" }}`, "parseVer"},
		{`{{ withPre "a b" "1.2.3" }}`, "withPre"},
		{`{{ withCore 1 "1.2.3" | withCore 10000 }}`, "withCore"},
		{`{{ withCore "1.2.3" }}`, "takes 1 to 4 numbers"},
		{`{{ addDuration "1x" "2026-01-02T00:00:00Z" }}`, "addDuration"},
		{`{{ utc 1 }}`, "is not a date-time"},
		{`{{ div 1 0 }}`, "division by zero"},
		{`{{ add "a" 1 }}`, `"a" is not a number`},
		{`{{ repeat -1 "a" }}`, "negative repeat count"},
		{`{{ replaceBetween "x" "y" "" "a\nb\n" }}`, `no line holds "x"`},
		{`{{ replaceBetween "a" "y" "" "a\nb\n" }}`, `no line after "a" holds "y"`},
	} {
		_, err := execute(t, c.src, nil)
		assert.ErrorContains(t, err, c.err, c.src)
	}
}

// TestRegisterFuncs registers a provider binding a function to the vars of
// the run, and one redefining a built-in function.
func TestRegisterFuncs(t *testing.T) {
	unregister := func(name string) {
		providersMu.Lock()
		defer providersMu.Unlock()

		delete(providers, name)
	}

	RegisterFuncs("test.var", func(r Run) template.FuncMap {
		return template.FuncMap{"runVar": func(k string) any { return r.Vars[k] }}
	})
	t.Cleanup(func() { unregister("test.var") })

	funcs, err := Funcs(Run{Vars: map[string]any{"k": "v"}})
	assert.NilError(t, err)

	tmpl, err := ParseMod("t", `{{ runVar "k" }}`, funcs)
	assert.NilError(t, err)

	var b strings.Builder
	assert.NilError(t, tmpl.Execute(&b, nil))
	assert.Equal(t, b.String(), "v")

	assert.Assert(t, panics(func() { RegisterFuncs("test.var", nil) }))

	RegisterFuncs("test.clash", func(Run) template.FuncMap {
		return template.FuncMap{"upper": strings.ToUpper}
	})
	t.Cleanup(func() { unregister("test.clash") })

	_, err = Funcs(Run{})
	assert.ErrorContains(
		t,
		err,
		`function "upper" of funcs "test.clash" is defined by the built-in functions too`,
	)
}

func panics(fn func()) (p bool) {
	defer func() { p = recover() != nil }()

	fn()

	return false
}
