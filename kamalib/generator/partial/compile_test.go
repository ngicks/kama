package partial

import (
	"context"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// stage writes files under the inline test directory of t and returns the
// overlay and the options loading their packages.
func stage(t *testing.T, files map[string]string) (*overlayfs.Fs, gopackages.Options) {
	t.Helper()

	f, err := overlayfs.New(".")
	assert.NilError(t, err)
	t.Cleanup(func() { f.Close() })

	dir := "testdata/inline/" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dirs := map[string]bool{}
	for name, src := range files {
		assert.NilError(t, f.Write(context.Background(), path.Join(dir, name), []byte(src)))
		dirs["./"+path.Dir(path.Join(dir, name))] = true
	}
	return f, gopackages.Options{Patterns: slices.Sorted(maps.Keys(dirs))}
}

// generateInto generates from the packages opts loads from f and stages
// the output next to them.
func generateInto(t *testing.T, f *overlayfs.Fs, opts gopackages.Options) {
	t.Helper()

	ctx := context.Background()
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
}

// generateAndBuild generates from files as load does, stages the output
// next to them and loads the packages again with buildFlags, returning the
// error of that second load, which reports any package error.
func generateAndBuild(t *testing.T, files map[string]string, buildFlags ...string) error {
	t.Helper()

	f, opts := stage(t, files)
	generateInto(t, f, opts)
	opts.BuildFlags = buildFlags
	_, err := gopackages.Load(context.Background(), f, opts)
	return err
}

// TestHelpersAcrossBuildVariants generates with a constrained file loaded,
// then with it left out, and builds with it included: the partial file of
// the constrained file from the first run keeps finding the helpers it
// calls.
func TestHelpersAcrossBuildVariants(t *testing.T) {
	f, opts := stage(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ N *N }\n\ntype N struct{ I int }\n",
		"b_tagged.go": "//go:build tagged\n\npackage x\n\n//kama:partial\ntype B struct {\n" +
			"\t//kama:partial:merge append\n\tS []int\n\tN *N\n}\n",
	})
	tagged := []string{"-tags=tagged"}

	opts.BuildFlags = tagged
	generateInto(t, f, opts)
	read := func(name string) string {
		t.Helper()
		b, err := fs.ReadFile(f.FS(), path.Join(opts.Patterns[0], name))
		assert.NilError(t, err)
		return string(b)
	}
	a, b := read("a.kama_partial.go"), read("b_tagged.kama_partial.go")
	h := helperIn(t, a, b)
	fromN := "\nfunc " + h("FromTarget", "converts *N into und.Und[NPartial],") + "("
	appendInts := "\nfunc " + h("MergeAppend", "both und.Und[[]int]:") + "("
	// A helper both files call lives in the file without a constraint,
	// which every build variant has.
	assert.Assert(t, strings.Contains(a, fromN), a)
	assert.Assert(t, !strings.Contains(b, fromN), b)
	assert.Assert(t, strings.Contains(b, appendInts), b)

	opts.BuildFlags = nil
	generateInto(t, f, opts)
	assert.Assert(t, genhelper.SameExceptGenerated([]byte(read("a.kama_partial.go")), []byte(a)))

	opts.BuildFlags = tagged
	_, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)
}

// TestHelperOfConstrainedFiles builds the helpers a constrained file calls
// alone, which it declares itself.
func TestHelperOfConstrainedFiles(t *testing.T) {
	assert.NilError(t, generateAndBuild(t, map[string]string{
		"a_linux.go": "package x\n\n//kama:partial\ntype A struct{ N *N }\n",
		"b.go":       "package x\n\ntype N struct{ I int }\n\n//kama:partial\ntype B struct{ N N }\n",
	}))
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
	const src = "package x\n\n//kama:partial\ntype A struct {\n" +
		"\t//kama:partial:merge append\n\tN []int\n\tS *S\n\tE []E\n}\n\n" +
		"type S struct{ A *A }\n\n//kama:partial ignore\ntype E struct{}\n\ntype "
	// The generated code spells interface{} for any.
	assert.NilError(t, generateAndBuild(t, map[string]string{"a.go": src + "any int\n"}))

	for _, name := range []string{"make", "len", "append", "new", "nil"} {
		t.Run(name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"a.go": src + name + " int\n"}, Options{})
			assert.ErrorIs(t, err, ErrConflict)
			assert.ErrorContains(t, err, "a.go:16:6")
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
