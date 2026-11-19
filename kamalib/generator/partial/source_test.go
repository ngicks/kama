package partial

import (
	"go/ast"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/kama/kamalib/generator/base"

	"github.com/ngicks/opiter"

	"github.com/dave/dst"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// partialDecl returns the declaration of the type name in src, a generated
// file, from its type keyword to its closing brace.
func partialDecl(t *testing.T, src, name string) string {
	t.Helper()

	m := regexp.MustCompile(`(?ms)^type ` + name + `\b.*?(?:struct\{\}|^\})$`).FindString(src)
	assert.Assert(t, m != "", "no declaration of %s in\n%s", name, src)

	return m
}

// TestSourceStructLayout generates a struct whose fields carry comments
// and directives: the partial struct keeps the blank lines between the
// fields and none of the comments.
func TestSourceStructLayout(t *testing.T) {
	files := map[string]string{
		"a.go": `package x

// A is documented.
//
//kama:partial
type A struct { // A opens.
	// X leads.
	X int // X trails.

	// Y is merged by append.
	//
	//kama:partial:merge append
	Y []int ` + "`json:\"y\"`" + `
	//kama:partial:merge append
	Z []int // Z trails.

	W []int //kama:partial:merge append
	// W is followed by a comment of its own line.
} // A closes.
`,
	}
	assert.NilError(t, generateAndBuild(t, files))

	src := generated(t, files)["a.kama_partial.go"]
	assert.Equal(t, partialDecl(t, src, "APartial"), `type APartial struct {
	X und.Und[int] `+"`json:\",omitzero\"`"+`

	Y und.Und[[]int] `+"`json:\"y,omitzero\"`"+`
	Z und.Und[[]int] `+"`json:\",omitzero\"`"+`

	W und.Und[[]int] `+"`json:\",omitzero\"`"+`
}`)
	assert.Assert(t, strings.Contains(src, "// APartial is the partial variant of A.\n//\n"), src)

	for _, s := range []string{
		"A is documented", "A opens", "X leads", "X trails", "Y is merged",
		"Z trails", "W is followed", "A closes", "kama:partial:",
	} {
		assert.Assert(t, !strings.Contains(src, s), "%s in\n%s", s, src)
	}
}

func TestSourceStructGroups(t *testing.T) {
	files := map[string]string{
		"a.go": `package x

//kama:partial
type A struct {
	// Grouped share a type and a tag.
	A, B int

	// Split differ in their tags, as only exported fields get json.
	c, D int
	E    int
}

//kama:partial
type Line struct{ X, Y int }
`,
	}
	assert.NilError(t, generateAndBuild(t, files))

	src := generated(t, files)["a.kama_partial.go"]
	assert.Equal(t, partialDecl(t, src, "APartial"), `type APartial struct {
	A, B und.Und[int] `+"`json:\",omitzero\"`"+`

	c und.Und[int]
	D und.Und[int] `+"`json:\",omitzero\"`"+`
	E und.Und[int] `+"`json:\",omitzero\"`"+`
}`)
	assert.Equal(t, partialDecl(t, src, "LinePartial"), `type LinePartial struct {
	X, Y und.Und[int] `+"`json:\",omitzero\"`"+`
}`)
}

func TestSourceStructBlanks(t *testing.T) {
	files := map[string]string{
		"a.go": `package x

import "time"

//kama:partial
type A struct {
	_, A int

	// Gone leads.
	_, _ time.Time // Gone trails.

	B int
	_ string
}
`,
	}
	assert.NilError(t, generateAndBuild(t, files))

	src := generated(t, files)["a.kama_partial.go"]
	assert.Equal(t, partialDecl(t, src, "APartial"), `type APartial struct {
	A und.Und[int] `+"`json:\",omitzero\"`"+`

	B und.Und[int] `+"`json:\",omitzero\"`"+`
}`)
	assert.Assert(t, !strings.Contains(src, `"time"`), src)
}

func TestSourceStructTags(t *testing.T) {
	files := map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct {\n" +
			"\tRaw    int `json:\"raw,omitzero\" xml:\"raw\"`\n" +
			"\tQuoted int \"json:\\\"quoted,omitzero\\\"\"\n" +
			"\tEdited int `json:\"edited\" xml:\"edited\"`\n" +
			"\tQEdit  int \"json:\\\"qedit\\\"\"\n" +
			"\tSkip   int `json:\"-\"`\n" +
			"}\n",
	}
	assert.NilError(t, generateAndBuild(t, files))

	src := generated(t, files)["a.kama_partial.go"]
	assert.Equal(t, partialDecl(t, src, "APartial"), "type APartial struct {\n"+
		"\tRaw    und.Und[int] `json:\"raw,omitzero\" xml:\"raw\"`\n"+
		"\tQuoted und.Und[int] \"json:\\\"quoted,omitzero\\\"\"\n"+
		"\tEdited und.Und[int] `json:\"edited,omitzero\" xml:\"edited\"`\n"+
		"\tQEdit  und.Und[int] `json:\"qedit,omitzero\"`\n"+
		"\tSkip   und.Und[int] `json:\"-\"`\n"+
		"}")
}

func TestSourceStructEmbedded(t *testing.T) {
	files := map[string]string{
		"a.go": `package x

import "time"

//kama:partial
type A struct {
	Value ` + "`json:\"value\"`" + `
	*Ptr

	time.Time
	Basic
}

type Value struct{ V int }

type Ptr struct{ P int }

type Basic string
`,
	}
	assert.NilError(t, generateAndBuild(t, files))

	src := generated(t, files)["a.kama_partial.go"]
	assert.Equal(t, partialDecl(t, src, "APartial"), `type APartial struct {
	ValuePartial `+"`json:\"value\"`"+`
	PtrPartial

	Time  und.Und[time.Time] `+"`json:\",omitzero\"`"+`
	Basic und.Und[Basic]     `+"`json:\",omitzero\"`"+`
}`)
}

// TestSourceStructFallback generates targets not declared by a struct
// literal next to the struct literals they derive from. Defined and Inst
// are built from their types, without the blank lines of Base and Gen.
func TestSourceStructFallback(t *testing.T) {
	files := map[string]string{
		"a.go": `package x

//kama:partial
type Defined Base

type Base struct {
	M int

	N int
}

//kama:partial
type Gen[T any] struct {
	U T

	V T
}

//kama:partial
type Inst Gen[int]

//kama:partial
type List []Base

//kama:partial
type Dict map[string]Base
`,
	}
	assert.NilError(t, generateAndBuild(t, files))

	src := generated(t, files)["a.kama_partial.go"]
	assert.Equal(t, partialDecl(t, src, "DefinedPartial"), `type DefinedPartial struct {
	M und.Und[int] `+"`json:\",omitzero\"`"+`
	N und.Und[int] `+"`json:\",omitzero\"`"+`
}`)
	assert.Equal(t, partialDecl(t, src, "BasePartial"), `type BasePartial struct {
	M und.Und[int] `+"`json:\",omitzero\"`"+`

	N und.Und[int] `+"`json:\",omitzero\"`"+`
}`)
	assert.Equal(t, partialDecl(t, src, "GenPartial"), `type GenPartial[T any] struct {
	U und.Und[T] `+"`json:\",omitzero\"`"+`

	V und.Und[T] `+"`json:\",omitzero\"`"+`
}`)
	assert.Equal(t, partialDecl(t, src, "InstPartial"), `type InstPartial struct {
	U und.Und[int] `+"`json:\",omitzero\"`"+`
	V und.Und[int] `+"`json:\",omitzero\"`"+`
}`)
	assert.Assert(t, strings.Contains(src, "type ListPartial []und.Und[BasePartial]\n"), src)
	assert.Assert(
		t,
		strings.Contains(src, "type DictPartial map[string]und.Und[BasePartial]\n"),
		src,
	)
}

// TestSourceNotMutated generates twice from one load and renders a file
// twice on one selection, as Generate does, then reads the decorated
// source: it keeps its directives, names and types.
func TestSourceNotMutated(t *testing.T) {
	files := map[string]string{
		"a.go": `package x

//kama:partial
type A struct {
	//kama:partial:merge append
	_, X []int // X trails.
	a, B int
}
`,
	}
	pkgs := load(t, files)
	opts := Options{Registry: registry(t), Now: testNow}
	first, _, err := Generate(pkgs, opts)
	assert.NilError(t, err)
	second, _, err := Generate(pkgs, opts)
	assert.NilError(t, err)
	assert.DeepEqual(t, second, first)

	opts = opts.WithDefaults()
	sel, err := selectTargets(base.SourcePackages(pkgs), opts)
	assert.NilError(t, err)

	n := opiter.WrapSeq(maps.Values(sel.targets)).
		First().
		Value()

	assert.Assert(t, n != nil)

	hdr, err := fileHeader(opts, n.Id.PackagePath, testNow())
	assert.NilError(t, err)

	f := &partialFile{nodes: []*typegraph.Node{n}}
	for range 2 {
		_, _, err := renderFileWith(
			sel, hdr, n.Detail.Pkg, f, supportPaths, newHelperSet(),
		)
		assert.NilError(t, err)
	}

	ts, err := sel.sources.TypeSpec(n.Detail.Pkg.Fset, n.Detail.File, n.Detail.Ts)
	assert.NilError(t, err)

	fields := ts.Type.(*dst.StructType).Fields.List
	assert.Equal(t, len(fields), 2)
	assert.DeepEqual(t, []string(fields[0].Decs.Start), []string{"//kama:partial:merge append"})
	assert.DeepEqual(t, []string(fields[0].Decs.End), []string{"// X trails."})
	assert.DeepEqual(t, identNames(fields[0].Names), []string{"_", "X"})
	assert.DeepEqual(t, identNames(fields[1].Names), []string{"a", "B"})
	assert.Equal(t, fields[1].Type.(*dst.Ident).Name, "int")

	assert.Assert(
		t,
		opiter.WrapSeq(slices.Values(n.Detail.File.Comments)).
			ContainsFunc(
				func(cg *ast.CommentGroup) bool { return cg.List[0].Text == "//kama:partial:merge append" },
			),
	)
}

func identNames(ids []*dst.Ident) []string {
	return opiter.WrapSeq(slices.Values(ids)).
		Map(func(id *dst.Ident) string { return id.Name }).
		CollectSlice()
}
