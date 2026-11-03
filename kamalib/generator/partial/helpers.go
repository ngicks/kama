package partial

import (
	"bytes"
	"fmt"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
)

// HelpersFile is the name of the file holding the helper functions the
// generated methods of a package call. The generator owns it and writes it
// again on every run.
const HelpersFile = "kama_partial_helpers.go"

const (
	fmtPath     = "fmt"
	mapsPath    = "maps"
	reflectPath = "reflect"
)

// helper is a generic function the generated methods call. Its source
// refers to packages as $name., replaced by the identifiers the file
// imports them through.
type helper struct {
	imports []string
	doc     string
	src     string
}

// helperIdents are the identifiers the helper sources declare inside
// their functions, which import names must not shadow.
var helperIdents = []string{
	"D", "DE", "DV", "E", "K", "M", "P", "PP", "PT", "S", "SE", "SV", "T", "V", "zero",
	"d", "err", "l", "name", "o", "out", "p", "r", "s", "u", "v", "x",
}

// helpers are keyed by name. Helpers writing into a destination pointer
// take it first, so every type argument is inferred from the arguments.
var helpers = map[string]helper{
	"kamaPartialFromValue": {
		imports: []string{undPath},
		doc:     "returns v as a defined value.",
		src: `func kamaPartialFromValue[T interface{}](v T) $und.Und[T] {
	return $und.Defined(v)
}`,
	},
	"kamaPartialToValue": {
		imports: []string{undPath},
		doc:     "returns the value of u, or the zero value when u is undefined or null.",
		src: `func kamaPartialToValue[T interface{}](u $und.Und[T]) T {
	return u.Value()
}`,
	},
	"kamaPartialFromSlice": {
		imports: []string{undPath},
		doc:     "returns a nil v as null and any other v as a defined value.",
		src: `func kamaPartialFromSlice[S ~[]E, E interface{}](v S) $und.Und[S] {
	if v == nil {
		return $und.Null[S]()
	}
	return $und.Defined(v)
}`,
	},
	"kamaPartialFromMap": {
		imports: []string{undPath},
		doc:     "returns a nil v as null and any other v as a defined value.",
		src: `func kamaPartialFromMap[M ~map[K]V, K comparable, V interface{}](v M) $und.Und[M] {
	if v == nil {
		return $und.Null[M]()
	}
	return $und.Defined(v)
}`,
	},
	"kamaPartialFromPointer": {
		imports: []string{undPath},
		doc:     "returns a nil v as null and any other v as its defined pointee.",
		src: `func kamaPartialFromPointer[T interface{}](v *T) $und.Und[T] {
	if v == nil {
		return $und.Null[T]()
	}
	return $und.Defined(*v)
}`,
	},
	"kamaPartialToPointer": {
		imports: []string{undPath},
		doc:     "returns a pointer to a copy of the value of u, or nil when u is undefined or null.",
		src: `func kamaPartialToPointer[T interface{}](u $und.Und[T]) *T {
	if !u.IsDefined() {
		return nil
	}
	v := u.Value()
	return &v
}`,
	},
	"kamaPartialFromOption": {
		imports: []string{undPath, optPath},
		doc:     "returns a none o as null and a some o as a defined value.",
		src: `func kamaPartialFromOption[T interface{}](o $opt.Option[T]) $und.Und[T] {
	return $und.FromOption(o, true)
}`,
	},
	"kamaPartialToOption": {
		imports: []string{undPath, optPath},
		doc:     "returns a defined u as some and any other u as none.",
		src: `func kamaPartialToOption[T interface{}](u $und.Und[T]) $opt.Option[T] {
	return u.Flatten()
}`,
	},
	"kamaPartialFromTarget": {
		imports: []string{undPath},
		doc:     "sets d to the defined partial value of v.",
		src: `func kamaPartialFromTarget[V interface{}, P interface{}, PP interface {
	*P
	FromValue(V)
}](d *$und.Und[P], v V) {
	var p P
	PP(&p).FromValue(v)
	*d = $und.Defined(p)
}`,
	},
	"kamaPartialFromSliceTarget": {
		imports: []string{undPath},
		doc:     "sets d to null for a nil v and to the defined partial value of v otherwise.",
		src: `func kamaPartialFromSliceTarget[V ~[]E, E interface{}, P interface{}, PP interface {
	*P
	FromValue(V)
}](d *$und.Und[P], v V) {
	if v == nil {
		*d = $und.Null[P]()
		return
	}
	var p P
	PP(&p).FromValue(v)
	*d = $und.Defined(p)
}`,
	},
	"kamaPartialFromMapTarget": {
		imports: []string{undPath},
		doc:     "sets d to null for a nil v and to the defined partial value of v otherwise.",
		src: `func kamaPartialFromMapTarget[
	V ~map[K]E, K comparable, E interface{}, P interface{},
	PP interface {
		*P
		FromValue(V)
	},
](d *$und.Und[P], v V) {
	if v == nil {
		*d = $und.Null[P]()
		return
	}
	var p P
	PP(&p).FromValue(v)
	*d = $und.Defined(p)
}`,
	},
	"kamaPartialToTarget": {
		imports: []string{undPath},
		doc:     "sets d to the value u describes, or to the zero value when u is undefined or null.",
		src: `func kamaPartialToTarget[V interface{}, P interface{ ToValue() V }](d *V, u $und.Und[P]) {
	if !u.IsDefined() {
		var zero V
		*d = zero
		return
	}
	*d = u.Value().ToValue()
}`,
	},
	"kamaPartialFromTargetPointer": {
		imports: []string{undPath},
		doc:     "sets d to null for a nil v and to the defined partial value of *v otherwise.",
		src: `func kamaPartialFromTargetPointer[V interface{}, P interface{}, PP interface {
	*P
	FromValue(V)
}](d *$und.Und[P], v *V) {
	if v == nil {
		*d = $und.Null[P]()
		return
	}
	var p P
	PP(&p).FromValue(*v)
	*d = $und.Defined(p)
}`,
	},
	"kamaPartialToTargetPointer": {
		imports: []string{undPath},
		doc:     "sets d to a new value u describes, or to nil when u is undefined or null.",
		src: `func kamaPartialToTargetPointer[V interface{}, P interface{ ToValue() V }](
	d **V, u $und.Und[P],
) {
	if !u.IsDefined() {
		*d = nil
		return
	}
	v := u.Value().ToValue()
	*d = &v
}`,
	},
	"kamaPartialFromTargetRef": {
		imports: []string{undPath},
		doc:     "sets d to null for a nil v and to a new defined partial value of *v otherwise.",
		src: `func kamaPartialFromTargetRef[V interface{}, P interface{}, PP interface {
	*P
	FromValue(V)
}](d *$und.Und[*P], v *V) {
	if v == nil {
		*d = $und.Null[*P]()
		return
	}
	p := PP(new(P))
	p.FromValue(*v)
	*d = $und.Defined((*P)(p))
}`,
	},
	"kamaPartialToTargetRef": {
		imports: []string{undPath},
		doc:     "sets d to a new value u describes, or to nil when u is undefined, null or nil.",
		src: `func kamaPartialToTargetRef[V interface{}, P interface{ ToValue() V }](
	d **V, u $und.Und[*P],
) {
	if !u.IsDefined() || u.Value() == nil {
		*d = nil
		return
	}
	v := (*u.Value()).ToValue()
	*d = &v
}`,
	},
	"kamaPartialFromEmbeddedPointer": {
		doc: "sets d to the partial value of *v, or to the zero partial value for a nil v.",
		src: `func kamaPartialFromEmbeddedPointer[V interface{}, P interface{}, PP interface {
	*P
	FromValue(V)
}](d *P, v *V) {
	var p P
	if v != nil {
		PP(&p).FromValue(*v)
	}
	*d = p
}`,
	},
	"kamaPartialToEmbeddedPointer": {
		doc: "sets d to a new value p describes, or to nil when every field of p is undefined.",
		src: `func kamaPartialToEmbeddedPointer[V interface{}, P interface{ ToValue() V }](d **V, p P) {
	if kamaPartialAllUndefined(p) {
		*d = nil
		return
	}
	v := p.ToValue()
	*d = &v
}`,
	},
	"kamaPartialMakeSlice": {
		doc: "sets d to nil for a nil s and to a new slice of the length of s otherwise.",
		src: `func kamaPartialMakeSlice[D ~[]DE, S ~[]SE, DE, SE interface{}](d *D, s S) {
	if s == nil {
		*d = nil
		return
	}
	*d = make(D, len(s))
}`,
	},
	"kamaPartialMakeMap": {
		doc: "sets d to nil for a nil s and to a new empty map otherwise.",
		src: `func kamaPartialMakeMap[D ~map[K]DV, S ~map[K]SV, K comparable, DV, SV interface{}](
	d *D, s S,
) {
	if s == nil {
		*d = nil
		return
	}
	*d = make(D, len(s))
}`,
	},
	"kamaPartialMergeOverwrite": {
		imports: []string{undPath},
		doc:     "returns l when r is undefined and r otherwise.",
		src: `func kamaPartialMergeOverwrite[T interface{}](l, r $und.Und[T]) $und.Und[T] {
	if r.IsUndefined() {
		return l
	}
	return r
}`,
	},
	"kamaPartialAllUndefined": {
		imports: []string{reflectPath},
		doc: "reports whether every field of the partial value p is undefined.\n" +
			"//\n" +
			"// The zero value of a partial type has every field undefined, and partial\n" +
			"// types are not comparable in general, so reflect tells the zero value.",
		src: `func kamaPartialAllUndefined[P interface{}](p P) bool {
	return $reflect.ValueOf(&p).Elem().IsZero()
}`,
	},
	"kamaPartialMergeOverwriteEmbedded": {
		doc: "returns l when every field of the embedded partial value r is undefined\n" +
			"// and r otherwise. An embedded partial has no undefined state of its own.",
		src: `func kamaPartialMergeOverwriteEmbedded[P interface{}](l, r P) P {
	if kamaPartialAllUndefined(r) {
		return l
	}
	return r
}`,
	},
	"kamaPartialMergeOverwriteElastic": {
		imports: []string{elasticPath},
		doc:     "returns l when r is undefined and r otherwise.",
		src: `func kamaPartialMergeOverwriteElastic[T interface{}](
	l, r $elastic.Elastic[T],
) $elastic.Elastic[T] {
	if r.IsUndefined() {
		return l
	}
	return r
}`,
	},
	"kamaPartialMergeAppendSlice": {
		imports: []string{undPath},
		doc: "returns l when r is undefined, a new slice of the elements of l then r\n" +
			"// when both are defined, and r otherwise. Two nil slices give nil.\n" +
			"//\n" +
			"// A defined nil or empty side is not returned in place of the new slice,\n" +
			"// so the result never shares storage with l or r.",
		src: `func kamaPartialMergeAppendSlice[S ~[]E, E interface{}](l, r $und.Und[S]) $und.Und[S] {
	if r.IsUndefined() {
		return l
	}
	if !l.IsDefined() || !r.IsDefined() {
		return r
	}
	lv, rv := l.Value(), r.Value()
	if lv == nil && rv == nil {
		return r
	}
	out := make(S, 0, len(lv)+len(rv))
	out = append(out, lv...)
	return $und.Defined(append(out, rv...))
}`,
	},
	"kamaPartialMergeAppendMap": {
		imports: []string{undPath, mapsPath},
		doc: "returns l when r is undefined, a new map of the entries of l with the\n" +
			"// entries of r set when both are defined, and r otherwise. Two nil maps\n" +
			"// give nil.\n" +
			"//\n" +
			"// A defined nil or empty side is not returned in place of the new map, so\n" +
			"// the result never shares storage with l or r.",
		src: `func kamaPartialMergeAppendMap[M ~map[K]V, K comparable, V interface{}](
	l, r $und.Und[M],
) $und.Und[M] {
	if r.IsUndefined() {
		return l
	}
	if !l.IsDefined() || !r.IsDefined() {
		return r
	}
	lv, rv := l.Value(), r.Value()
	if lv == nil && rv == nil {
		return r
	}
	out := make(M, len(lv)+len(rv))
	$maps.Copy(out, lv)
	$maps.Copy(out, rv)
	return $und.Defined(out)
}`,
	},
	"kamaPartialMergeRecursive": {
		imports: []string{undPath},
		doc: "returns the merge of the values of l and r when both are defined, l when\n" +
			"// r is undefined, and r otherwise.",
		src: `func kamaPartialMergeRecursive[P interface{ Merge(P) P }](l, r $und.Und[P]) $und.Und[P] {
	if r.IsUndefined() {
		return l
	}
	if !l.IsDefined() || !r.IsDefined() {
		return r
	}
	return $und.Defined(l.Value().Merge(r.Value()))
}`,
	},
	"kamaPartialMergeRecursiveRef": {
		imports: []string{undPath},
		doc: "returns a new merge of the values of l and r when both are defined and\n" +
			"// non-nil, l when r is undefined, and r otherwise.",
		src: `func kamaPartialMergeRecursiveRef[P interface{ Merge(P) P }](
	l, r $und.Und[*P],
) $und.Und[*P] {
	if r.IsUndefined() {
		return l
	}
	if !l.IsDefined() || !r.IsDefined() || l.Value() == nil || r.Value() == nil {
		return r
	}
	v := (*l.Value()).Merge(*r.Value())
	return $und.Defined(&v)
}`,
	},
	"kamaPartialOverwriteSlice": {
		doc: "returns l when r is nil and r otherwise.",
		src: `func kamaPartialOverwriteSlice[S ~[]E, E interface{}](l, r S) S {
	if r == nil {
		return l
	}
	return r
}`,
	},
	"kamaPartialOverwriteMap": {
		doc: "returns l when r is nil and r otherwise.",
		src: `func kamaPartialOverwriteMap[M ~map[K]V, K comparable, V interface{}](l, r M) M {
	if r == nil {
		return l
	}
	return r
}`,
	},
	"kamaPartialAppendSlice": {
		doc: "returns l when r is nil, r when l is nil, and a new slice of the\n" +
			"// elements of l then r otherwise.",
		src: `func kamaPartialAppendSlice[S ~[]E, E interface{}](l, r S) S {
	if r == nil {
		return l
	}
	if l == nil {
		return r
	}
	out := make(S, 0, len(l)+len(r))
	out = append(out, l...)
	return append(out, r...)
}`,
	},
	"kamaPartialFromText": {
		imports: []string{undPath},
		doc:     "sets d to the defined value s unmarshals to as text.",
		src: `func kamaPartialFromText[T interface{}, PT interface {
	*T
	UnmarshalText([]byte) error
}](d *$und.Und[T], s string) error {
	var v T
	if err := PT(&v).UnmarshalText([]byte(s)); err != nil {
		return err
	}
	*d = $und.Defined(v)
	return nil
}`,
	},
	"kamaPartialFlagError": {
		imports: []string{fmtPath},
		doc:     "wraps err with the flag name.",
		src: `func kamaPartialFlagError(name string, err error) error {
	return $fmt.Errorf("flag --%s: %w", name, err)
}`,
	},
	"kamaPartialAppendMap": {
		imports: []string{mapsPath},
		doc: "returns l when r is nil, r when l is nil, and a new map of the entries\n" +
			"// of l with the entries of r set otherwise.",
		src: `func kamaPartialAppendMap[M ~map[K]V, K comparable, V interface{}](l, r M) M {
	if r == nil {
		return l
	}
	if l == nil {
		return r
	}
	out := make(M, len(l)+len(r))
	$maps.Copy(out, l)
	$maps.Copy(out, r)
	return out
}`,
	},
}

func helperNames() []string { return slices.Sorted(maps.Keys(helpers)) }

// renderHelpers prints the helpers file of pkg holding every helper,
// sorted by name.
//
// The file has no build constraint, while the partial files calling the
// helpers keep those of their sources. A run loads one build variant only,
// so a helper used by a partial file of another variant would be dropped
// if the file held only the helpers the loaded targets use. Holding every
// helper keeps the file the same across variants.
func renderHelpers(pkg *packages.Package) ([]byte, error) {
	paths := map[string]bool{}
	for name := range helpers {
		for _, path := range helpers[name].imports {
			paths[path] = true
		}
	}

	parser := importdecl.NewParser(pkg.Types)
	parser.Reserve(types.Universe.Names()...)
	parser.Reserve(pkg.Types.Scope().Names()...)
	parser.Reserve(helperIdents...)
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		err := parser.Require(importdecl.Import{
			Package: importdecl.Package{Path: path, Name: filepath.Base(path)},
		})
		if err != nil {
			return nil, err
		}
	}
	m, err := parser.ParseDst(nil)
	if err != nil {
		return nil, err
	}

	var pairs []string
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		ident, ok := m.Ident(path)
		if !ok {
			return nil, fmt.Errorf("partial: unknown package %q", path)
		}
		pairs = append(pairs, "$"+filepath.Base(path)+".", ident+".")
	}
	replacer := strings.NewReplacer(pairs...)

	var src strings.Builder
	src.WriteString("package " + pkg.Name + "\n")
	for _, name := range helperNames() {
		h := helpers[name]
		fmt.Fprintf(&src, "\n// %s %s\n//\n%s\n%s\n",
			name, h.doc, genmark.Marker(GeneratorName), replacer.Replace(h.src))
	}

	f, err := decorator.Parse(src.String())
	if err != nil {
		return nil, fmt.Errorf("partial: parse helpers of %s: %w", pkg.PkgPath, err)
	}
	f.Decs.Start = dst.Decorations{header, "\n"}
	m.AddMissingDst(f)

	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, f); err != nil {
		return nil, fmt.Errorf("partial: print helpers of %s: %w", pkg.PkgPath, err)
	}
	return buf.Bytes(), nil
}
