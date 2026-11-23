package partial

import (
	"context"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
)

// generateDependent generates the package in the sub directory of files
// first and stages its output. It then loads the package at the top of
// files alone and returns what Generate gives for it, keyed by base name,
// and the error of loading every package with that output staged too.
func generateDependent(
	t *testing.T,
	files map[string]string,
) (out map[string]string, genErr, buildErr error) {
	t.Helper()

	ctx := context.Background()
	f, all := gentest.Stage(t, files)
	top := "./" + gentest.InlineDir(t)

	generateInto(t, f, gopackages.Options{Patterns: []string{top + "/sub"}})

	pkgs, err := gopackages.Load(ctx, f, gopackages.Options{Patterns: []string{top}})
	assert.NilError(t, err)

	produced, warnings, genErr := Generate(pkgs, Options{Registry: registry(t), Now: gentest.Now})
	if genErr != nil {
		return nil, genErr, nil
	}

	assert.Equal(t, len(warnings), 0, "%v", warnings)

	gentest.Write(t, f, produced)

	return gentest.ByBase(produced), nil, gentest.Build(f, all)
}

// implementorSub declares targets generated before the dependent package
// is loaded, a type without a partial variant, and a type next to a type
// named like its partial variant that no generator wrote.
const implementorSub = `package sub

//kama:partial
type S struct{ X int }

//kama:partial
type L []S

//kama:partial
type G[T any] struct{ V T }

type Plain struct{ Y int }

type Fake struct{ Z int }

type FakePartial struct{ Z int }
`

// TestImplementor maps a type of a package outside the run to the partial
// variant its package already declares with the marker of the generator.
func TestImplementor(t *testing.T) {
	out, genErr, buildErr := generateDependent(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:partial\ntype A struct {\n" +
			"\tsub.S\n\tV sub.S\n\tP *sub.S\n\tL sub.L\n\tG sub.G[int]\n" +
			"\tItems []sub.S\n\tPlain sub.Plain\n\tFake sub.Fake\n}\n\n" +
			"//kama:partial\ntype List []sub.S\n",
		"sub/sub.go": implementorSub,
	})
	assert.NilError(t, genErr)
	assert.NilError(t, buildErr)

	src := out["a.kama_partial.go"]

	// The run declares no partial variant of a type outside it.
	assert.DeepEqual(t, partialTypes(t, src), []string{"APartial", "ListPartial"})

	got := opiter.WrapSeq2(maps.All(fieldTypes(t, src, "APartial"))).
		Map(withoutTag).
		Collect(maps.Collect)

	assert.DeepEqual(t, got, map[string]string{
		"embedded sub.SPartial": "sub.SPartial",
		"V":                     "und.Und[sub.SPartial]",
		"P":                     "und.Und[sub.SPartial]",
		"L":                     "und.Und[sub.LPartial]",
		"G":                     "und.Und[sub.GPartial[int]]",
		// A slice maps as a whole, as a slice of targets does.
		"Items": "und.Und[[]sub.S]",
		// A type without a partial variant, and a type whose package only
		// declares a type named like one, map as a whole.
		"Plain": "und.Und[sub.Plain]",
		"Fake":  "und.Und[sub.Fake]",
	})
	assert.Assert(t, strings.Contains(src, "type ListPartial []und.Und[sub.SPartial]"), src)

	fs := funcs(t, src)
	assert.Assert(t, strings.Contains(fs["APartial.FromValue"], "p.SPartial.FromValue(v.S)"),
		fs["APartial.FromValue"])
	assert.Assert(t, strings.Contains(fs["APartial.ToValue"], "v.V = p.V.Value().ToValue()"),
		fs["APartial.ToValue"])
	assert.Assert(
		t,
		strings.Contains(fs["APartial.Merge"], "p.SPartial = p.SPartial.Merge(r.SPartial)"),
		fs["APartial.Merge"],
	)
}

// TestImplementorOfTheRun gives no partial form to a type of a loaded
// package the run leaves out, whatever a file of an earlier run declares.
func TestImplementorOfTheRun(t *testing.T) {
	files := map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ I I }\n\n" +
			"//kama:partial ignore\ntype I struct{ N int }\n",
		"stale.go": "package x\n\n// IPartial is left by an earlier run.\n//\n" +
			"//kama:generated kama-partial\ntype IPartial struct{}\n",
	}

	out, _, err := Generate(
		gentest.Load(t, files),
		Options{Registry: registry(t), Now: gentest.Now},
	)
	assert.NilError(t, err)

	for _, o := range out {
		if filepath.Base(o.Path) != "a.kama_partial.go" {
			continue
		}

		got := opiter.WrapSeq2(maps.All(fieldTypes(t, string(o.Content), "APartial"))).
			Map(withoutTag).
			Collect(maps.Collect)

		assert.DeepEqual(t, got, map[string]string{"I": "und.Und[I]"})

		return
	}

	t.Fatal("a.kama_partial.go is not generated")
}

// TestImplementorFieldMask addresses the fields of an implementor with
// field masks below its field, and rejects an embedded implementor, whose
// paths the run cannot know.
func TestImplementorFieldMask(t *testing.T) {
	sub := "package sub\n\n//kama:partial\n//kama:partial:fieldmask\ntype S struct{ X int }\n"

	out, genErr, buildErr := generateDependent(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n\tV sub.S\n\tP *sub.S\n}\n",
		"sub/sub.go": sub,
	})
	assert.NilError(t, genErr)
	assert.NilError(t, buildErr)

	fs := funcs(t, out["a.kama_partial.go"])
	assert.Assert(t, strings.Contains(fs["APartial.FieldMask"], "p.V.Value().FieldMask()"),
		fs["APartial.FieldMask"])

	_, genErr, _ = generateDependent(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:partial\n//kama:partial:fieldmask\ntype A struct {\n\tsub.S\n}\n",
		"sub/sub.go": sub,
	})
	assert.ErrorIs(t, genErr, ErrUnsupported)
	assert.ErrorContains(t, genErr, "declared outside the packages of the run")

	// The fields of A are those of sub.T, declared outside the run.
	_, genErr, _ = generateDependent(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:partial\n//kama:partial:fieldmask\ntype A sub.T\n",
		"sub/sub.go": sub + "\ntype T struct {\n\tS\n\tN int\n}\n",
	})
	assert.ErrorIs(t, genErr, ErrUnsupported)
	assert.ErrorContains(t, genErr, "sub.go:8:2): embedded")
}

// TestImplementorEmbeddedPointer builds the helpers of an embedded pointer
// to an implementor, which test the fields of its partial variant.
func TestImplementorEmbeddedPointer(t *testing.T) {
	out, genErr, buildErr := generateDependent(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:partial\ntype A struct {\n\t*sub.S\n\tN int\n}\n",
		"sub/sub.go": implementorSub,
	})
	assert.NilError(t, genErr)
	assert.NilError(t, buildErr)

	helpers := out[HelpersFile]
	helperIn(t, helpers)("AllUndefined", "every field of p, a sub.SPartial, is undefined")
	helperIn(t, helpers)("ToEmbedded", "converts sub.SPartial into *sub.S,")
}

// TestImplementorHoldingLoadedType builds the helpers of an embedded
// pointer to an implementor that embeds a type of a loaded package. The
// run reaches that type only through the implementor and leaves it out, so
// the partial variant of the implementor holds it whole, as the package of
// the implementor generated it without that type.
func TestImplementorHoldingLoadedType(t *testing.T) {
	ctx := context.Background()
	f, all := gentest.Stage(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:partial\ntype A struct {\n\t*sub.S\n\tN int\n}\n",
		"other/other.go": "package other\n\ntype O struct{ N int }\n",
		"sub/sub.go": "package sub\n\nimport \"" + modulePath(t) + "/other\"\n\n" +
			"//kama:partial\ntype S struct {\n\tother.O\n\tX int\n}\n",
	})
	top := "./" + gentest.InlineDir(t)

	generateInto(t, f, gopackages.Options{Patterns: []string{top + "/sub"}})

	pkgs, err := gopackages.Load(
		ctx,
		f,
		gopackages.Options{Patterns: []string{top, top + "/other"}},
	)
	assert.NilError(t, err)

	out, _, err := Generate(pkgs, Options{Registry: registry(t), Now: gentest.Now})
	assert.NilError(t, err)

	gentest.Write(t, f, out)
	assert.NilError(t, gentest.Build(f, all))

	helperIn(t, gentest.ByBase(out)[HelpersFile])(
		"AllUndefined", "every field of p, a sub.SPartial, is undefined",
	)
}

// TestImplementorFieldMaskPromoted rejects an embedded implementor with
// field masks in a target without them, which would promote its methods.
func TestImplementorFieldMaskPromoted(t *testing.T) {
	_, genErr, _ := generateDependent(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:partial\ntype A struct {\n\tsub.S\n\tN int\n}\n",
		"sub/sub.go": "package sub\n\n//kama:partial\n//kama:partial:fieldmask\n" +
			"type S struct{ X int }\n",
	})
	assert.ErrorIs(t, genErr, ErrUnsupported)
	assert.ErrorContains(t, genErr, "has field masks")
}

// TestImplementorSameAsTarget generates a package alone, with the package
// it depends on generated before, and together with that package. Both
// ways give the same files for it.
func TestImplementorSameAsTarget(t *testing.T) {
	cases := map[string]map[string]string{
		// sub.Plain is left out: loaded together it is reachable and so a target.
		"plain": {
			"a.go": "package x\n\nimport \"%s/sub\"\n\n" +
				"//kama:partial\ntype A struct {\n" +
				"\tsub.S\n\tV sub.S\n\tP *sub.S\n\tL sub.L\n\tG sub.G[int]\n" +
				"\tItems []sub.S\n}\n\n" +
				"//kama:partial\ntype B struct {\n\t*sub.S\n}\n\n" +
				"//kama:partial\ntype List []sub.S\n",
			"sub/sub.go": implementorSub,
		},
		"field masks": {
			"a.go": "package x\n\nimport \"%s/sub\"\n\n" +
				"//kama:partial\n//kama:partial:fieldmask\n" +
				"type A struct {\n\tV sub.S\n\tP *sub.S\n\tN sub.N\n}\n",
			"sub/sub.go": "package sub\n\n//kama:partial\n//kama:partial:fieldmask\n" +
				"type S struct{ X int }\n\n//kama:partial\ntype N struct{ Y int }\n",
		},
	}

	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			// The import path of sub holds the directory of the subtest.
			files["a.go"] = strings.ReplaceAll(files["a.go"], "%s", modulePath(t))

			alone, genErr, buildErr := generateDependent(t, files)
			assert.NilError(t, genErr)
			assert.NilError(t, buildErr)

			together := generated(t, files)

			for _, file := range []string{"a.kama_partial.go", HelpersFile} {
				assert.Equal(
					t,
					withoutRunParams(alone[file]),
					withoutRunParams(together[file]),
					file,
				)
			}
		})
	}
}
