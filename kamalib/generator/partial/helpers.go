package partial

import (
	"bufio"
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"go/token"
	"go/types"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

const (
	fmtPath  = "fmt"
	mapsPath = "maps"
)

// supportPaths are the packages the generated code spells by path rather
// than through a type it prints. A package that does not import one of
// them, directly or not, is unknown to the import Parser, so they are
// required explicitly.
var supportPaths = []string{fmtPath, mapsPath, undPath}

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
	// in and out are the parameter and result types, and inEnc and outEnc
	// their canonical encodings, set when the helper is recorded.
	in, out       []types.Type
	inEnc, outEnc []byte
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
		undOrigin: syntheticOrigin(types.NewPackage(undPath, "und"), "Und", 1),
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
		var elems []types.Type
		for et := range t.EmbeddedTypes() {
			if _, ok := types.Unalias(et).Underlying().(*types.Interface); !ok {
				elems = append(elems, et)
			}
		}
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

func isMapType(t types.Type) bool {
	_, ok := t.Underlying().(*types.Map)
	return ok
}

// typeParamsOf returns the type parameters t mentions.
func typeParamsOf(t types.Type) []*types.TypeParam {
	var out []*types.TypeParam
	var visit func(t types.Type)
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
	h.in, h.out = h.signature(s.syn)
	h.inEnc, h.outEnc = s.syn.encodeList(h.in), s.syn.encodeList(h.out)
	base := helperName(helperOpNames[h.op], s.hash(h.inEnc, h.outEnc))
	for _, prev := range s.groups[base] {
		if identicalSignature(prev, h) || len(h.typeParams()) > 0 &&
			bytes.Equal(prev.inEnc, h.inEnc) && bytes.Equal(prev.outEnc, h.outEnc) {
			return prev, nil
		}
	}
	for other, group := range s.groups {
		for _, prev := range group {
			if other != base && identicalSignature(prev, h) {
				return nil, fmt.Errorf(
					"%w: helpers %s and %s serve one signature %s",
					ErrConflict, other, base, h.describe(),
				)
			}
		}
	}
	if s.names != nil {
		return nil, fmt.Errorf("partial: helper %s added after naming", h.describe())
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
			return cmp.Or(bytes.Compare(a.inEnc, b.inEnc), bytes.Compare(a.outEnc, b.outEnc))
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
	return helperName(helperOpNames[h.op], s.hash(h.inEnc, h.outEnc))
}

func identicalSignature(a, b *helper) bool {
	return a.op == b.op && identicalList(a.in, b.in) && identicalList(a.out, b.out)
}

func identicalList(a, b []types.Type) bool {
	return slices.EqualFunc(a, b, types.Identical)
}

func (h *helper) describe() string {
	return fmt.Sprintf("%s %v -> %v", helperOpNames[h.op], h.in, h.out)
}

// step returns the code converting the value the expression in denotes.
type step func(in string) string

// pipe returns leaf wrapped by wrap once for each node of route, the first
// node outermost. Each wrapper receives the step converting the value one
// node further along the route.
func pipe(
	route []typegraph.EdgeRouteNode,
	leaf step,
	wrap func(n typegraph.EdgeRouteNode, next step) step,
) step {
	s := leaf
	for _, n := range slices.Backward(route) {
		s = wrap(n, s)
	}
	return s
}

// call returns the call of h with args, recording h in the helpers of the
// package and as called by the file.
//
// The first call from a file renders the function of h once: the helpers
// its body calls are called by the file too, and the packages of
// supportPaths it spells are recorded on h, while the imports of the file
// stay those it spells itself.
func (p *mapper) call(h *helper, args ...string) (string, error) {
	h, err := p.helpers.add(h)
	if err != nil {
		return "", err
	}
	name := p.helpers.name(h)
	if !p.calls[h] {
		p.calls[h] = true
		// The file declaring the helper imports what it spells, which the
		// calling file may not, so the rendering goes to a separate map.
		m, err := p.parser.ParseDst(nil)
		if err != nil {
			return "", err
		}
		fileMap, support := p.m, p.support
		p.m, p.support = m, map[string]bool{}
		_, err = p.helperFunc(h)
		h.spelled = p.support
		p.m, p.support = fileMap, support
		if err != nil {
			return "", err
		}
	}
	p.refs[name] = true

	var targs string
	if tps := h.typeParams(); len(tps) > 0 {
		names := make([]string, 0, len(tps))
		for _, tp := range tps {
			names = append(names, tp.Obj().Name())
		}
		// Type arguments are given rather than inferred: a parameter only a
		// constraint mentions cannot be inferred from the arguments.
		targs = "[" + strings.Join(names, ", ") + "]"
	}
	return name + targs + "(" + strings.Join(args, ", ") + ")", nil
}

// helperFunc is a helper as source text.
type helperFunc struct {
	tparams, params, results, body string
	// doc is the doc text after the name of the helper.
	doc string
}

// helperFunc spells the function of h.
//
// The route of a conversion is piped from the leaf conversion of the
// target outwards. Every route node is a pointer: a nil pointer gives null
// on the way to the partial form, and the way back allocates.
func (p *mapper) helperFunc(h *helper) (helperFunc, error) {
	tps := h.typeParams()
	names := make([]string, 0, len(tps))
	var tparams []string
	for _, tp := range tps {
		names = append(names, tp.Obj().Name())
		c, err := p.constraintString(tp)
		if err != nil {
			return helperFunc{}, err
		}
		tparams = append(tparams, tp.Obj().Name()+" "+c)
	}
	// p.m is the Map in effect now, which call swaps while it renders.
	scope := p.funcContext(names).Scope()
	l := func(role string) string {
		name, _ := scope.LookupOrDeclare(role)
		return name
	}
	v, x, pp, lv, rv, ll, r, o := l("v"), l("x"), l("p"), l("lv"), l("rv"), l("l"), l("r"), l("o")

	f := helperFunc{}
	if len(tparams) > 0 {
		f.tparams = "[" + strings.Join(tparams, ", ") + "]"
	}
	src, err := p.typeString(h.typ)
	if err != nil {
		return helperFunc{}, err
	}
	var part string
	if h.named != nil {
		if part, err = p.partialString(h.named); err != nil {
			return helperFunc{}, err
		}
	}
	// held is the type the und.Und of the helper holds.
	held := part
	switch pointer, plain := h.held(); {
	case plain:
		held = src
	case pointer:
		held = "*" + part
	}
	// The doc comment spells the types as the code does. Spelling und
	// records its import, so only the helpers whose code spells it do.
	var undSpelled string
	switch h.op {
	case opFromEmbedded, opToEmbedded, opAllUndefined:
	default:
		u, err := p.ident(undPath)
		if err != nil {
			return helperFunc{}, err
		}
		undSpelled = u + ".Und[" + held + "]"
	}
	in, out := signatureOf(h.op, src, part, undSpelled, "bool")
	f.doc = "converts " + in[0] + " into " + out[0]
	// und is spelled only by the helpers holding an und.Und.
	und := func() (string, error) { return p.ident(undPath) }
	undOf := func() (string, error) {
		u, err := und()
		return u + ".Und[" + held + "]", err
	}

	switch h.op {
	case opFromTarget, opFromTargetRef:
		u, err := und()
		if err != nil {
			return helperFunc{}, err
		}
		null := u + ".Null[" + held + "]()"
		leaf := func(in string) string {
			return "var " + x + " " + part + "\n" + x + ".FromValue(" + in + ")\n" +
				"return " + u + ".Defined(" + x + ")"
		}
		if h.op == opFromTargetRef {
			leaf = func(in string) string {
				return x + " := new(" + part + ")\n" + x + ".FromValue(" + in + ")\n" +
					"return " + u + ".Defined(" + x + ")"
			}
		}
		// A nil slice or map converts to null like a nil pointer does.
		nilable := len(h.route) > 0 || !isStruct(h.named)
		if len(h.route) == 0 && nilable {
			inner := leaf
			leaf = func(in string) string {
				return "if " + in + " == nil {\nreturn " + null + "\n}\n" + inner(in)
			}
		}
		wrap := func(_ typegraph.EdgeRouteNode, next step) step {
			return func(in string) string {
				return "if " + in + " == nil {\nreturn " + null + "\n}\n" + next("*"+in)
			}
		}
		f.params, f.results = v+" "+src, u+".Und["+held+"]"
		f.body = pipe(h.route, leaf, wrap)(v)
		switch {
		case h.op == opFromTargetRef:
			f.doc += ", a newly allocated defined value, or null for a nil v."
		case nilable:
			f.doc += ", defined, or null for a nil v."
		default:
			f.doc += ", defined."
		}
		return f, nil

	case opToTarget, opToTargetRef:
		cond := func(in string) string { return "!" + in + ".IsDefined()" }
		f.doc += ", newly allocated, or nil when p is undefined or null."
		if h.op == opToTargetRef {
			cond = func(in string) string {
				return "!" + in + ".IsDefined() || " + in + ".Value() == nil"
			}
			f.doc = "converts " + in[0] + " into " + out[0] +
				", newly allocated, or nil when p is undefined, null or nil."
		}
		leaf := func(in string) string { return in + ".ToValue()" }
		wrap := func(_ typegraph.EdgeRouteNode, next step) step {
			return func(in string) string {
				return "if " + cond(in) + " {\nreturn nil\n}\n" +
					x + " := " + next(in+".Value()") + "\nreturn &" + x
			}
		}
		param, err := undOf()
		if err != nil {
			return helperFunc{}, err
		}
		f.params, f.results, f.body = pp+" "+param, src, pipe(h.route, leaf, wrap)(pp)
		return f, nil

	case opFromEmbedded:
		leaf := func(in string) string { return x + ".FromValue(" + in + ")" }
		wrap := func(_ typegraph.EdgeRouteNode, next step) step {
			return func(in string) string {
				return "if " + in + " != nil {\n" + next("*"+in) + "\n}"
			}
		}
		f.params, f.results = v+" "+src, part
		f.body = "var " + x + " " + part + "\n" + pipe(h.route, leaf, wrap)(v) + "\nreturn " + x
		f.doc += ", or the zero partial value for a nil v."
		return f, nil

	case opToEmbedded:
		leaf := func(in string) string { return in + ".ToValue()" }
		// An embedded partial has no undefined state of its own.
		var werr error
		wrap := func(_ typegraph.EdgeRouteNode, next step) step {
			return func(in string) string {
				test, err := p.call(&helper{op: opAllUndefined, typ: h.named, named: h.named}, in)
				werr = cmp.Or(werr, err)
				return "if " + test + " {\nreturn nil\n}\n" + x + " := " + next(
					in,
				) + "\nreturn &" + x
			}
		}
		f.params, f.results, f.body = pp+" "+part, src, pipe(h.route, leaf, wrap)(pp)
		f.doc += ", newly allocated, or nil when every field of p is undefined."
		return f, werr

	case opAllUndefined:
		var tests []string
		st := h.named.Underlying().(*types.Struct)
		for fv := range st.Fields() {
			if fv.Name() == "_" {
				continue
			}
			named, ok := p.sel.partialOf(deref(fv.Type()), p.pkg.PkgPath)
			if ok && fv.Embedded() && isStruct(named) {
				test, err := p.call(
					&helper{op: opAllUndefined, typ: named, named: named},
					pp+"."+partialName(named.Obj().Name()),
				)
				if err != nil {
					return helperFunc{}, err
				}
				tests = append(tests, test)
				continue
			}
			// An unexported field of a partial type of another package cannot
			// be read here.
			if !fv.Exported() && fv.Pkg() != nil && fv.Pkg().Path() != p.pkg.PkgPath {
				return helperFunc{}, fmt.Errorf(
					"%w: embedded %s of package %s has the unexported field %s, "+
						"which package %s cannot test for undefined",
					ErrUnsupported, part, fv.Pkg().Path(), fv.Name(), p.pkg.PkgPath,
				)
			}
			tests = append(tests, pp+"."+fv.Name()+".IsUndefined()")
		}
		if len(tests) == 0 {
			tests = []string{"true"}
		}
		f.params, f.results = pp+" "+part, "bool"
		f.body = "return " + strings.Join(tests, " &&\n")
		f.doc = "reports whether every field of p, a " + in[0] + ", is undefined, the fields " +
			"of embedded partial values included. Blank fields are skipped."
		return f, nil

	case opMergeAppend:
		u, err := und()
		if err != nil {
			return helperFunc{}, err
		}
		var b strings.Builder
		printf, flush := bufPrintf(&b)
		printf("if %s.IsUndefined() {\nreturn %s\n}\n", r, ll)
		printf("if !%s.IsDefined() || !%s.IsDefined() {\nreturn %s\n}\n", ll, r, r)
		printf("%s, %s := %s.Value(), %s.Value()\n", lv, rv, ll, r)
		printf("if %s == nil && %s == nil {\nreturn %s\n}\n", lv, rv, r)
		elems := "a new slice of the elements of l then r"
		if isMapType(h.mergeType()) {
			elems = "a new map of the entries of l with the entries of r set"
			mp, err := p.ident(mapsPath)
			if err != nil {
				return helperFunc{}, err
			}
			printf("%s := make(%s, len(%s)+len(%s))\n", o, held, lv, rv)
			printf("%s.Copy(%s, %s)\n%s.Copy(%s, %s)\n", mp, o, lv, mp, o, rv)
			printf("return %s.Defined(%s)", u, o)
		} else {
			printf("%s := make(%s, 0, len(%s)+len(%s))\n", o, held, lv, rv)
			printf("%s = append(%s, %s...)\n", o, o, lv)
			printf("return %s.Defined(append(%s, %s...))", u, o, rv)
		}
		if err := flush(); err != nil {
			return helperFunc{}, err
		}
		undT := u + ".Und[" + held + "]"
		f.params, f.results, f.body = ll+", "+r+" "+undT, undT, b.String()
		f.doc = "merges r onto l, both " + in[0] + ": it returns l when r is undefined, " +
			elems + " when both are defined, and r otherwise. Two nil values give r." +
			"\n\nA defined nil or empty side is not returned in place of the new value, " +
			"so the result never shares storage with l or r."
		return f, nil

	case opMergeRecursive:
		u, err := und()
		if err != nil {
			return helperFunc{}, err
		}
		var b strings.Builder
		printf, flush := bufPrintf(&b)
		printf("if %s.IsUndefined() {\nreturn %s\n}\n", r, ll)
		if len(h.route) > 0 {
			printf("if !%s.IsDefined() || !%s.IsDefined() || "+
				"%s.Value() == nil || %s.Value() == nil {\nreturn %s\n}\n", ll, r, ll, r, r)
			printf("%s := %s.Value().Merge(*%s.Value())\n", x, ll, r)
			printf("return %s.Defined(&%s)", u, x)
			f.doc = "merges r onto l, both " + in[0] + ": it returns a new merge of their " +
				"values when both are defined and non-nil, l when r is undefined, and r otherwise."
		} else {
			printf("if !%s.IsDefined() || !%s.IsDefined() {\nreturn %s\n}\n", ll, r, r)
			printf("return %s.Defined(%s.Value().Merge(%s.Value()))", u, ll, r)
			f.doc = "merges r onto l, both " + in[0] + ": it returns the merge of their " +
				"values when both are defined, l when r is undefined, and r otherwise."
		}
		if err := flush(); err != nil {
			return helperFunc{}, err
		}
		undT := u + ".Und[" + held + "]"
		f.params, f.results, f.body = ll+", "+r+" "+undT, undT, b.String()
		return f, nil
	}
	return helperFunc{}, fmt.Errorf("partial: unknown helper op %d", h.op)
}

// mergeType is the type whose values a merge helper combines, past its
// pointers.
func (h *helper) mergeType() types.Type {
	if h.partial {
		return h.named
	}
	return h.typ
}

// commentWidth is the width doc comments of helpers are wrapped at.
const commentWidth = 76

// wrapComment returns text as // comment lines of at most commentWidth
// bytes, except for words longer than that. Paragraphs of text are
// separated by an empty line.
func wrapComment(text string) string {
	var lines []string
	for i, para := range strings.Split(text, "\n\n") {
		if i > 0 {
			lines = append(lines, "//")
		}
		line := "//"
		for word := range strings.FieldsSeq(para) {
			if line != "//" && len(line)+1+len(word) > commentWidth {
				lines = append(lines, line)
				line = "//"
			}
			line += " " + word
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n") + "\n"
}

// bufPrintf returns a printf writing to w through a buffer and the flush
// writing out what the buffer holds. Generated code goes in as arguments
// only, so that a % it holds is not read as a verb.
func bufPrintf(w io.Writer) (printf func(format string, args ...any), flush func() error) {
	bw := bufio.NewWriter(w)
	return func(format string, args ...any) { fmt.Fprintf(bw, format, args...) }, bw.Flush
}

// helperDecl spells the declaration of the helper name.
func (p *mapper) helperDecl(name string, h *helper) (string, error) {
	f, err := p.helperFunc(h)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("\n%s//\n%s\nfunc %s%s(%s) %s {\n%s\n}\n",
		wrapComment(name+" "+f.doc), genmark.Marker(GeneratorName),
		name, f.tparams, f.params, f.results, f.body,
	), nil
}

// partialFile is a partial file of a package and the helpers it calls.
type partialFile struct {
	path  string
	nodes []*typegraph.Node
	// constrained reports whether the source file has a build constraint,
	// as a //go:build line or a file name suffix.
	constrained bool
	// calls are the helpers the file calls, used their names, and support
	// the paths of supportPaths it spells.
	calls   map[*helper]bool
	used    map[string]bool
	support map[string]bool
	// hosted are the names of the helpers the file declares.
	hosted []string
}

// hostHelpers sets the helpers each of files, sorted by path, declares.
// Every helper is declared once in the package: in the first file calling
// it without a build constraint, or failing that the first file calling it.
//
// A file without a build constraint is in every build variant, and whether
// it calls a helper does not depend on the variant loaded, so every
// variant finds the helpers such a file calls in the same place. A helper
// called by constrained files only lives in one of them, which stays right
// as long as the files calling it are loaded together.
//
// A file declares its helpers sorted by name, that is by op and then by
// the hashes of their signatures.
func hostHelpers(files []*partialFile) {
	names := map[string]bool{}
	for _, f := range files {
		maps.Copy(names, f.used)
	}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		var host *partialFile
		for _, f := range files {
			if !f.used[name] {
				continue
			}
			if !f.constrained {
				host = f
				break
			}
			host = cmp.Or(host, f)
		}
		host.hosted = append(host.hosted, name)
	}
}
