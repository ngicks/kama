package partial

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ngicks/opiter"

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
		return maps.Collect(
			opiter.WrapSeq2(maps.All(fieldTypes(t, src, name))).
				Map(withoutTag).
				Iter2(),
		)
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

	h := helperIn(t, files[HelpersFile])
	fromRefA := h("FromTargetRef", "converts *A into und.Und[*APartial],")
	fromC := h("FromTarget", "converts *C into und.Und[CPartial],")
	toRefA := h("ToTargetRef", "converts und.Und[*APartial] into *A,")
	toC := h("ToTarget", "converts und.Und[CPartial] into *C,")
	mergeRefA := h("MergeRecursive", "both und.Und[*APartial]:")
	mergeC := h("MergeRecursive", "both und.Und[CPartial]:")
	// Helpers spelling a type parameter are generic over it.
	fromRefL := h("FromTargetRef", "converts *L[T] into und.Und[*LPartial[T]],")
	mergeRefL := h("MergeRecursive", "both und.Und[*LPartial[T]]:")

	fs := allFuncs(t, src, files[HelpersFile])
	for name, want := range map[string][]string{
		"APartial.FromValue": {
			"p.Self = " + fromRefA + "(v.Self)",
			"p.C = " + fromC + "(v.C)",
		},
		"APartial.ToValue": {"v.Self = " + toRefA + "(p.Self)"},
		"APartial.Merge": {
			"p.Self = " + mergeRefA + "(p.Self, r.Self)",
			"p.C = " + mergeC + "(p.C, r.C)",
		},
		"LPartial.FromValue": {"p.Next = " + fromRefL + "[T](v.Next)"},
		"LPartial.Merge":     {"p.Next = " + mergeRefL + "[T](p.Next, r.Next)"},
		fromRefL:             {"func " + fromRefL + "[T any](v *L[T]) und.Und[*LPartial[T]] {"},
	} {
		for _, w := range want {
			assert.Assert(t, strings.Contains(fs[name], w), "%s\n%s", w, fs[name])
		}
	}
	for _, h := range []string{fromRefA, fromC, toRefA, toC, mergeRefA, mergeC, mergeRefL} {
		assert.Assert(t, fs[h] != "", "%s is missing", h)
	}
	assert.Assert(t, !strings.Contains(src, "func("), "a function literal remains")
	for name := range fs {
		assert.Assert(t, !strings.Contains(name, "_L"), "%s spells a type parameter", name)
	}
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
	assert.Assert(t, strings.Contains(a, "p.X = und_2.Defined(v.X)"), a)
}

func TestLocalsAvoidTypeParams(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A[p, v any] struct{ X p; Y v }\n\n" +
			"//kama:partial\ntype M[k comparable, e any] map[k]*N[e]\n\n" +
			"type N[T any] struct{ V T }\n",
	})
	fs := funcs(t, files["a.kama_partial.go"])
	assert.Assert(t, strings.HasPrefix(fs["APartial.FromValue"],
		"func (p2 *APartial[p, v]) FromValue(v2 A[p, v]) {"), fs["APartial.FromValue"])
	assert.Assert(t, strings.Contains(fs["MPartial.FromValue"], "for k2, e2 := range v {"),
		fs["MPartial.FromValue"])

	// Locals do not shadow the package-level types the methods spell.
	files = generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype v map[string]*x\n\ntype x struct{ I int }\n",
	})
	fs = allFuncs(t, files["a.kama_partial.go"], files[HelpersFile])
	assert.Assert(
		t,
		strings.Contains(fs["vPartial.ToValue"], "v2 := make(v, len(p))\n"),
		fs["vPartial.ToValue"],
	)
	toX := helperIn(
		t,
		files[HelpersFile],
	)(
		"ToTarget",
		"converts und.Und[xPartial] into *x,",
	)
	assert.Assert(t, strings.Contains(fs[toX], "x2 := p.Value().ToValue()"), fs[toX])

	// A map element converted by a conditional statement goes through a
	// local.
	files = generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype v map[string]*int\n\ntype x int\n",
	})
	fs = funcs(t, files["a.kama_partial.go"])
	assert.Assert(
		t,
		strings.Contains(fs["vPartial.ToValue"], "var x2 *int\n"),
		fs["vPartial.ToValue"],
	)
}

// TestDocNamesRenamedParams checks that the doc comments of methods and
// helpers spell the receiver and parameters as their signatures do once
// type parameters take the names p, v, l and r, and that helper docs stay
// wrapped at commentWidth.
func TestDocNamesRenamedParams(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\n//kama:partial:fieldmask\n" +
			"type A[p, v any] struct{ Z *N[p]; W *N[v] }\n\n" +
			"//kama:partial\ntype S[p any] []p\n\n" +
			"//kama:partial:fieldmask\ntype N[T any] struct{ V T }\n",
	})
	src := files["a.kama_partial.go"]
	for _, want := range []string{
		"// FromValue sets every field of p2 to the partial form of the field of v2.\n",
		"// ToValue returns the value p2 describes.",
		"// Merge returns p2 with the fields of r merged onto it.",
		"// ApplyTo returns v2 with the change p2 describes applied. It is\n" +
			"// FromValue of v2 merged with p2, converted back with ToValue.\n",
		"// FieldMask returns the sorted paths of the fields p2 sets.",
		"// FromValueWithMask sets p2 to the partial form of the fields of v2 that\n",
		"// FromValue sets p2 to the partial forms of the elements of v. A nil v\n" +
			"// gives a nil p2.\n",
		"// Merge returns r, or p2 when r is nil. Merge writes to neither p2 nor r.\n",
	} {
		assert.Assert(t, strings.Contains(src, want), "%q in\n%s", want, src)
	}

	// Helpers are shared by the shape of their types, so the type parameters
	// l and r go to a package of their own.
	merged := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype B[l, r any] struct{ L *N[l]; R *N[r] }\n\n" +
			"//kama:partial\ntype N[T any] struct{ V T }\n",
	})
	helper := helperIn(t, files[HelpersFile], merged[HelpersFile])
	for _, c := range []struct{ op, doc string }{
		{"FromTarget", "into und.Und[NPartial[v]], defined, or null for a nil v2."},
		{"FromTarget", "into und.Und[NPartial[p]], defined, or null for a nil v."},
		{"ToTarget", "into *N[p], newly allocated, or nil when p2 is undefined or null."},
		{"ToTarget", "into *N[v], newly allocated, or nil when p is undefined or null."},
		{"MergeRecursive", "merges r onto l2, both und.Und[NPartial[l]]: " +
			"it returns the merge of their values when both are defined, l2 when r is"},
		{"MergeRecursive", "merges r2 onto l, both und.Und[NPartial[r]]: " +
			"it returns the merge of their values when both are defined, l when r2 is"},
	} {
		helper(c.op, c.doc)
	}

	for _, out := range []string{
		src, files[HelpersFile], merged["a.kama_partial.go"], merged[HelpersFile],
	} {
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", out, parser.ParseComments)
		assert.NilError(t, err)
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil {
				continue
			}
			for _, c := range fd.Doc.List {
				assert.Assert(t, len(c.Text) <= commentWidth || !strings.Contains(c.Text[3:], " "),
					"%s: %q", fd.Name.Name, c.Text)
			}
		}
	}
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
	got := maps.Collect(
		opiter.WrapSeq2(maps.All(fieldTypes(t, src, "APartial"))).
			Map(withoutTag).
			Iter2(),
	)
	assert.DeepEqual(t, got, map[string]string{
		"Public": "und.Und[sub.Public]",
		"X":      "und.Und[sub.Public]",
		"P":      "und.Und[sub.Public]",
	})
	fs := funcs(t, src)
	assert.Assert(
		t,
		strings.Contains(fs["APartial.FromValue"], "p.P = und.FromPointer(v.P).CollapseToNull()"),
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
	h := helperIn(t, files[HelpersFile])
	assert.Assert(t, strings.Contains(fs["APartial.FromValue"],
		"p.Items = "+h("FromTarget", "converts Items into und.Und[ItemsPartial],")+"(v.Items)"))
	assert.Assert(t, strings.Contains(fs["APartial.FromValue"],
		"p.Index = "+h("FromTarget", "converts *Index into und.Und[IndexPartial],")+"(v.Index)"))
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
		FlagSetFile: "package x\n\nfunc mine() {}\n",
	}, Options{})
	assert.ErrorIs(t, err, ErrConflict)
	assert.ErrorContains(t, err, FlagSetFile+":3:1")
}

func TestRegenerateKeepsOwnDecls(t *testing.T) {
	first := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ X []int }\n",
	})
	second := generated(t, map[string]string{
		"a.go":              "package x\n\n//kama:partial\ntype A struct{ X []int }\n",
		"a.kama_partial.go": first["a.kama_partial.go"],
		FlagSetFile:         first[FlagSetFile],
	})
	assert.DeepEqual(t, second, first)
}

func TestMergeDefaults(t *testing.T) {
	overwrite := func(f string) string {
		return "if !r." + f + ".IsUndefined() {\n\t\tp." + f + " = r." + f + "\n\t}"
	}
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

	merge := func(h func(op, doc string) string, op, held, field string) string {
		return "p." + field + " = " + h(op, "both und.Und["+held+"]:") +
			"(p." + field + ", r." + field + ")"
	}
	for _, c := range []struct {
		name string
		opts Options
		want func(h func(op, doc string) string) []string
		l    string
	}{
		{
			name: "defaults",
			want: func(h func(op, doc string) string) []string {
				return []string{
					overwrite("S"),
					overwrite("M"),
					merge(h, "MergeRecursive", "NPartial", "N"),
					merge(h, "MergeRecursive", "NPartial", "PN"),
					merge(h, "MergeRecursive", "LPartial", "L"),
					overwrite("I"),
					overwrite("E"),
					overwrite("S2"),
					merge(h, "MergeRecursive", "NPartial", "N3"),
					merge(h, "MergeAppend", "LPartial", "L2"),
				}
			},
			l: "if r == nil {\n\t\treturn p\n\t}\n\treturn r\n",
		},
		{
			name: "append and overwrite",
			opts: Options{Merge: MergeAppend, MergeNested: MergeNestedOverwrite},
			want: func(h func(op, doc string) string) []string {
				return []string{
					merge(h, "MergeAppend", "[]int", "S"),
					merge(h, "MergeAppend", "map[string]int", "M"),
					overwrite("N"),
					overwrite("PN"),
					overwrite("L"),
					// Plain values have no append.
					overwrite("I"),
					overwrite("E"),
					overwrite("S2"),
					merge(h, "MergeRecursive", "NPartial", "N3"),
					merge(h, "MergeAppend", "LPartial", "L2"),
				}
			},
			l: "o := make(LPartial, 0, len(p)+len(r))\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := generatedWith(t, src, c.opts)
			fs := funcs(t, files["a.kama_partial.go"])
			for _, w := range c.want(helperIn(t, files[HelpersFile])) {
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

// TestHelperPlacement checks that each helper is declared once in the
// package, in its helpers file, and only when called.
func TestHelperPlacement(t *testing.T) {
	files := generated(t, map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct{ X int }\n",
		"b.go": "package x\n\n//kama:partial\ntype B struct{ N *N }\n\ntype N struct{ I int }\n",
		"c.go": "package x\n\n//kama:partial\ntype C struct {\n\tN *N\n" +
			"\t//kama:partial:merge append\n\tM map[string]int\n}\n",
	})
	declared := map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		for fn := range funcs(t, files[name]) {
			if strings.HasPrefix(fn, "kamaPartial") {
				declared[fn] = append(declared[fn], name)
			}
		}
	}
	h := helperIn(t, slices.Collect(maps.Values(files))...)
	assert.DeepEqual(t, declared, map[string][]string{
		h("FromTarget", "converts *N into und.Und[NPartial],"): {HelpersFile},
		h("ToTarget", "converts und.Und[NPartial] into *N,"):   {HelpersFile},
		h("MergeRecursive", "both und.Und[NPartial]:"):         {HelpersFile},
		h("MergeAppend", "both und.Und[map[string]int]:"):      {HelpersFile},
	})
	// Only the helpers file imports what the helpers spell.
	assert.Assert(t, strings.Contains(files[HelpersFile], `"maps"`), files[HelpersFile])
	for _, name := range []string{"a.kama_partial.go", "b.kama_partial.go", "c.kama_partial.go"} {
		assert.Assert(t, !strings.Contains(files[name], `"maps"`), files[name])
	}
}

// TestHelperNamesSamePackageName gives types of two packages of one name
// helpers of their own.
func TestHelperNamesSamePackageName(t *testing.T) {
	files := map[string]string{
		"a.go": "package x\n\nimport (\n\t\"" + modulePath(t) + "/one/v\"\n\tw \"" + modulePath(t) +
			"/two/v\"\n)\n\n//kama:partial\ntype A struct {\n\t//kama:partial:merge append\n" +
			"\tX []v.T\n\t//kama:partial:merge append\n\tY []w.T\n}\n",
		"one/v/v.go": "package v\n\ntype T int\n",
		"two/v/v.go": "package v\n\ntype T string\n",
	}
	out := generated(t, files)
	src := out["a.kama_partial.go"]
	fs := allFuncs(t, src, out[HelpersFile])
	call := regexp.MustCompile(`p\.([XY]) = (kamaPartialMergeAppend_[0-9a-f_]+)\(`)
	names := map[string]string{}
	for _, m := range call.FindAllStringSubmatch(fs["APartial.Merge"], -1) {
		names[m[1]] = m[2]
	}
	assert.Equal(t, len(names), 2, fs["APartial.Merge"])
	assert.Assert(t, names["X"] != names["Y"], src)
	for _, name := range names {
		assert.Assert(t, fs[name] != "", "%s is not declared", name)
	}
	assert.NilError(t, generateAndBuild(t, files))
}

// TestHelperDeclared reports a declaration named like a helper.
func TestHelperDeclared(t *testing.T) {
	const src = "package x\n\n//kama:partial\ntype A struct{ X M }\n\ntype M struct{}\n"
	name := helperIn(t, generated(t, map[string]string{"x.go": src})[HelpersFile])(
		"FromTarget", "converts M into und.Und[MPartial],")
	_, err := generate(t, map[string]string{
		"x.go": "package x\n\n//kama:partial\ntype A struct{ X M }\n\n" +
			"type M struct{}\n\nfunc " + name + "() {}\n",
	}, Options{})
	assert.ErrorIs(t, err, ErrConflict)
	assert.ErrorContains(t, err, "x.go:8:6")
	assert.ErrorContains(t, err, name)
	assert.ErrorContains(t, err, "helper")
}
