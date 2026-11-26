package genopt

import (
	"fmt"
	"testing"

	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

var testLoader = Loader[leaf, leaf, leaf]{ToolName: "kama", Generator: "x", Version: 1}

const directivesSrc = `// Package a.
//
//kama:x package
package a

//kama:x file
//kama:x:sub one
//kama:other ignored
//kama:xx ignored
//kama:y ignored

// Single is a type.
//
//kama:x
//other:x ignored
type Single struct {
	//kama:x:merge append
	//kama:x:merge more=1
	A []string
	B string //kama:x:flag name="addr", usage="listen address"
	C, D int //kama:x:both
	*Embedded //kama:x:embedded
	Nested struct {
		//kama:x:nested
		E int
	}
}

//kama:x:group
type (
	//kama:x grouped
	G1 int
	G2 struct {
		//kama:x:g2
		F int
	}
)

type Embedded struct{}

type OnlyField struct {
	//kama:x:only
	F int
}

//kama:x func
func F() {}
`

func TestLoadPackages(t *testing.T) {
	pkg := parsedPackage(t, "m/a", "a", map[string]string{"x.go": directivesSrc})

	loaded, err := testLoader.LoadPackages([]*packages.Package{pkg})
	assert.NilError(t, err)
	assert.Equal(t, len(loaded.Warnings), 0)
	assert.DeepEqual(t, flatten(loaded.Option), map[string]string{
		"m/a file x.go": "[]package, file; [sub]one; " +
			"at x.go:3:1 a free-floating comment, x.go:7:1 a free-floating comment",
		"m/a.Single":      "a/x.go:14:1 fields=true",
		"m/a.Single type": "[]; at x.go:14:1 type m/a.Single",
		"m/a.Single.A":    "[merge]append, more=1; at x.go:17:2 field m/a.Single.A",
		"m/a.Single.B": `[flag]name="addr", usage="listen address"; ` +
			"at x.go:20:11 field m/a.Single.B",
		"m/a.Single.C":        "[both]; at x.go:21:11 field m/a.Single.C",
		"m/a.Single.D":        "[both]; at x.go:21:11 field m/a.Single.D",
		"m/a.Single.Embedded": "[embedded]; at x.go:22:12 field m/a.Single.Embedded",
		"m/a.G1":              "a/x.go:29:1 fields=false",
		"m/a.G1 group":        "[group]; at x.go:29:1 a type group",
		"m/a.G1 type":         "[]grouped; at x.go:31:2 type m/a.G1",
		"m/a.G2":              "a/x.go:29:1 fields=true",
		"m/a.G2 group":        "[group]; at x.go:29:1 a type group",
		"m/a.G2.F":            "[g2]; at x.go:34:3 field m/a.G2.F",
		"m/a.OnlyField":       "a/x.go:42:2 fields=true",
		"m/a.OnlyField.F":     "[only]; at x.go:42:2 field m/a.OnlyField.F",
	})
	assert.DeepEqual(t, nodeKinds(loaded.Option), map[string]string{
		"m/a.Single":    "TypeSpec",
		"m/a.G1":        "TypeSpec",
		"m/a.G2":        "TypeSpec",
		"m/a.OnlyField": "TypeSpec",
	})
}

func TestLoadPackagesFilter(t *testing.T) {
	pkg := parsedPackage(t, "m/a", "a", map[string]string{"x.go": directivesSrc})

	l := testLoader
	l.Filter = IgnoreFreeFloating

	loaded, err := l.LoadPackages([]*packages.Package{pkg})
	assert.NilError(t, err)

	got := flatten(loaded.Option)
	_, ok := got["m/a file x.go"]
	assert.Assert(t, !ok, "%v", got)
	assert.Equal(t, got["m/a.Single type"], "[]; at x.go:14:1 type m/a.Single")

	l.Filter = func(t Target) bool { return t.Kind != TargetField }

	loaded, err = l.LoadPackages([]*packages.Package{pkg})
	assert.NilError(t, err)
	assert.DeepEqual(
		t,
		flatten(loaded.Option)["m/a.Single type"],
		"[]; at x.go:14:1 type m/a.Single",
	)
	_, ok = flatten(loaded.Option)["m/a.OnlyField"]
	assert.Assert(t, !ok)
}

// TestLoadPackagesTypes checks that a package with types tells struct types
// by their types: a type defined on a struct type is one.
func TestLoadPackagesTypes(t *testing.T) {
	src := "package a\n\ntype S struct{ F int }\n\n//kama:x\ntype D S\n\n//kama:x\ntype I int\n"

	loaded, err := testLoader.LoadPackages([]*packages.Package{
		typedPackage(t, "m/a", "a", map[string]string{"x.go": src}),
	})
	assert.NilError(t, err)

	got := flatten(loaded.Option)
	assert.Equal(t, got["m/a.D"], "a/x.go:5:1 fields=true")
	assert.Equal(t, got["m/a.I"], "a/x.go:8:1 fields=false")

	loaded, err = testLoader.LoadPackages([]*packages.Package{
		parsedPackage(t, "m/a", "a", map[string]string{"x.go": src}),
	})
	assert.NilError(t, err)
	assert.Equal(t, flatten(loaded.Option)["m/a.D"], "a/x.go:5:1 fields=false")
}

func TestLoadPackagesErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		is   error
		// contains are substrings the error must hold.
		contains []string
	}{
		{
			name:     "unparsable args",
			src:      "package a\n\n//kama:x a=\"b\ntype T int\n",
			is:       ErrDirective,
			contains: []string{"x.go:3:1: //kama:x: ", "unexpected newline in string"},
		},
		{
			name:     "leaf error",
			src:      "package a\n\ntype T struct {\n\t//kama:x:fail\n\tF int\n}\n",
			is:       errLeaf,
			contains: []string{"x.go:4:2"},
		},
		{
			name:     "leaf error of a group",
			src:      "package a\n\n//kama:x:fail\ntype (\n\tA int\n\tB int\n)\n",
			is:       errLeaf,
			contains: []string{"x.go:3:1"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := testLoader.LoadPackages([]*packages.Package{
				parsedPackage(t, "m/a", "a", map[string]string{"x.go": c.src}),
			})
			assert.ErrorIs(t, err, c.is)

			for _, s := range c.contains {
				assert.ErrorContains(t, err, s)
			}
		})
	}
}

// TestLoadPackagesLines checks that each line of one directive on one
// target parses on its own: a quote cannot span lines, and a line needs no
// trailing comma.
func TestLoadPackagesLines(t *testing.T) {
	loaded, err := testLoader.LoadPackages([]*packages.Package{
		parsedPackage(t, "m/a", "a", map[string]string{
			"x.go": "package a\n\n//kama:x a, b=\"c\"\n//kama:x d\ntype T int\n",
		}),
	})
	assert.NilError(t, err)
	assert.Equal(t, flatten(loaded.Option)["m/a.T type"], `[]a, b="c", d; at x.go:3:1 type m/a.T`)

	_, err = testLoader.LoadPackages([]*packages.Package{
		parsedPackage(t, "m/a", "a", map[string]string{
			"x.go": "package a\n\n//kama:x a=\"b\n//kama:x c\" d\ntype T int\n",
		}),
	})
	assert.ErrorIs(t, err, ErrDirective)
	assert.ErrorContains(t, err, "x.go:3:1: //kama:x: expr: 1:")
}

// TestLoadPackagesPositions checks that the positions of the items count
// lines from the first line of the directive, and columns from the start
// of the args of their line.
func TestLoadPackagesPositions(t *testing.T) {
	l := Loader[leaf, starts, leaf]{ToolName: "kama", Generator: "x"}

	loaded, err := l.LoadPackages([]*packages.Package{
		parsedPackage(t, "m/a", "a", map[string]string{
			"x.go": "package a\n\n//kama:x a, b\n// text\n//kama:x   c\ntype T int\n",
		}),
	})
	assert.NilError(t, err)

	to, _ := loaded.Option.Type(typegraph.NewQualifiedType("m/a", "T"))
	assert.DeepEqual(t, to.Type.at, []string{"a 1:1", "b 1:4", "c 2:3"})

	_, err = l.LoadPackages([]*packages.Package{
		parsedPackage(t, "m/a", "a", map[string]string{
			"x.go": "package a\n\n//kama:x a\n//kama:x b c\ntype T int\n",
		}),
	})
	assert.ErrorIs(t, err, ErrDirective)
	assert.ErrorContains(t, err, "x.go:3:1: //kama:x: expr: 2:")
}

// starts is a leaf recording the items and their positions.
type starts struct{ at []string }

func (s *starts) UnmarshalExpr(_ Source, _ string, args []expr.Arg) error {
	for _, a := range args {
		s.at = append(s.at, fmt.Sprintf("%s %d:%d", a, a.Start.Line, a.Start.Column))
	}

	return nil
}

func TestOldSpellingHint(t *testing.T) {
	// Each row maps args to the suggested spelling, or to "" for no hint.
	for args, suggested := range map[string]string{
		`name=b short=s usage="the b"`: `name="b", short="s", usage="the b"`,
		"append overwrite":             "append, overwrite",
		`hook=True pre="f"`:            `hook=True, pre="f"`,
		"name=timeout short=t":         `name="timeout", short="t"`,
		"more=1 name=x":                `more=1, name="x"`,
		`short="1" name=x`:             `short="1", name="x"`,
		"a= b":                         `a="", b`,
		`length=">=1" or(a)`:           "",
		">=1 b":                        "",
		`a="b`:                         "",
		"":                             "",
	} {
		if suggested == "" {
			assert.Equal(t, oldSpellingHint(args), "", args)
			continue
		}

		assert.Equal(
			t,
			oldSpellingHint(args),
			"; separate items with commas and quote strings, as in "+suggested,
			args,
		)

		_, err := expr.Parse(suggested)
		assert.NilError(t, err, args)
	}

	_, err := testLoader.LoadPackages([]*packages.Package{
		parsedPackage(t, "m/a", "a", map[string]string{
			"x.go": "package a\n\n//kama:x a=b c\ntype T int\n",
		}),
	})
	assert.ErrorContains(t, err, `x.go:3:1: //kama:x: expr: 1:`)
	assert.ErrorContains(t, err, `as in a="b", c`)
}

func TestWord(t *testing.T) {
	for text, want := range map[string]string{
		"ignore":   "ignore",
		"ignore()": "",
		`a="b"`:    "",
		"a or b":   "",
	} {
		args, err := expr.Parse(text)
		assert.NilError(t, err)

		w, ok := Word(args[0])
		assert.Equal(t, w, want, text)
		assert.Equal(t, ok, want != "", text)
	}
}

func TestKeywordString(t *testing.T) {
	for text, want := range map[string]string{
		`name="a b"`: "",
		"name=a":     `name=a; want a string, as in name="a"`,
		"name=1.5":   `name=1.5; want a string, as in name="1.5"`,
		"name=a()":   `name=a(); want name="<text>"`,
		"name=True":  `name=True; want name="<text>"`,
	} {
		args, err := expr.Parse(text)
		assert.NilError(t, err)

		v, err := KeywordString(args[0])
		if want == "" {
			assert.NilError(t, err)
			assert.Equal(t, v, "a b")

			continue
		}

		assert.Error(t, err, want, text)
	}
}

// plain is a leaf taking no directives.
type plain struct {
	Value string `json:"value"`
}

func TestLoadPackagesWithoutUnmarshaler(t *testing.T) {
	l := Loader[plain, leaf, plain]{ToolName: "kama", Generator: "x"}
	pkg := parsedPackage(t, "m/a", "a", map[string]string{
		"x.go": "package a\n\n//kama:x\ntype T struct {\n\t//kama:x:merge\n\tF int\n}\n\n" +
			"//kama:x file\n",
	})

	_, err := l.LoadPackages([]*packages.Package{pkg})
	assert.ErrorIs(t, err, ErrDirective)
	assert.ErrorContains(
		t,
		err,
		"x.go:5:2: //kama:x:merge on field m/a.T.F; *genopt.plain takes no directives",
	)
	assert.ErrorContains(t, err, "x.go:9:1: //kama:x on a free-floating comment")
}

func TestLoadPackagesWithoutSyntax(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName}, "./internal/fixture")
	assert.NilError(t, err)

	_, err = testLoader.LoadPackages(pkgs)
	assert.ErrorContains(t, err, "without syntax")
}

// TestLoadPackagesTestVariant checks that a package path given twice counts
// once, taking the test variant rather than the package.
func TestLoadPackagesTestVariant(t *testing.T) {
	variant := parsedPackage(t, "m/a", "a", map[string]string{
		"x.go":      "package a\n\n//kama:x\ntype T int\n",
		"x_test.go": "package a\n\n//kama:x\ntype U int\n",
	})
	variant.ID = "m/a [m/a.test]"

	loaded, err := testLoader.LoadPackages([]*packages.Package{
		variant,
		parsedPackage(
			t,
			"m/a",
			"a",
			map[string]string{"x.go": "package a\n\n//kama:x\ntype T int\n"},
		),
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, flatten(loaded.Option), map[string]string{
		"m/a.T":      "a/x.go:3:1 fields=false",
		"m/a.T type": "[]; at x.go:3:1 type m/a.T",
		"m/a.U":      "a/x_test.go:3:1 fields=false",
		"m/a.U type": "[]; at x_test.go:3:1 type m/a.U",
	})
}
