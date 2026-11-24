package partial

import (
	"context"
	"io/fs"
	"path"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// generateInto generates from the packages opts loads from f and stages
// the output next to them.
func generateInto(t *testing.T, f *overlayfs.Fs, opts gopackages.Options) {
	t.Helper()

	pkgs, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)
	out, warnings, err := runGenerate(t, f, pkgs, testRun{})
	assert.NilError(t, err)
	// A partial file of a build variant not loaded is not stale.
	assert.Equal(t, len(warnings), 0, "%v", warnings)

	gentest.Write(t, f, out)
}

// generateAndBuild generates from files as gentest.Load does, stages the
// output next to them and loads the packages again with buildFlags,
// returning the error of that second load, which reports any package error.
func generateAndBuild(t *testing.T, files map[string]string, buildFlags ...string) error {
	t.Helper()

	f, opts := gentest.Stage(t, files)
	generateInto(t, f, opts)
	opts.BuildFlags = buildFlags

	return gentest.Build(f, opts)
}

// TestHelpersAcrossBuildVariants generates with a constrained file loaded,
// then with it left out, and builds with it included: the helpers file of
// the package comes out the same, and the partial file of the constrained
// file from the first run keeps finding the helpers it calls.
func TestHelpersAcrossBuildVariants(t *testing.T) {
	f, opts := gentest.Stage(t, map[string]string{
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
	shared, own := read(HelpersFile), read("zz_gen.helper.b_tagged.kama_partial.go")
	h := helperIn(t, shared, own)
	fromN := "\nfunc " + h("FromTarget", "converts *N into und.Und[NPartial],") + "("
	appendInts := "\nfunc " + h("MergeAppend", "both und.Und[[]int]:") + "("
	// A helper a file without a constraint calls lives in the helpers file
	// of the package, which every build variant has.
	assert.Assert(t, strings.Contains(shared, fromN), shared)
	// A helper only the constrained file calls lives in a helpers file of
	// the same constraint.
	assert.Assert(t, strings.Contains(own, appendInts), own)
	assert.Assert(t, strings.Contains(own, "//go:build tagged\n"), own)

	for _, name := range []string{"a.kama_partial.go", "b_tagged.kama_partial.go"} {
		assert.Assert(t, !strings.Contains(read(name), "\nfunc kamaPartial"), read(name))
	}

	opts.BuildFlags = nil
	generateInto(t, f, opts)
	assert.Assert(t, genhelper.SameExceptGenerated([]byte(read(HelpersFile)), []byte(shared)))

	opts.BuildFlags = tagged
	_, err := gopackages.Load(context.Background(), f, opts)
	assert.NilError(t, err)
}

// TestHelperOfConstrainedFiles builds the helpers a file with a constraint
// in its name calls alone, which a helpers file of that constraint declares.
func TestHelperOfConstrainedFiles(t *testing.T) {
	files := map[string]string{
		"a_linux.go": "package x\n\n//kama:partial\ntype A struct{ N *N }\n",
		"b.go":       "package x\n\ntype N struct{ I int }\n\n//kama:partial\ntype B struct{ N N }\n",
	}
	out := generated(t, files)
	own := out["zz_gen.helper.a.kama_partial_linux.go"]
	helperIn(t, own)("FromTarget", "converts *N into und.Und[NPartial],")
	assert.NilError(t, generateAndBuild(t, files))
}

func TestForeignUnexportedFields(t *testing.T) {
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:partial\ntype A sub.Exported\n",
		"sub/sub.go": "package sub\n\ntype Exported struct {\n\tPublic int\n\tprivate int\n}\n",
	}, testRun{})
	assert.ErrorIs(t, err, ErrUnsupported)

	for _, s := range []string{"Fields.A (", "field private", "sub.go:5:2", "unexported"} {
		assert.ErrorContains(t, err, s)
	}

	_, err = generate(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:partial\ntype A sub.Exported\n",
		"sub/sub.go": "package sub\n\ntype Exported struct {\n\tPublic int\n\thidden\n}\n\n" +
			"type hidden struct{ N int }\n",
	}, testRun{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorContains(t, err, "field hidden")
}

// modulePath returns the import path of the inline test directory of t.
func modulePath(t *testing.T) string {
	t.Helper()

	return "github.com/ngicks/kama/kamalib/generator/partial/" + gentest.InlineDir(t)
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
			_, err := generate(t, map[string]string{"a.go": src + name + " int\n"}, testRun{})
			assert.ErrorIs(t, err, ErrConflict)
			assert.ErrorContains(t, err, "a.go:16:6")
			assert.ErrorContains(t, err, "predeclared "+name)
		})
	}
}

// TestErrorsOfEveryPackage reports the error of each package: an error in
// the files of one package does not stop the files of the next.
func TestErrorsOfEveryPackage(t *testing.T) {
	const src = "//kama:partial\ntype A struct {\n\t//kama:partial:merge append\n\tN []int\n}\n\n" +
		"type append int\n"

	_, err := generate(t, map[string]string{
		"a.go":   "package x\n\n" + src,
		"y/y.go": "package y\n\n" + src,
	}, testRun{})
	assert.ErrorIs(t, err, ErrConflict)
	assert.ErrorContains(t, err, modulePath(t)+".append shadows the predeclared append")
	assert.ErrorContains(t, err, modulePath(t)+"/y.append shadows the predeclared append")
}

func TestTypeParamNamedLikePredeclared(t *testing.T) {
	for _, name := range []string{"error", "nil", "false"} {
		t.Run(name, func(t *testing.T) {
			_, err := generate(t, map[string]string{
				"a.go": "package x\n\n//kama:partial\ntype A[" + name + " any] struct {\n" +
					"\t//kama:partial:flag name=b\n\tB bool\n\tV " + name + "\n}\n",
			}, testRun{})
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

// TestCollidingLocalsBuild names type parameters and package-level
// declarations after the locals the generated functions declare, and
// builds the output. A renamed local that the body still spells by its
// plain name fails to build.
func TestCollidingLocalsBuild(t *testing.T) {
	files := map[string]string{
		"typeparams/a.go": `package typeparams

//kama:partial
type A[p, v, r, o, x, e, k, i, f, err, base, l, lv, rv any] struct {
	X  p
	S  []v
	M  map[string]r
	P  *o
	N  Inner[x]
	PN *Inner[e]
	Emb[k]
	*EmbP[i]
}

//kama:partial
type L[p, v, r, o, x, e, k, i any] []*Inner[e]

//kama:partial
type LV[p, v, r, o, x, e, k, i any] []Inner[e]

//kama:partial
type M[k comparable, p, v, r, o, x, e, i any] map[k]*Inner[e]

//kama:partial
type MI[k comparable, p, v, r, o, x, e, i any] map[k]*int

//kama:partial
type Inner[x any] struct{ V x }

//kama:partial
type Emb[e any] struct{ E e }

//kama:partial
type EmbP[v any] struct{ F v }
`,
		"pkglevel/a.go": `package pkglevel

type (
	p    int
	r    int
	o    int
	e    int
	k    int
	i    int
	f    int
	err  int
	base int
	l    int
	lv   int
	rv   int
)

//kama:partial
type v map[string]*x

//kama:partial
type x struct {
	I  int
	S  []string
	M  map[string]int
	P  *int
	N  inner
	PN *inner
	emb
	*embP
}

//kama:partial
type s []*x

//kama:partial
type inner struct{ V int }

//kama:partial
type emb struct{ E int }

//kama:partial
type embP struct{ F int }
`,
		"fieldmask/a.go": `package fieldmask

type (
	paths  int
	path   int
	head   int
	rest   int
	nested int
	y      int
	subB   int
	wholeB int
	subEmb int
	slices int
)

//kama:partial
//kama:partial:fieldmask
type A[fmt, strings, x, err, p, v, subPB, wholePB any] struct {
	X  p
	B  Inner[x]
	PB *Inner[err]
	Emb[v]
	*EmbP[strings]
	R *A[fmt, strings, x, err, p, v, subPB, wholePB]
}

//kama:partial
//kama:partial:fieldmask
type T[paths, path, head, rest, nested, y, subN, wholeN any] struct {
	L paths
	N Inner[y]
}

//kama:partial:fieldmask
type Inner[T any] struct{ V T }

//kama:partial:fieldmask
type Emb[T any] struct{ E T }

//kama:partial:fieldmask
type EmbP[T any] struct{ F T }
`,
	}

	for _, opts := range []Options{
		{},
		{Merge: MergeAppend},
		{MergeNested: MergeNestedOverwrite},
	} {
		t.Run(string(opts.Merge)+"/"+string(opts.MergeNested), func(t *testing.T) {
			f, load := gentest.Stage(t, files)
			pkgs, err := gopackages.Load(t.Context(), f, load)
			assert.NilError(t, err)
			out, _, err := runGenerate(t, f, pkgs, testRun{Options: opts})
			assert.NilError(t, err)

			gentest.Write(t, f, out)
			assert.NilError(t, gentest.Build(f, load))
		})
	}
}

// TestBlankFieldsCopied builds partial types of structs with blank
// fields. A partial struct keeps them with their types as written, and
// the file imports the packages of those types.
func TestBlankFieldsCopied(t *testing.T) {
	files := map[string]string{
		"a.go": `package x

import (
	"structs"
	"time"
)

//kama:partial
type Host struct {
	_ structs.HostLayout
	N int32
}

//kama:partial
type OnlyBlank struct {
	_ int
	_ time.Time
}

//kama:partial
type Mixed struct {
	_ [0]func()
	X int
	_ **int
	_ Inner
	Y *Inner
}

//kama:partial
type Inner struct {
	_ string
	V string
}
`,
	}
	assert.NilError(t, generateAndBuild(t, files))

	src := generated(t, files)["a.kama_partial.go"]
	assert.Equal(t, partialDecl(t, src, "OnlyBlankPartial"), `type OnlyBlankPartial struct {
	_ int
	_ time.Time
}`)
	assert.Equal(t, partialDecl(t, src, "HostPartial"), `type HostPartial struct {
	_ structs.HostLayout
	N und.Und[int32] `+"`json:\",omitzero\"`"+`
}`)

	// A blank field of a target type is copied too, and is not wrapped.
	mixed := partialDecl(t, src, "MixedPartial")
	for _, field := range []string{"\t_ [0]func()\n", "\t_ **int\n", "\t_ Inner\n"} {
		assert.Assert(t, strings.Contains(mixed, field), mixed)
	}
}
