package partial

import (
	"context"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// generateAndBuild generates from files as load does, stages the output
// next to them and loads the packages again with buildFlags, returning the
// error of that second load, which reports any package error.
func generateAndBuild(t *testing.T, files map[string]string, buildFlags ...string) error {
	t.Helper()

	ctx := context.Background()
	f, err := overlayfs.New(".")
	assert.NilError(t, err)
	t.Cleanup(func() { f.Close() })

	dir := "testdata/inline/" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dirs := map[string]bool{}
	for name, src := range files {
		assert.NilError(t, f.Write(ctx, path.Join(dir, name), []byte(src)))
		dirs["./"+path.Dir(path.Join(dir, name))] = true
	}
	opts := gopackages.Options{Patterns: slices.Sorted(maps.Keys(dirs))}

	pkgs, err := gopackages.Load(ctx, f, opts)
	assert.NilError(t, err)
	out, warnings, err := Generate(pkgs, Options{Registry: registry(t)})
	assert.NilError(t, err)
	// A partial file of a build variant not loaded is not stale.
	assert.Equal(t, len(warnings), 0, "%v", warnings)
	for _, o := range out {
		rel, err := filepath.Rel(f.Root(), o.Path)
		assert.NilError(t, err)
		assert.NilError(t, f.Write(ctx, filepath.ToSlash(rel), o.Content))
	}

	opts.BuildFlags = buildFlags
	_, err = gopackages.Load(ctx, f, opts)
	return err
}

// TestHelpersAcrossBuildVariants generates with a constrained file left
// out, and builds with it included: the constrained partial file from an
// earlier run calls helpers no loaded target uses.
func TestHelpersAcrossBuildVariants(t *testing.T) {
	files := map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ N int }\n",
		"b_tagged.go": "//go:build tagged\n\npackage x\n\n//kama:partial\ntype B struct {\n" +
			"\t//kama:partial:merge append\n\tS []int\n\tM map[string]int\n\tP *int\n}\n",
		// The partial file of b_tagged.go as an earlier run with the tag gave
		// it.
		"b_tagged.kama_partial.go": "//go:build tagged\n\npackage x\n\n" +
			"import \"github.com/ngicks/option/und\"\n\n" +
			"//kama:generated kama-partial\ntype BPartial struct {\n" +
			"\tS und.Und[[]int]\n\tM und.Und[map[string]int]\n\tP und.Und[int]\n}\n\n" +
			"//kama:generated kama-partial\nfunc (p BPartial) Merge(r BPartial) BPartial {\n" +
			"\tp.S = kamaPartialMergeAppendSlice(p.S, r.S)\n" +
			"\tp.M = kamaPartialMergeOverwrite(p.M, r.M)\n" +
			"\tp.P = kamaPartialMergeOverwrite(p.P, r.P)\n\treturn p\n}\n\n" +
			"//kama:generated kama-partial\nfunc (p *BPartial) FromValue(v B) {\n" +
			"\tp.S = kamaPartialFromSlice(v.S)\n\tp.M = kamaPartialFromMap(v.M)\n" +
			"\tp.P = kamaPartialFromPointer(v.P)\n}\n",
	}
	assert.NilError(t, generateAndBuild(t, files, "-tags=tagged"))

	got := generated(t, files)[HelpersFile]
	for _, h := range helperNames() {
		assert.Assert(
			t,
			strings.Contains(got, "\nfunc "+h+"[") || strings.Contains(got, "\nfunc "+h+"("),
			"%s is missing",
			h,
		)
	}
}

func TestForeignUnexportedFields(t *testing.T) {
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:partial\ntype A sub.Exported\n",
		"sub/sub.go": "package sub\n\ntype Exported struct {\n\tPublic int\n\tprivate int\n}\n",
	}, Options{})
	assert.ErrorIs(t, err, ErrUnsupported)
	for _, s := range []string{"Fields.A (", "field private", "sub.go:5:2", "unexported"} {
		assert.ErrorContains(t, err, s)
	}

	_, err = generate(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:partial\ntype A sub.Exported\n",
		"sub/sub.go": "package sub\n\ntype Exported struct {\n\tPublic int\n\thidden\n}\n\n" +
			"type hidden struct{ N int }\n",
	}, Options{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorContains(t, err, "field hidden")
}

// modulePath returns the import path of the inline test directory of t.
func modulePath(t *testing.T) string {
	t.Helper()
	return "github.com/ngicks/kama/kamalib/generator/partial/testdata/inline/" +
		strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
}

func TestBlankTypeParams(t *testing.T) {
	assert.NilError(t, generateAndBuild(t, map[string]string{
		"a.go": `package x

//kama:partial
type Box[_ any] struct{ N int }

//kama:partial
type Pair[_, T any, _ comparable] struct{ V T }

//kama:partial
type L[_ any, T any] []Item[T]

//kama:partial
type M[_ any, T any] map[string]Item[T]

//kama:partial
type MP[_ any, T any] map[string]*Item[T]

type Item[T any] struct{ V T }
`,
	}))
}

func TestPredeclaredShadowed(t *testing.T) {
	// The generated code spells interface{} for any.
	assert.NilError(t, generateAndBuild(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ N []int }\n\ntype any int\n",
	}))

	for _, name := range []string{
		"comparable", "make", "len", "append", "new", "error", "nil", "string", "byte",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := generate(t, map[string]string{
				"a.go": "package x\n\n//kama:partial\ntype A struct{ N []int }\n\ntype " +
					name + " int\n",
			}, Options{})
			assert.ErrorIs(t, err, ErrConflict)
			assert.ErrorContains(t, err, "a.go:6:6")
			assert.ErrorContains(t, err, "predeclared "+name)
		})
	}
}

func TestTypeParamNamedLikePredeclared(t *testing.T) {
	for _, name := range []string{"error", "nil", "false"} {
		t.Run(name, func(t *testing.T) {
			_, err := generate(t, map[string]string{
				"a.go": "package x\n\n//kama:partial\ntype A[" + name + " any] struct {\n" +
					"\t//kama:partial:flag name=b\n\tB bool\n\tV " + name + "\n}\n",
			}, Options{})
			assert.ErrorIs(t, err, ErrConflict)
			assert.ErrorContains(t, err, "a.go:4:8")
			assert.ErrorContains(t, err, "type parameter "+name+" of A")
		})
	}
}

func TestImportNamedLikePredeclared(t *testing.T) {
	assert.NilError(t, generateAndBuild(t, map[string]string{
		"a.go": "package x\n\nimport s \"" + modulePath(t) + "/string\"\n\n" +
			"//kama:partial\ntype A struct {\n\tT s.T\n\tN string\n}\n",
		"string/string.go": "package string\n\ntype T struct{ N int }\n",
	}))
}
