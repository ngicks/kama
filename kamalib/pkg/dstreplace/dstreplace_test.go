package dstreplace_test

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/dstreplace"
)

// parse parses src, a file declaring the type A, and returns the decorated
// struct literal of A with the cache holding it.
func parse(t *testing.T, src string) (*dstreplace.Cache, *dst.StructType) {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "a.go", src, parser.ParseComments)
	assert.NilError(t, err)

	c := &dstreplace.Cache{}
	ts, err := c.TypeSpec(fset, f, typeSpec(t, f, "A"))
	assert.NilError(t, err)

	return c, ts.Type.(*dst.StructType)
}

func typeSpec(t *testing.T, f *ast.File, name string) *ast.TypeSpec {
	t.Helper()

	specs := opiter.Flatten(
		opiter.WrapSeq(
			slices.Values(f.Decls),
		).
			Map(typeDeclSpecs),
	)

	found := opiter.WrapSeq(specs).
		Map(
			func(s ast.Spec) *ast.TypeSpec { return s.(*ast.TypeSpec) },
		).
		Filter(
			func(ts *ast.TypeSpec) bool { return ts.Name.Name == name },
		).
		First()

	if found.IsNone() {
		t.Fatalf("no type %s", name)
	}

	return found.Value()
}

func typeDeclSpecs(d ast.Decl) []ast.Spec {
	if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
		return gd.Specs
	}

	return nil
}

// printStruct prints st as the type B.
func printStruct(t *testing.T, st *dst.StructType) string {
	t.Helper()

	f := &dst.File{Name: dst.NewIdent("x"), Decls: []dst.Decl{&dst.GenDecl{
		Tok:   token.TYPE,
		Specs: []dst.Spec{&dst.TypeSpec{Name: dst.NewIdent("B"), Type: st}},
	}}}

	var buf bytes.Buffer
	assert.NilError(t, decorator.Fprint(&buf, f))

	return strings.TrimPrefix(buf.String(), "package x\n\n")
}

func TestStruct(t *testing.T) {
	for _, tc := range []struct {
		name    string
		src     string
		replace dstreplace.Replacer
		want    string
	}{
		{
			name: "comments erased, layout kept",
			src: `package x

// A doc.
type A struct { // opens
	// X leads.
	X int // X trails.

	//kama:partial:merge append
	Y, Z []int /* inline */ ` + "`json:\"y\"`" + `
	// before close
} // after
`,
			replace: func(f dstreplace.Field) (dstreplace.Replacement, error) {
				return dstreplace.Replacement{Type: dst.NewIdent("T"), Tag: f.Tag}, nil
			},
			want: "type B struct {\n\tX T\n\n\tY, Z T `json:\"y\"`\n}\n",
		},
		{
			name: "group split by type",
			src:  "package x\n\ntype A struct {\n\tX int\n\n\tA, B, C int `t:\"v\"`\n\n\tD int\n}\n",
			replace: func(f dstreplace.Field) (dstreplace.Replacement, error) {
				typ := "Same"
				if f.Name == "B" {
					typ = "Other"
				}

				return dstreplace.Replacement{Type: dst.NewIdent(typ), Tag: f.Tag}, nil
			},
			want: "type B struct {\n\tX Same\n\n" +
				"\tA Same  `t:\"v\"`\n\tB Other `t:\"v\"`\n\tC Same  `t:\"v\"`\n\n\tD Same\n}\n",
		},
		{
			name: "group split by tag",
			src:  "package x\n\ntype A struct {\n\ta, B int\n}\n",
			replace: func(f dstreplace.Field) (dstreplace.Replacement, error) {
				r := dstreplace.Replacement{Type: dst.NewIdent("T")}
				if f.Name == "B" {
					r.Tag = "json:\"b\""
				}

				return r, nil
			},
			want: "type B struct {\n\ta T\n\tB T `json:\"b\"`\n}\n",
		},
		{
			name: "drops",
			src: "package x\n\ntype A struct {\n\t_ int\n\n\t_, X int\n\n" +
				"\t// gone\n\t_, _ int // gone\n\n" +
				"\tY int\n\n\t_ int\n}\n",
			replace: func(f dstreplace.Field) (dstreplace.Replacement, error) {
				if f.Name == "_" {
					return dstreplace.Replacement{Drop: true}, nil
				}

				return dstreplace.Replacement{Type: dst.NewIdent("T")}, nil
			},
			want: "type B struct {\n\tX T\n\n\tY T\n}\n",
		},
		{
			name: "everything dropped",
			src:  "package x\n\ntype A struct {\n\t// c\n\t_ int\n}\n",
			replace: func(dstreplace.Field) (dstreplace.Replacement, error) {
				return dstreplace.Replacement{Drop: true}, nil
			},
			want: "type B struct{}\n",
		},
		{
			name: "tags",
			src: "package x\n\ntype A struct {\n" +
				"\tRaw    int `a:\"1\"`\n" +
				"\tQuoted int \"a:\\\"1\\\"\"\n" +
				"\tEdited int \"a:\\\"1\\\"\"\n" +
				"\tAdded  int\n" +
				"\tGone   int `a:\"1\"`\n" +
				"\tQuote  int\n" +
				"}\n",
			replace: func(f dstreplace.Field) (dstreplace.Replacement, error) {
				r := dstreplace.Replacement{Type: dst.NewIdent("T"), Tag: f.Tag}
				switch f.Name {
				case "Edited", "Added":
					r.Tag = `a:"2"`
				case "Gone":
					r.Tag = ""
				case "Quote":
					r.Tag = "a:\"`\""
				}

				return r, nil
			},
			want: "type B struct {\n" +
				"\tRaw    T `a:\"1\"`\n" +
				"\tQuoted T \"a:\\\"1\\\"\"\n" +
				"\tEdited T `a:\"2\"`\n" +
				"\tAdded  T `a:\"2\"`\n" +
				"\tGone   T\n" +
				"\tQuote  T \"a:\\\"`\\\"\"\n" +
				"}\n",
		},
		{
			name: "embedded",
			src:  "package x\n\ntype A struct {\n\tE `json:\"e\"`\n\t*P\n\tsub.S\n\tG[int]\n}\n",
			replace: func(f dstreplace.Field) (dstreplace.Replacement, error) {
				r := dstreplace.Replacement{Type: dst.NewIdent(f.Name + "X"), Tag: f.Tag}
				if f.Name == "S" || f.Name == "G" {
					r = dstreplace.Replacement{Name: f.Name, Type: dst.NewIdent("T")}
				}

				return r, nil
			},
			want: "type B struct {\n\tEX `json:\"e\"`\n\tPX\n\tS T\n\tG T\n}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, src := parse(t, tc.src)
			before := printStruct(t, dst.Clone(src).(*dst.StructType))
			got, err := dstreplace.Struct(src, tc.replace)
			assert.NilError(t, err)
			assert.Equal(t, printStruct(t, got), tc.want)
			assert.Equal(t, printStruct(t, dst.Clone(src).(*dst.StructType)), before, "src changed")
		})
	}
}

func TestStructErrors(t *testing.T) {
	_, src := parse(t, "package x\n\ntype A struct{ X int }\n")
	errBoom := errors.New("boom")
	_, err := dstreplace.Struct(src, func(dstreplace.Field) (dstreplace.Replacement, error) {
		return dstreplace.Replacement{}, errBoom
	})
	assert.ErrorIs(t, err, errBoom)
	assert.ErrorContains(t, err, "field X")

	_, err = dstreplace.Struct(src, func(dstreplace.Field) (dstreplace.Replacement, error) {
		return dstreplace.Replacement{}, nil
	})
	assert.ErrorIs(t, err, dstreplace.ErrReplacement)
}

func TestFields(t *testing.T) {
	_, src := parse(t, "package x\n\ntype A struct {\n\tE\n\t*P `t:\"p\"`\n\tsub.S\n\t"+
		"*G[int, string]\n\tX, _ int `t:\"x\"`\n}\n")
	got, err := dstreplace.Fields(src)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []dstreplace.Field{
		{Index: 0, Name: "E", Embedded: true},
		{Index: 1, Name: "P", Embedded: true, Tag: `t:"p"`},
		{Index: 2, Name: "S", Embedded: true},
		{Index: 3, Name: "G", Embedded: true},
		{Index: 4, Name: "X", Tag: `t:"x"`},
		{Index: 5, Name: "_", Tag: `t:"x"`},
	})
}

func TestCache(t *testing.T) {
	fset := token.NewFileSet()
	a, err := parser.ParseFile(
		fset,
		"a.go",
		"package x\n\ntype A struct{ X int }\n",
		parser.ParseComments,
	)
	assert.NilError(t, err)
	b, err := parser.ParseFile(
		fset,
		"b.go",
		"package x\n\ntype B struct{ Y int }\n",
		parser.ParseComments,
	)
	assert.NilError(t, err)

	var c dstreplace.Cache

	first, err := c.TypeSpec(fset, a, typeSpec(t, a, "A"))
	assert.NilError(t, err)
	second, err := c.TypeSpec(fset, a, typeSpec(t, a, "A"))
	assert.NilError(t, err)
	assert.Assert(t, first == second, "a.go decorated twice")
	assert.Equal(t, first.Name.Name, "A")

	_, err = c.TypeSpec(fset, a, typeSpec(t, b, "B"))
	assert.ErrorIs(t, err, dstreplace.ErrNotFound)
}

// TestStructSharedType gives every name one replacement type node. A dst
// node belongs to one tree, so Struct must copy it for each field.
func TestStructSharedType(t *testing.T) {
	shared := &dst.IndexExpr{X: dst.NewIdent("Box"), Index: dst.NewIdent("int")}

	for _, tc := range []struct {
		name string
		tag  func(f dstreplace.Field) string
		want string
	}{
		{
			name: "grouped",
			tag:  func(dstreplace.Field) string { return "" },
			want: "type B struct {\n\tX, Y Box[int]\n\tZ    Box[int]\n}\n",
		},
		{
			name: "split by tag",
			tag:  func(f dstreplace.Field) string { return `k:"` + f.Name + `"` },
			want: "type B struct {\n\tX Box[int] `k:\"X\"`\n\tY Box[int] `k:\"Y\"`\n" +
				"\tZ Box[int] `k:\"Z\"`\n}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, src := parse(t, "package x\n\ntype A struct {\n\tX, Y int\n\tZ    string\n}\n")
			st, err := dstreplace.Struct(
				src,
				func(f dstreplace.Field) (dstreplace.Replacement, error) {
					return dstreplace.Replacement{Type: shared, Tag: tc.tag(f)}, nil
				},
			)
			assert.NilError(t, err)
			assert.Equal(t, printStruct(t, st), tc.want)
		})
	}
}

// TestStructEmptyTagLiteral drops a source literal that spells the empty
// tag when the replacement has no tag.
func TestStructEmptyTagLiteral(t *testing.T) {
	_, src := parse(t, "package x\n\ntype A struct {\n\tX int \"\"\n\tY int ``\n}\n")
	st, err := dstreplace.Struct(src, func(dstreplace.Field) (dstreplace.Replacement, error) {
		return dstreplace.Replacement{Type: dst.NewIdent("T")}, nil
	})
	assert.NilError(t, err)
	assert.Equal(t, printStruct(t, st), "type B struct {\n\tX T\n\tY T\n}\n")
}
