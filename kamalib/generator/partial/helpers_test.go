package partial

import (
	"bytes"
	"cmp"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// helperIn returns a function returning the name of the one helper of op
// declared in srcs whose doc comment holds doc, white space collapsed.
// Names hash signatures, so tests find helpers by what they state.
func helperIn(t *testing.T, srcs ...string) func(op, doc string) string {
	t.Helper()
	type decl struct{ name, doc string }
	var decls []decl
	for _, src := range srcs {
		f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ParseComments)
		assert.NilError(t, err)
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil &&
				strings.HasPrefix(fd.Name.Name, "kamaPartial") {
				decls = append(
					decls,
					decl{fd.Name.Name, strings.Join(strings.Fields(fd.Doc.Text()), " ")},
				)
			}
		}
	}
	return func(op, doc string) string {
		t.Helper()
		var found []string
		for _, d := range decls {
			if strings.HasPrefix(d.name, "kamaPartial"+op+"_") && strings.Contains(d.doc, doc) {
				found = append(found, d.name)
			}
		}
		assert.Equal(t, len(found), 1, "helpers of %s stating %q: %v", op, doc, found)
		return found[0]
	}
}

var (
	pkgA     = types.NewPackage("a.example/v", "v")
	pkgB     = types.NewPackage("b.example/v", "v")
	intT     = types.Typ[types.Int]
	stringT  = types.Typ[types.String]
	emptyIfc = types.NewInterfaceType(nil, nil).Complete()
)

func newNamed(pkg *types.Package, name string, underlying types.Type) *types.Named {
	return types.NewNamed(types.NewTypeName(token.NoPos, pkg, name, nil), underlying, nil)
}

func field(pkg *types.Package, name string, t types.Type, embedded bool) *types.Var {
	return types.NewField(token.NoPos, pkg, name, t, embedded)
}

func params(pkg *types.Package, names []string, ts ...types.Type) *types.Tuple {
	vars := make([]*types.Var, len(ts))
	for i, t := range ts {
		var name string
		if i < len(names) {
			name = names[i]
		}
		vars[i] = types.NewParam(token.NoPos, pkg, name, t)
	}
	return types.NewTuple(vars...)
}

func method(pkg *types.Package, name string) *types.Func {
	return types.NewFunc(
		token.NoPos,
		pkg,
		name,
		types.NewSignatureType(nil, nil, nil, nil, nil, false),
	)
}

// typeParamOf returns the type parameter of a new generic type named name.
func typeParamOf(name, tparam string, constraint types.Type) *types.TypeParam {
	named := newNamed(pkgA, name, types.NewStruct(nil, nil))
	tp := types.NewTypeParam(types.NewTypeName(token.NoPos, pkgA, tparam, nil), constraint)
	named.SetTypeParams([]*types.TypeParam{tp})
	return tp
}

func encode(t types.Type) []byte { return newSynthetic().encodeList([]types.Type{t}) }

func TestEncodeDeterministic(t *testing.T) {
	m := types.NewMap(stringT, types.NewSlice(types.NewPointer(newNamed(pkgA, "T", intT))))
	assert.DeepEqual(t, encode(m), encode(m))
	// Types built apart but identical encode alike.
	assert.DeepEqual(t, encode(types.NewSlice(intT)), encode(types.NewSlice(intT)))
}

// TestEncodeMatchesIdentical checks that types encode alike exactly when
// types.Identical reports them identical.
func TestEncodeMatchesIdentical(t *testing.T) {
	alias := types.NewAlias(
		types.NewTypeName(token.NoPos, pkgA, "Names", nil),
		types.NewSlice(stringT),
	)
	sig := func(names []string, in, out []types.Type, variadic bool) types.Type {
		return types.NewSignatureType(
			nil,
			nil,
			nil,
			params(pkgA, names, in...),
			params(pkgA, nil, out...),
			variadic,
		)
	}
	ifc := func(ms ...*types.Func) types.Type { return types.NewInterfaceType(ms, nil).Complete() }
	tA := newNamed(pkgA, "T", intT)

	for _, c := range []struct {
		name      string
		a, b      types.Type
		identical bool
	}{
		{"alias and aliased", types.NewPointer(alias), types.NewPointer(types.NewSlice(stringT)), true},
		{"byte and uint8", types.Universe.Lookup("byte").Type(), types.Typ[types.Uint8], true},
		{"rune and int32", types.Universe.Lookup("rune").Type(), types.Typ[types.Int32], true},
		{
			"unexported fields of two packages",
			types.NewStruct([]*types.Var{field(pkgA, "x", intT, false)}, nil),
			types.NewStruct([]*types.Var{field(pkgB, "x", intT, false)}, nil),
			false,
		},
		{
			"exported fields of two packages",
			types.NewStruct([]*types.Var{field(pkgA, "X", intT, false)}, nil),
			types.NewStruct([]*types.Var{field(pkgB, "X", intT, false)}, nil),
			true,
		},
		{
			"tags",
			types.NewStruct([]*types.Var{field(pkgA, "X", intT, false)}, []string{`json:"x"`}),
			types.NewStruct([]*types.Var{field(pkgA, "X", intT, false)}, []string{`json:"y"`}),
			false,
		},
		{
			"embedded field",
			types.NewStruct([]*types.Var{field(pkgA, "T", tA, true)}, nil),
			types.NewStruct([]*types.Var{field(pkgA, "T", tA, false)}, nil),
			false,
		},
		{
			"parameter names",
			sig([]string{"a"}, []types.Type{intT}, nil, false),
			sig([]string{"b"}, []types.Type{intT}, nil, false),
			true,
		},
		{
			"parameters and results",
			sig(nil, []types.Type{intT, intT}, []types.Type{intT}, false),
			sig(nil, []types.Type{intT}, []types.Type{intT, intT}, false),
			false,
		},
		{
			"variadic",
			sig(nil, []types.Type{types.NewSlice(intT)}, nil, true),
			sig(nil, []types.Type{types.NewSlice(intT)}, nil, false),
			false,
		},
		{
			"channel direction",
			types.NewChan(types.SendOnly, intT), types.NewChan(types.SendRecv, intT),
			false,
		},
		{"array length", types.NewArray(intT, 2), types.NewArray(intT, 3), false},
		{
			"method order",
			ifc(method(pkgA, "A"), method(pkgA, "B")), ifc(method(pkgA, "B"), method(pkgA, "A")),
			true,
		},
		{"unexported methods of two packages", ifc(method(pkgA, "m")), ifc(method(pkgB, "m")), false},
		{"named types of two paths", newNamed(pkgA, "T", intT), newNamed(pkgB, "T", intT), false},
		{"map key and element", types.NewMap(intT, stringT), types.NewMap(stringT, intT), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, types.Identical(c.a, c.b), c.identical)
			assert.Equal(t, bytes.Equal(encode(c.a), encode(c.b)), c.identical)
		})
	}
}

// TestEncodeTypeParams ignores the names of type parameters, which the
// helper generic over them declares itself.
func TestEncodeTypeParams(t *testing.T) {
	tpT, tpU := typeParamOf("G", "T", emptyIfc), typeParamOf("H", "U", emptyIfc)
	assert.DeepEqual(t, encode(types.NewSlice(tpT)), encode(types.NewSlice(tpU)))
	other := typeParamOf("K", "T", types.NewInterfaceType(nil, []types.Type{
		types.NewUnion([]*types.Term{types.NewTerm(true, intT)}),
	}).Complete())
	assert.Assert(t, !bytes.Equal(encode(tpT), encode(other)))
}

// names adds hs to set in order, names them and returns their names.
func names(t *testing.T, set *helperSet, hs ...*helper) ([]string, error) {
	t.Helper()
	var added []*helper
	for _, h := range hs {
		h, err := set.add(h)
		assert.NilError(t, err)
		added = append(added, h)
	}
	if err := set.resolve(); err != nil {
		return nil, err
	}
	out := make([]string, len(added))
	for i, h := range added {
		out[i] = set.name(h)
	}
	return out, nil
}

func mustNames(t *testing.T, hs ...*helper) []string {
	t.Helper()
	out, err := names(t, newHelperSet(), hs...)
	assert.NilError(t, err)
	return out
}

func TestHelperNameFormat(t *testing.T) {
	name := helperName("FromTarget", signatureHash(encode(intT), encode(stringT)))
	assert.Assert(
		t,
		regexp.MustCompile(`^kamaPartialFromTarget_[0-9a-f]{10}_[0-9a-f]{10}$`).
			MatchString(name),
		name,
	)
}

func TestHelperNameSeparatesInAndOut(t *testing.T) {
	s := newSynthetic()
	a, b, c := intT, stringT, types.Typ[types.Bool]
	assert.Assert(t,
		signatureHash(s.encodeList([]types.Type{a, b}), s.encodeList([]types.Type{c})) !=
			signatureHash(s.encodeList([]types.Type{a}), s.encodeList([]types.Type{b, c})),
	)
}

func TestHelperNameSeparatesOps(t *testing.T) {
	m := newNamed(pkgA, "M", types.NewSlice(intT))
	got := mustNames(t,
		&helper{op: opMergeAppend, typ: m, named: m, partial: true},
		&helper{op: opMergeRecursive, typ: m, named: m, partial: true},
	)
	// One signature: only the op prefix differs.
	assert.Equal(t,
		strings.TrimPrefix(got[0], "kamaPartialMergeAppend"),
		strings.TrimPrefix(got[1], "kamaPartialMergeRecursive"),
	)
}

func TestHelperNameSamePackageName(t *testing.T) {
	got := mustNames(t,
		&helper{op: opMergeAppend, typ: types.NewSlice(newNamed(pkgA, "T", intT))},
		&helper{op: opMergeAppend, typ: types.NewSlice(newNamed(pkgB, "T", intT))},
	)
	assert.Assert(t, got[0] != got[1])
	// No collision: neither has a suffix.
	for _, name := range got {
		assert.Assert(t, regexp.MustCompile(`_[0-9a-f]{10}_[0-9a-f]{10}$`).MatchString(name), name)
	}
}

// TestHelperNameDocExample pins the name of the example of the package
// documentation.
func TestHelperNameDocExample(t *testing.T) {
	m := types.NewPackage("example.com/m", "m")
	address := newNamed(m, "Address", types.NewStruct(nil, nil))
	got := mustNames(t, &helper{
		op:    opFromTarget,
		typ:   types.NewPointer(address),
		route: []typegraph.EdgeRouteNode{{Kind: typegraph.EdgeKindPointer}},
		named: address,
	})
	assert.Equal(t, got[0], "kamaPartialFromTarget_2b27c0c476_7bfb4f6244")
}

// colliding returns a helper set whose hashes all collide.
func colliding() *helperSet {
	set := newHelperSet()
	set.hash = func(_, _ []byte) string { return "0000000000_0000000000" }
	return set
}

// sliceHelpers returns append helpers of slices of n distinct named types.
func sliceHelpers(n int) []*helper {
	out := make([]*helper, n)
	for i := range out {
		elem := newNamed(pkgA, "T"+strconv.Itoa(i), intT)
		out[i] = &helper{op: opMergeAppend, typ: types.NewSlice(elem)}
	}
	return out
}

// TestHelperCollision suffixes the names of colliding helpers in the order
// of their encodings, whatever order they are added in.
func TestHelperCollision(t *testing.T) {
	const base = "kamaPartialMergeAppend_0000000000_0000000000"
	hs := sliceHelpers(3)
	// Adding sets the encodings the expected order follows.
	_, err := names(t, newHelperSet(), hs...)
	assert.NilError(t, err)
	slices.SortFunc(hs, func(a, b *helper) int {
		return cmp.Or(bytes.Compare(a.inEnc, b.inEnc), bytes.Compare(a.outEnc, b.outEnc))
	})

	got, err := names(t, colliding(), hs...)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []string{base + "_1", base + "_2", base + "_3"})
	assert.Assert(t, !bytes.Equal(hs[0].inEnc, hs[1].inEnc))

	// Another run adding them in another order names them alike.
	again, err := names(t, colliding(), hs[2], hs[0], hs[1])
	assert.NilError(t, err)
	assert.DeepEqual(t, again, []string{base + "_3", base + "_1", base + "_2"})

	// An identical signature joins its helper rather than the group.
	set := colliding()
	got, err = names(
		t,
		set,
		append(slices.Clone(hs), &helper{op: opMergeAppend, typ: hs[1].typ})...)
	assert.NilError(t, err)
	assert.Equal(t, got[3], base+"_2")

	// A helper of another op has a base name of its own.
	m := newNamed(pkgA, "M", types.NewStruct(nil, nil))
	got, err = names(t, colliding(), hs[0], &helper{op: opAllUndefined, typ: m, named: m})
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []string{base, "kamaPartialAllUndefined_0000000000_0000000000"})
}

func TestHelperCollisionSuffixes(t *testing.T) {
	got, err := names(t, colliding(), sliceHelpers(maxCollisions)...)
	assert.NilError(t, err)
	const base = "kamaPartialMergeAppend_0000000000_0000000000"
	assert.Assert(t, slices.Contains(got, base+"_ff"))
	assert.Assert(t, slices.Contains(got, base+"_a"))
	assert.Equal(t, len(got), len(slices.Compact(slices.Sorted(slices.Values(got)))))

	_, err = names(t, colliding(), sliceHelpers(maxCollisions+1)...)
	assert.ErrorIs(t, err, ErrConflict)
}

// TestHelperSetInvariant reports a signature identical to one recorded
// under another name, which the encoding never gives.
func TestHelperSetInvariant(t *testing.T) {
	ints := &helper{op: opMergeAppend, typ: types.NewSlice(intT)}
	set := newHelperSet()
	prev, err := set.add(ints)
	assert.NilError(t, err)
	set.groups = map[string][]*helper{"kamaPartialMergeAppend_other": {prev}}
	_, err = set.add(&helper{op: opMergeAppend, typ: types.NewSlice(intT)})
	assert.ErrorIs(t, err, ErrConflict)

	// A helper first seen after naming is an error too.
	set = newHelperSet()
	_, err = names(t, set, ints)
	assert.NilError(t, err)
	_, err = set.add(&helper{op: opMergeAppend, typ: types.NewSlice(stringT)})
	assert.Error(t, err, err.Error())
}

// TestAllUndefined tests embedded partial values field by field, through
// nested embedded partials and past blank fields, without reflection.
func TestAllUndefined(t *testing.T) {
	files := map[string]string{
		"a.go": "package x\n\n//kama:partial\ntype A struct {\n\t*B\n}\n\n" +
			"type B struct {\n\tC\n\tY string\n}\n\ntype C struct {\n\t_ int\n\tX int\n}\n",
	}
	src := generatedWith(t, files, Options{MergeNested: MergeNestedOverwrite})["a.kama_partial.go"]
	assert.Assert(t, !strings.Contains(src, "reflect"), src)

	h := helperIn(t, src)
	allB := h("AllUndefined", "every field of p, a BPartial, is undefined")
	allC := h("AllUndefined", "every field of p, a CPartial, is undefined")
	fs := funcs(t, src)
	assert.Assert(t, strings.Contains(fs["APartial.Merge"], "if !"+allB+"(r.BPartial) {"),
		fs["APartial.Merge"])
	assert.Assert(
		t,
		strings.Contains(fs[allB], "return "+allC+"(p.CPartial) &&\n\t\tp.Y.IsUndefined()"),
		fs[allB],
	)
	assert.Assert(t, strings.Contains(fs[allC], "return p.X.IsUndefined()\n"), fs[allC])
	toB := h("ToEmbedded", "converts BPartial into *B,")
	assert.Assert(t, strings.Contains(fs[toB], "if "+allB+"(p) {"), fs[toB])

	assert.NilError(t, generateAndBuild(t, files))
}

// TestAllUndefinedForeignUnexported rejects an embedded partial of another
// package whose unexported fields cannot be tested from the embedding one.
func TestAllUndefinedForeignUnexported(t *testing.T) {
	_, err := generate(t, map[string]string{
		"a.go": "package x\n\nimport \"" + modulePath(t) + "/sub\"\n\n" +
			"//kama:partial\ntype A struct{ *sub.S }\n",
		"sub/sub.go": "package sub\n\ntype S struct {\n\tX int\n\ty int\n}\n",
	}, Options{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorContains(t, err, "unexported field y")
}
