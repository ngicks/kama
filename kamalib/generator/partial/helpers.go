package partial

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/utils/base"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/typegraph"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

const (
	fmtPath     = "fmt"
	mapsPath    = "maps"
	slicesPath  = "slices"
	stringsPath = "strings"
)

// supportPaths are the packages the generated code spells by path rather
// than through a type it prints. A package that does not import one of
// them, directly or not, is unknown to the import Parser, so they are
// required explicitly.
var supportPaths = []string{fmtPath, mapsPath, slicesPath, stringsPath, typequery.UndPath}

// helperOp is what a helper does. With the type of the helper it decides
// the signature.
type helperOp int

const (
	// opFromTarget: M or *M -> und.Und[MPartial].
	opFromTarget helperOp = iota
	// opFromTargetRef: *M -> und.Und[*MPartial].
	opFromTargetRef
	// opToTarget: und.Und[MPartial] -> *M.
	opToTarget
	// opToTargetRef: und.Und[*MPartial] -> *M.
	opToTargetRef
	// opFromEmbedded: *M -> MPartial.
	opFromEmbedded
	// opToEmbedded: MPartial -> *M.
	opToEmbedded
	// opMergeAppend: (und.Und[T], und.Und[T]) -> und.Und[T], T a slice or
	// map, or the partial variant of one.
	opMergeAppend
	// opMergeRecursive: (und.Und[P], und.Und[P]) -> und.Und[P], P the
	// partial variant MPartial or *MPartial.
	opMergeRecursive
	// opAllUndefined: MPartial -> bool, M a struct.
	opAllUndefined
)

var helperOpNames = map[helperOp]string{
	opFromTarget:     "FromTarget",
	opFromTargetRef:  "FromTargetRef",
	opToTarget:       "ToTarget",
	opToTargetRef:    "ToTargetRef",
	opFromEmbedded:   "FromEmbedded",
	opToEmbedded:     "ToEmbedded",
	opMergeAppend:    "MergeAppend",
	opMergeRecursive: "MergeRecursive",
	opAllUndefined:   "AllUndefined",
}

// helper is a conversion, merge or test the generated code calls for one
// type. Every caller in a package needing the same one calls the same
// function, named by helperName.
//
// A helper is generic only over the type parameters of the receiver its
// types mention. A generic function is instantiated once per GC shape, so
// every pointer type would share one instantiation dispatching through a
// dictionary; a helper of concrete types gets code of its own.
//
// A helper is a request the emitting code makes, not a fact of the source:
// op tells what the caller wants done with typ.
type helper struct {
	op helperOp
	// typ is the source type a conversion converts from or to, the type the
	// und.Und values of a merge hold, or the struct target whose partial
	// variant a test reads.
	typ types.Type
	// route are the pointer steps from typ to named.
	route []typegraph.EdgeRouteNode
	// named is the target whose partial variant the helper spells, or nil.
	named *types.Named
	// partial reports whether the und.Und values of a merge hold the partial
	// variant of typ rather than typ itself.
	partial bool
	// spelled are the paths of supportPaths the function spells, recorded
	// when the function is first rendered.
	spelled map[string]bool
}

// typeParams returns the type parameters h is generic over: those its types
// mention and those their constraints mention, in the order of the
// receiver.
func (h *helper) typeParams() []*types.TypeParam {
	seen := map[*types.TypeParam]bool{}

	var visit func(t types.Type)

	visit = func(t types.Type) {
		for _, tp := range typeParamsOf(t) {
			if !seen[tp] {
				seen[tp] = true
				visit(tp.Constraint())
			}
		}
	}
	visit(h.typ)

	return slices.SortedFunc(maps.Keys(seen), func(a, b *types.TypeParam) int {
		return cmp.Compare(a.Index(), b.Index())
	})
}

// held reports what the und.Und of h holds: the partial variant of named,
// a pointer to it, or typ itself.
func (h *helper) held() (pointer, plain bool) {
	switch h.op {
	case opFromTargetRef, opToTargetRef:
		return true, false
	case opMergeRecursive:
		return len(h.route) > 0, false
	case opMergeAppend:
		return false, !h.partial
	}

	return false, false
}

// signatureOf returns the parameter and result lists of a helper of op from
// its source type src, the partial variant part of its target, the und.Und
// it holds and the bool type.
func signatureOf[T any](op helperOp, src, part, und, boolean T) (in, out []T) {
	switch op {
	case opFromTarget, opFromTargetRef:
		return []T{src}, []T{und}
	case opToTarget, opToTargetRef:
		return []T{und}, []T{src}
	case opFromEmbedded:
		return []T{src}, []T{part}
	case opToEmbedded:
		return []T{part}, []T{src}
	case opMergeAppend, opMergeRecursive:
		return []T{und, und}, []T{und}
	case opAllUndefined:
		return []T{part}, []T{boolean}
	}

	return nil, nil
}

// signature returns the parameter and result types of h, with und.Und and
// partial variants made by syn.
func (h *helper) signature(syn *synthetic) (in, out []types.Type) {
	var part types.Type
	if h.named != nil {
		part = syn.partial(h.named)
	}

	held := part
	switch pointer, plain := h.held(); {
	case plain:
		held = h.typ
	case pointer:
		held = types.NewPointer(part)
	}

	return signatureOf(h.op, h.typ, part, syn.und(held), types.Type(types.Typ[types.Bool]))
}

// signatureHash returns the part of a helper name standing for parameter
// and result lists encoding to in and out: the first 5 bytes of the SHA-256
// of in, _ and those of out, in lowercase hexadecimal.
func signatureHash(in, out []byte) string {
	i, o := sha256.Sum256(in), sha256.Sum256(out)
	return hex.EncodeToString(i[:5]) + "_" + hex.EncodeToString(o[:5])
}

// helperName names a helper of op whose signature hashes to hash, before
// any collision suffix.
func helperName(op, hash string) string { return "kamaPartial" + op + "_" + hash }

// synthetic makes the types a helper signature holds that the loaded
// packages need not declare: und.Und, which a package may not import, and
// the partial variants of targets, which do not exist before generation.
//
// Each is a named type of its package path and name with an empty struct
// underlying type and type parameters without constraints, instantiated
// with the type arguments it is spelled with. One origin is made per type,
// so instances with identical type arguments are identical. The encoder
// writes und.Und as the named type it stands for, and a partial variant as
// a kind of its own naming its target, so the encoding of a signature
// does not depend on whether the types exist.
type synthetic struct {
	undOrigin *types.Named
	// partials are the partial variant origins by the object of their
	// target origin, and targets the reverse.
	partials map[*types.TypeName]*types.Named
	targets  map[*types.TypeName]*types.TypeName
}

func newSynthetic() *synthetic {
	return &synthetic{
		undOrigin: syntheticOrigin(types.NewPackage(typequery.UndPath, "und"), "Und", 1),
		partials:  map[*types.TypeName]*types.Named{},
		targets:   map[*types.TypeName]*types.TypeName{},
	}
}

func syntheticOrigin(pkg *types.Package, name string, tparams int) *types.Named {
	named := types.NewNamed(
		types.NewTypeName(token.NoPos, pkg, name, nil), types.NewStruct(nil, nil), nil,
	)

	if tparams > 0 {
		list := make([]*types.TypeParam, tparams)
		for i := range list {
			obj := types.NewTypeName(token.NoPos, pkg, "T"+strconv.Itoa(i), nil)
			list[i] = types.NewTypeParam(obj, types.NewInterfaceType(nil, nil).Complete())
		}

		named.SetTypeParams(list)
	}

	return named
}

// instantiate instantiates origin with args without validating them: the
// synthetic type parameters have no constraints to satisfy.
func instantiate(origin *types.Named, args []types.Type) types.Type {
	if len(args) == 0 {
		return origin
	}

	t, err := types.Instantiate(nil, origin, args, false)
	if err != nil {
		panic(fmt.Errorf("partial: instantiate %s: %w", origin, err))
	}

	return t
}

// und returns und.Und[t].
func (s *synthetic) und(t types.Type) types.Type {
	return instantiate(s.undOrigin, []types.Type{t})
}

// partial returns the partial variant of the target named, with its type
// arguments.
func (s *synthetic) partial(named *types.Named) types.Type {
	obj := named.Origin().Obj()

	origin, ok := s.partials[obj]
	if !ok {
		origin = syntheticOrigin(
			obj.Pkg(), partialName(obj.Name()), named.Origin().TypeParams().Len(),
		)
		s.partials[obj] = origin
		s.targets[origin.Obj()] = obj
	}

	return instantiate(origin, slices.Collect(named.TypeArgs().Types()))
}

// Kinds of the nodes of the canonical encoding.
const (
	encBasic     = 'b'
	encPointer   = 'p'
	encSlice     = 's'
	encArray     = 'a'
	encMap       = 'm'
	encChan      = 'c'
	encNamed     = 'n'
	encPartial   = 'P'
	encTypeParam = 't'
	encTypeRef   = 'r'
	encStruct    = 'S'
	encSignature = 'F'
	encInterface = 'I'
	encUnion     = 'U'
	encList      = 'L'
)

// encoder writes the canonical encoding of types: equal encodings for
// types identical under types.Identical, different ones otherwise. Every
// node starts with its kind, every string and list with its length, so no
// encoding is a prefix of another. Aliases are resolved at every level.
//
// Two type parameters encode alike when they have one index and alike
// constraints, so a helper generic over the type parameters of two
// receivers is one helper even though the parameters are not identical.
type encoder struct {
	b   []byte
	syn *synthetic
	// visiting are the type parameters whose constraints are being
	// written; a constraint naming its own parameter refers to it by index.
	visiting map[*types.TypeParam]bool
}

// encodeList returns the encoding of the type list ts.
func (s *synthetic) encodeList(ts []types.Type) []byte {
	e := &encoder{syn: s, visiting: map[*types.TypeParam]bool{}}
	e.list(ts)

	return e.b
}

func (e *encoder) kind(k byte)   { e.b = append(e.b, k) }
func (e *encoder) uint(n uint64) { e.b = binary.AppendUvarint(e.b, n) }
func (e *encoder) int(n int)     { e.uint(uint64(n)) }
func (e *encoder) str(s string)  { e.int(len(s)); e.b = append(e.b, s...) }
func (e *encoder) bool(v bool) {
	var b byte
	if v {
		b = 1
	}

	e.b = append(e.b, b)
}

// target returns the target obj is the synthetic partial variant of.
func (e *encoder) target(obj *types.TypeName) (*types.TypeName, bool) {
	if e.syn == nil {
		return nil, false
	}

	target, ok := e.syn.targets[obj]

	return target, ok
}

func (e *encoder) list(ts []types.Type) {
	e.kind(encList)
	e.int(len(ts))

	for _, t := range ts {
		e.typ(t)
	}
}

func (e *encoder) tuple(t *types.Tuple) {
	e.kind(encList)
	e.int(t.Len())

	for v := range t.Variables() {
		e.typ(v.Type())
	}
}

// pkgPath returns the path of pkg, or empty for the universe.
func pkgPath(pkg *types.Package) string {
	if pkg == nil {
		return ""
	}

	return pkg.Path()
}

func (e *encoder) typ(t types.Type) {
	switch t := types.Unalias(t).(type) {
	case *types.Basic:
		// byte and rune share the kinds of uint8 and int32.
		e.kind(encBasic)
		e.int(int(t.Kind()))
	case *types.Pointer:
		e.kind(encPointer)
		e.typ(t.Elem())
	case *types.Slice:
		e.kind(encSlice)
		e.typ(t.Elem())
	case *types.Array:
		e.kind(encArray)
		e.uint(uint64(t.Len()))
		e.typ(t.Elem())
	case *types.Map:
		e.kind(encMap)
		e.typ(t.Key())
		e.typ(t.Elem())
	case *types.Chan:
		e.kind(encChan)
		e.int(int(t.Dir()))
		e.typ(t.Elem())
	case *types.Named:
		obj := t.Origin().Obj()
		if target, ok := e.target(obj); ok {
			e.kind(encPartial)

			obj = target
		} else {
			e.kind(encNamed)
		}

		e.str(pkgPath(obj.Pkg()))
		e.str(obj.Name())
		e.list(slices.Collect(t.TypeArgs().Types()))
	case *types.TypeParam:
		if e.visiting[t] {
			e.kind(encTypeRef)
			e.int(t.Index())

			return
		}

		e.kind(encTypeParam)
		e.int(t.Index())
		e.visiting[t] = true
		e.typ(t.Constraint())
		delete(e.visiting, t)
	case *types.Struct:
		e.kind(encStruct)
		e.int(t.NumFields())

		for i, f := range slices.Collect(t.Fields()) {
			e.str(f.Name())

			if f.Exported() {
				e.str("")
			} else {
				e.str(pkgPath(f.Pkg()))
			}

			e.bool(f.Embedded())
			e.typ(f.Type())
			e.str(t.Tag(i))
		}
	case *types.Signature:
		// Parameter names and the receiver are not part of the identity.
		e.kind(encSignature)
		e.tuple(t.Params())
		e.tuple(t.Results())
		e.bool(t.Variadic())
	case *types.Interface:
		e.kind(encInterface)

		methods := slices.Collect(t.Methods())
		slices.SortFunc(methods, func(a, b *types.Func) int {
			return cmp.Or(cmp.Compare(a.Name(), b.Name()), cmp.Compare(methodPkg(a), methodPkg(b)))
		})
		e.int(len(methods))

		for _, m := range methods {
			e.str(m.Name())
			e.str(methodPkg(m))
			e.typ(m.Type())
		}

		// Embedded interfaces contribute their methods above; the other
		// embedded elements restrict the type set.
		elems := opiter.WrapSeq(t.EmbeddedTypes()).
			Filter(notInterface).
			CollectSlice()

		e.int(len(elems))

		for _, et := range elems {
			e.typ(et)
		}

		e.bool(t.IsComparable())
	case *types.Union:
		e.kind(encUnion)
		e.int(t.Len())

		for term := range t.Terms() {
			e.bool(term.Tilde())
			e.typ(term.Type())
		}
	default:
		panic(fmt.Errorf("partial: cannot encode %T", t))
	}
}

// methodPkg returns the path qualifying the name of m: its package for an
// unexported name, empty otherwise.
func methodPkg(m *types.Func) string {
	if m.Exported() {
		return ""
	}

	return pkgPath(m.Pkg())
}

func notInterface(t types.Type) bool {
	_, ok := types.Unalias(t).Underlying().(*types.Interface)
	return !ok
}

func isMapType(t types.Type) bool {
	_, ok := t.Underlying().(*types.Map)
	return ok
}

// typeParamsOf returns the type parameters t mentions.
func typeParamsOf(t types.Type) []*types.TypeParam {
	var (
		out   []*types.TypeParam
		visit func(t types.Type)
	)

	visit = func(t types.Type) {
		switch t := types.Unalias(t).(type) {
		case *types.TypeParam:
			out = append(out, t)
		case *types.Pointer:
			visit(t.Elem())
		case *types.Slice:
			visit(t.Elem())
		case *types.Array:
			visit(t.Elem())
		case *types.Chan:
			visit(t.Elem())
		case *types.Map:
			visit(t.Key())
			visit(t.Elem())
		case *types.Named:
			for arg := range t.TypeArgs().Types() {
				visit(arg)
			}
		case *types.Struct:
			for f := range t.Fields() {
				visit(f.Type())
			}
		case *types.Tuple:
			for v := range t.Variables() {
				visit(v.Type())
			}
		case *types.Signature:
			visit(t.Params())
			visit(t.Results())
		case *types.Interface:
			for m := range t.ExplicitMethods() {
				visit(m.Type())
			}

			for e := range t.EmbeddedTypes() {
				visit(e)
			}
		case *types.Union:
			for term := range t.Terms() {
				visit(term.Type())
			}
		}
	}
	visit(t)

	return out
}

// maxCollisions is the most helpers one base name can be shared by: their
// suffixes are one to two hexadecimal digits.
const maxCollisions = 0xff

// helperSet holds the helpers the partial files of one package call.
//
// Helpers are added while the files are rendered a first time, then
// resolve names them; the files are rendered again with the names.
//
// No source holds the set: it tells which helpers the generated code of
// the package calls, and a name depends on every helper of the package.
type helperSet struct {
	syn *synthetic
	// hash returns the part of a name standing for a signature encoding to
	// in and out. Tests replace it to force collisions.
	hash func(in, out []byte) string
	// groups are the helpers by the name their op and hash give.
	groups map[string][]*helper
	// names are the names resolve gives the helpers, and byName the reverse.
	names  map[*helper]string
	byName map[string]*helper
}

func newHelperSet() *helperSet {
	return &helperSet{
		syn:    newSynthetic(),
		hash:   signatureHash,
		groups: map[string][]*helper{},
	}
}

// add records h and returns the helper recorded for its signature, h itself
// unless an earlier helper has an identical one. Signatures of the type
// parameters of two receivers are never identical, so a generic helper
// whose signature encodes alike stands for them too.
//
// Helpers of different signatures may hash to one name; resolve tells them
// apart. A signature identical to one of another name breaks the encoding
// and is an error.
func (s *helperSet) add(h *helper) (*helper, error) {
	in, out := s.encodings(h)
	base := helperName(helperOpNames[h.op], s.hash(in, out))
	servesSignature := func(prev *helper) bool {
		if s.identicalSignature(prev, h) {
			return true
		}

		prevIn, prevOut := s.encodings(prev)

		return len(h.typeParams()) > 0 && bytes.Equal(prevIn, in) && bytes.Equal(prevOut, out)
	}

	served := opiter.WrapSeq(slices.Values(s.groups[base])).
		Filter(servesSignature).
		First()

	if served.IsSome() {
		return served.Value(), nil
	}

	for other, group := range s.groups {
		for _, prev := range group {
			if other != base && s.identicalSignature(prev, h) {
				return nil, fmt.Errorf(
					"%w: helpers %s and %s serve one signature %s",
					ErrConflict, other, base, s.describe(h),
				)
			}
		}
	}

	if s.names != nil {
		return nil, fmt.Errorf("partial: helper %s added after naming", s.describe(h))
	}

	s.groups[base] = append(s.groups[base], h)

	return h, nil
}

// resolve names the helpers. A helper alone under its base name takes it;
// the helpers sharing one take it with the suffixes _1, _2 and so on in
// lowercase hexadecimal, in the order of their encodings, parameters then
// results.
func (s *helperSet) resolve() error {
	s.names, s.byName = map[*helper]string{}, map[string]*helper{}
	for _, base := range slices.Sorted(maps.Keys(s.groups)) {
		group := s.groups[base]
		if len(group) == 1 {
			s.names[group[0]], s.byName[base] = base, group[0]
			continue
		}

		if len(group) > maxCollisions {
			return fmt.Errorf(
				"%w: %d helpers share the name %s; at most %d can be told apart",
				ErrConflict, len(group), base, maxCollisions,
			)
		}

		sorted := slices.SortedFunc(slices.Values(group), func(a, b *helper) int {
			aIn, aOut := s.encodings(a)
			bIn, bOut := s.encodings(b)

			return cmp.Or(bytes.Compare(aIn, bIn), bytes.Compare(aOut, bOut))
		})
		for i, h := range sorted {
			name := base + "_" + strconv.FormatInt(int64(i+1), 16)
			s.names[h], s.byName[name] = name, h
		}
	}

	return nil
}

// name returns the name of h, or before resolve the base name it would get
// without collisions.
func (s *helperSet) name(h *helper) string {
	if name, ok := s.names[h]; ok {
		return name
	}

	in, out := s.encodings(h)

	return helperName(helperOpNames[h.op], s.hash(in, out))
}

// encodings returns the canonical encodings of the parameter types and of
// the result types of h.
func (s *helperSet) encodings(h *helper) (in, out []byte) {
	i, o := h.signature(s.syn)
	return s.syn.encodeList(i), s.syn.encodeList(o)
}

func (s *helperSet) identicalSignature(a, b *helper) bool {
	if a.op != b.op {
		return false
	}

	aIn, aOut := a.signature(s.syn)
	bIn, bOut := b.signature(s.syn)

	return identicalList(aIn, bIn) && identicalList(aOut, bOut)
}

func identicalList(a, b []types.Type) bool {
	return slices.EqualFunc(a, b, types.Identical)
}

func (s *helperSet) describe(h *helper) string {
	in, out := h.signature(s.syn)

	return fmt.Sprintf("%s %v -> %v", helperOpNames[h.op], in, out)
}

// partialFile is what rendering a partial file found out: the helpers the
// file calls, the names of those helpers and the paths of supportPaths the
// file spells. No source holds these: they tell what the generator emitted.
// src names the source file the partial file is made from, which everything
// else about the file is read from.
type partialFile struct {
	src     string
	calls   map[*helper]bool
	used    map[string]bool
	support map[string]bool
}

// path returns the path of the partial file.
func (f *partialFile) path() string { return naming.SourceFile(f.src) }

// caller returns f as a caller of the helpers of its package.
func (f *partialFile) caller(sel *selection) base.Caller {
	return base.Caller{Src: f.src, File: sel.targetsIn(f.src)[0].Detail.File, Uses: f.used}
}
