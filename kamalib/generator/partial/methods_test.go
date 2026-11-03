package partial

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
)

// funcs returns the source of each function declared in src keyed by name,
// methods keyed by receiver type and name as in T.M.
func funcs(t *testing.T, src string) map[string]string {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	assert.NilError(t, err)
	out := map[string]string{}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := fd.Name.Name
		if fd.Recv != nil {
			recv := fd.Recv.List[0].Type
			if s, ok := recv.(*ast.StarExpr); ok {
				recv = s.X
			}
			for {
				switch x := recv.(type) {
				case *ast.IndexExpr:
					recv = x.X
					continue
				case *ast.IndexListExpr:
					recv = x.X
					continue
				}
				break
			}
			name = recv.(*ast.Ident).Name + "." + name
		}
		out[name] = src[fset.Position(fd.Pos()).Offset:fset.Position(fd.End()).Offset]
		_, marked := genmark.DefaultRegistry.Generated(fd.Doc)
		assert.Assert(t, marked, "%s has no marker", name)
	}
	return out
}

func TestCycleMapping(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": `package x

//kama:partial
type A struct {
	B    *B
	Self *A
	C    *C
}

type B struct {
	A *A
	V V
}

type V struct{ B *B }

type C struct{ I int }

//kama:partial
type L[T any] struct {
	Next *L[T]
	Kids Kids[T]
}

type Kids[T any] []L[T]
`,
	})
	src := files["a.kama_partial.go"]

	types := func(name string) map[string]string {
		out := map[string]string{}
		for k, v := range fieldTypes(t, src, name) {
			out[k] = v[0]
		}
		return out
	}
	assert.DeepEqual(t, types("APartial"), map[string]string{
		"B":    "und.Und[*BPartial]",
		"Self": "und.Und[*APartial]",
		// C is on no cycle.
		"C": "und.Und[CPartial]",
	})
	assert.DeepEqual(t, types("BPartial"), map[string]string{
		"A": "und.Und[*APartial]",
		// V is held by value and V.B closes its cycle.
		"V": "und.Und[VPartial]",
	})
	assert.DeepEqual(t, types("VPartial"), map[string]string{"B": "und.Und[*BPartial]"})
	assert.DeepEqual(t, types("LPartial"), map[string]string{
		"Next": "und.Und[*LPartial[T]]",
		// A slice partial holds its elements indirectly.
		"Kids": "und.Und[KidsPartial[T]]",
	})

	fs := funcs(t, src)
	assert.Assert(
		t,
		strings.Contains(fs["APartial.FromValue"], "kamaPartialFromTargetRef(&p.Self, v.Self)"),
	)
	assert.Assert(
		t,
		strings.Contains(fs["APartial.FromValue"], "kamaPartialFromTargetPointer(&p.C, v.C)"),
	)
	assert.Assert(
		t,
		strings.Contains(fs["APartial.ToValue"], "kamaPartialToTargetRef(&v.Self, p.Self)"),
	)
	assert.Assert(t, strings.Contains(fs["APartial.Merge"],
		"p.Self = kamaPartialMergeRecursiveRef(p.Self, r.Self)"))
	assert.Assert(t, strings.Contains(fs["APartial.Merge"],
		"p.C = kamaPartialMergeRecursive(p.C, r.C)"))
}

func TestImportNamesAvoidPackageAndTypeParams(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A[und any] struct{ X und }\n",
		"b.go": "package x\n\ntype opt int\n\n//kama:partial\ntype B struct{ X int }\n",
	})
	a := files["a.kama_partial.go"]
	assert.Assert(t, strings.Contains(a, `import und_2 "github.com/ngicks/option/und"`), a)
	assert.Assert(t, strings.Contains(a, "X und_2.Und[und]"), a)

	files = generated(t, map[string]string{
		"a.go": "package x\n\ntype und int\n\n//kama:partial\ntype A struct{ X und }\n",
	})
	a = files["a.kama_partial.go"]
	assert.Assert(t, strings.Contains(a, `import und_2 "github.com/ngicks/option/und"`), a)
	assert.Assert(t, strings.Contains(a, "X und_2.Und[und]"), a)
	h := files[HelpersFile]
	assert.Assert(t, strings.Contains(h, `und_2 "github.com/ngicks/option/und"`), h)
	assert.Assert(t, strings.Contains(h, "und_2.Defined(v)"), h)
}

func TestLocalsAvoidTypeParams(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A[p, v any] struct{ X p; Y v }\n\n" +
			"//kama:partial\ntype M[k comparable, e any] map[k]*N[e]\n\n" +
			"type N[T any] struct{ V T }\n",
	})
	fs := funcs(t, files["a.kama_partial.go"])
	assert.Assert(t, strings.HasPrefix(fs["APartial.FromValue"],
		"func (p_ *APartial[p, v]) FromValue(v_ A[p, v]) {"), fs["APartial.FromValue"])
	assert.Assert(t, strings.Contains(fs["MPartial.FromValue"], "for k_, e_ := range v {"),
		fs["MPartial.FromValue"])

	// Locals do not shadow the package-level types the methods spell.
	files = generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype v map[string]*x\n\ntype x struct{ I int }\n",
	})
	fs = funcs(t, files["a.kama_partial.go"])
	assert.Assert(t, strings.Contains(fs["vPartial.ToValue"], "var v_ v\n"), fs["vPartial.ToValue"])
	assert.Assert(
		t,
		strings.Contains(fs["vPartial.ToValue"], "var x_ *x\n"),
		fs["vPartial.ToValue"],
	)
}

func TestUnexportedAliasField(t *testing.T) {
	const sub = "github.com/ngicks/kama/kamalib/generator/partial/testdata/inline/" +
		"TestUnexportedAliasField/sub"
	files := generated(t, map[string]string{
		"a.go": "package x\n\nimport \"" + sub + "\"\n\n//kama:partial\n" +
			"type A struct {\n\tsub.Public\n\tX sub.Public\n\tP *sub.Public\n}\n",
		"sub/s.go": "package sub\n\ntype Public = hidden\n\ntype hidden struct{ I int }\n",
	})
	src := files["a.kama_partial.go"]
	got := map[string]string{}
	for k, v := range fieldTypes(t, src, "APartial") {
		got[k] = v[0]
	}
	assert.DeepEqual(t, got, map[string]string{
		"Public": "und.Und[sub.Public]",
		"X":      "und.Und[sub.Public]",
		"P":      "und.Und[sub.Public]",
	})
	fs := funcs(t, src)
	assert.Assert(
		t,
		strings.Contains(fs["APartial.FromValue"], "p.P = kamaPartialFromPointer(v.P)"),
	)
	assert.Assert(t, strings.Contains(files["s.kama_partial.go"], "type hiddenPartial struct"))
}

func TestEmbeddedCollection(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct {\n" +
			"\tItems\n\t*Index `json:\"idx\"`\n}\n\n" +
			"type Items []int\n\ntype Index map[string]int\n",
	})
	src := files["a.kama_partial.go"]
	assert.DeepEqual(t, fieldTypes(t, src, "APartial"), map[string][2]string{
		"Items": {"und.Und[ItemsPartial]", "`json:\",omitzero\"`"},
		"Index": {"und.Und[IndexPartial]", "`json:\"idx,omitzero\"`"},
	})
	fs := funcs(t, src)
	assert.Assert(
		t,
		strings.Contains(fs["APartial.FromValue"], "kamaPartialFromSliceTarget(&p.Items, v.Items)"),
	)
	assert.Assert(t, strings.Contains(fs["APartial.FromValue"],
		"kamaPartialFromTargetPointer(&p.Index, v.Index)"))
}

func TestRegistry(t *testing.T) {
	files := map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{}\n",
		"a.kama_partial.go": "package x\n\n//kama:generated kama-partial\n" +
			"type APartial struct{}\n",
	}
	_, err := generate(t, files, Options{})
	assert.NilError(t, err)

	// A registry without the generator does not tell its declarations.
	_, err = generate(t, files, Options{Registry: genmark.NewRegistry()})
	assert.ErrorIs(t, err, ErrConflict)
	assert.ErrorContains(t, err, "APartial is already declared")
}

func TestMethodConflicts(t *testing.T) {
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{}\n",
		"a.kama_partial.go": "package x\n\n//kama:generated kama-partial\n" +
			"type APartial struct{}\n\nfunc (APartial) Merge() {}\n",
	}, Options{})
	assert.ErrorIs(t, err, ErrConflict)
	assert.ErrorContains(t, err, "a.kama_partial.go:6:17")
	assert.ErrorContains(t, err, "method Merge of")

	_, err = generate(t, map[string]string{
		"a.go":      "package x\n\n//kama:partial\ntype A struct{}\n",
		HelpersFile: "package x\n\nfunc mine() {}\n",
	}, Options{})
	assert.ErrorIs(t, err, ErrConflict)
	assert.ErrorContains(t, err, HelpersFile+":3:1")
}

func TestRegenerateKeepsOwnDecls(t *testing.T) {
	first := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ X []int }\n",
	})
	second := generated(t, map[string]string{
		"a.go":              "package x\n\n//kama:partial\ntype A struct{ X []int }\n",
		"a.kama_partial.go": first["a.kama_partial.go"],
		HelpersFile:         first[HelpersFile],
	})
	assert.DeepEqual(t, second, first)
}

func TestMergeDefaults(t *testing.T) {
	src := map[string]string{
		"a.go": `package x

import "github.com/ngicks/option/elastic"

//kama:partial
type A struct {
	S  []int
	M  map[string]int
	N  N
	PN *N
	L  L
	I  int
	E  elastic.Elastic[int]
	N2 N
	//kama:partial:merge overwrite
	S2 []int
	//kama:partial:merge recursive
	N3 N
	//kama:partial:merge append
	L2 L
}

type N struct{ I int }

type L []int
`,
	}

	for _, c := range []struct {
		name string
		opts Options
		want []string
		l    string
	}{
		{
			name: "defaults",
			want: []string{
				"p.S = kamaPartialMergeOverwrite(p.S, r.S)",
				"p.M = kamaPartialMergeOverwrite(p.M, r.M)",
				"p.N = kamaPartialMergeRecursive(p.N, r.N)",
				"p.PN = kamaPartialMergeRecursive(p.PN, r.PN)",
				"p.L = kamaPartialMergeRecursive(p.L, r.L)",
				"p.I = kamaPartialMergeOverwrite(p.I, r.I)",
				"p.E = kamaPartialMergeOverwriteElastic(p.E, r.E)",
				"p.S2 = kamaPartialMergeOverwrite(p.S2, r.S2)",
				"p.N3 = kamaPartialMergeRecursive(p.N3, r.N3)",
				"p.L2 = kamaPartialMergeAppendSlice(p.L2, r.L2)",
			},
			l: "return kamaPartialOverwriteSlice(p, r)",
		},
		{
			name: "append and overwrite",
			opts: Options{Merge: MergeAppend, MergeNested: MergeNestedOverwrite},
			want: []string{
				"p.S = kamaPartialMergeAppendSlice(p.S, r.S)",
				"p.M = kamaPartialMergeAppendMap(p.M, r.M)",
				"p.N = kamaPartialMergeOverwrite(p.N, r.N)",
				"p.PN = kamaPartialMergeOverwrite(p.PN, r.PN)",
				"p.L = kamaPartialMergeOverwrite(p.L, r.L)",
				// Plain values have no append.
				"p.I = kamaPartialMergeOverwrite(p.I, r.I)",
				"p.E = kamaPartialMergeOverwriteElastic(p.E, r.E)",
				"p.S2 = kamaPartialMergeOverwrite(p.S2, r.S2)",
				"p.N3 = kamaPartialMergeRecursive(p.N3, r.N3)",
				"p.L2 = kamaPartialMergeAppendSlice(p.L2, r.L2)",
			},
			l: "return kamaPartialAppendSlice(p, r)",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := generatedWith(t, src, c.opts)
			fs := funcs(t, files["a.kama_partial.go"])
			for _, w := range c.want {
				assert.Assert(
					t,
					strings.Contains(fs["APartial.Merge"], w),
					"%s\n%s",
					w,
					fs["APartial.Merge"],
				)
			}
			assert.Assert(t, strings.Contains(fs["LPartial.Merge"], c.l), fs["LPartial.Merge"])
		})
	}
}

func TestHelpersFile(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ X int }\n",
		"b.go": "package x\n\n//kama:partial\ntype B struct{ X []int }\n",
	})
	h := files[HelpersFile]
	assert.Assert(t, strings.HasPrefix(h, header+"\n\npackage x\n"), h)

	var names []string
	for name := range funcs(t, h) {
		names = append(names, name)
	}
	slices.Sort(names)
	assert.DeepEqual(t, names, []string{
		"kamaPartialFromSlice",
		"kamaPartialFromValue",
		"kamaPartialMergeOverwrite",
		"kamaPartialToValue",
	})

	// Every helper the methods call is declared.
	for _, name := range []string{"a.kama_partial.go", "b.kama_partial.go"} {
		for _, n := range helperNames() {
			if strings.Contains(files[name], n+"(") {
				assert.Assert(t, slices.Contains(names, n), "%s calls %s", name, n)
			}
		}
	}

	// A helper calling another brings it along.
	files = generatedWith(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ X map[string]int }\n",
	}, Options{Merge: MergeAppend})
	h = files[HelpersFile]
	assert.Assert(t, strings.Contains(h, "func kamaPartialMergeAppendMap["), h)
	assert.Assert(t, strings.Contains(h, "func kamaPartialAppendMap["), h)
	assert.Assert(t, strings.Contains(h, `import (
	"github.com/ngicks/option/und"
	"maps"
)`), h)
}
