package partial

import (
	"bytes"
	"fmt"
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
	"D", "DE", "DV", "E", "K", "M", "P", "PP", "S", "SE", "SV", "T", "V", "zero",
	"d", "l", "o", "out", "p", "r", "s", "u", "v", "x",
}

// helpers are keyed by name. Helpers writing into a destination pointer
// take it first, so every type argument is inferred from the arguments.
var helpers = map[string]helper{
	"kamaPartialFromValue": {
		imports: []string{undPath},
		doc:     "returns v as a defined value.",
		src: `func kamaPartialFromValue[T any](v T) $und.Und[T] {
	return $und.Defined(v)
}`,
	},
	"kamaPartialToValue": {
		imports: []string{undPath},
		doc:     "returns the value of u, or the zero value when u is undefined or null.",
		src: `func kamaPartialToValue[T any](u $und.Und[T]) T {
	return u.Value()
}`,
	},
	"kamaPartialFromSlice": {
		imports: []string{undPath},
		doc:     "returns a nil v as null and any other v as a defined value.",
		src: `func kamaPartialFromSlice[S ~[]E, E any](v S) $und.Und[S] {
	if v == nil {
		return $und.Null[S]()
	}
	return $und.Defined(v)
}`,
	},
	"kamaPartialFromMap": {
		imports: []string{undPath},
		doc:     "returns a nil v as null and any other v as a defined value.",
		src: `func kamaPartialFromMap[M ~map[K]V, K comparable, V any](v M) $und.Und[M] {
	if v == nil {
		return $und.Null[M]()
	}
	return $und.Defined(v)
}`,
	},
	"kamaPartialFromPointer": {
		imports: []string{undPath},
		doc:     "returns a nil v as null and any other v as its defined pointee.",
		src: `func kamaPartialFromPointer[T any](v *T) $und.Und[T] {
	if v == nil {
		return $und.Null[T]()
	}
	return $und.Defined(*v)
}`,
	},
	"kamaPartialToPointer": {
		imports: []string{undPath},
		doc:     "returns a pointer to a copy of the value of u, or nil when u is undefined or null.",
		src: `func kamaPartialToPointer[T any](u $und.Und[T]) *T {
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
		src: `func kamaPartialFromOption[T any](o $opt.Option[T]) $und.Und[T] {
	return $und.FromOption(o, true)
}`,
	},
	"kamaPartialToOption": {
		imports: []string{undPath, optPath},
		doc:     "returns a defined u as some and any other u as none.",
		src: `func kamaPartialToOption[T any](u $und.Und[T]) $opt.Option[T] {
	return u.Flatten()
}`,
	},
	"kamaPartialFromTarget": {
		imports: []string{undPath},
		doc:     "sets d to the defined partial value of v.",
		src: `func kamaPartialFromTarget[V any, P any, PP interface {
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
		src: `func kamaPartialFromSliceTarget[V ~[]E, E any, P any, PP interface {
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
		src: `func kamaPartialFromMapTarget[V ~map[K]E, K comparable, E any, P any, PP interface {
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
	"kamaPartialToTarget": {
		imports: []string{undPath},
		doc:     "sets d to the value u describes, or to the zero value when u is undefined or null.",
		src: `func kamaPartialToTarget[V any, P interface{ ToValue() V }](d *V, u $und.Und[P]) {
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
		src: `func kamaPartialFromTargetPointer[V any, P any, PP interface {
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
		src: `func kamaPartialToTargetPointer[V any, P interface{ ToValue() V }](d **V, u $und.Und[P]) {
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
		src: `func kamaPartialFromTargetRef[V any, P any, PP interface {
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
		src: `func kamaPartialToTargetRef[V any, P interface{ ToValue() V }](d **V, u $und.Und[*P]) {
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
		src: `func kamaPartialFromEmbeddedPointer[V any, P any, PP interface {
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
		imports: []string{reflectPath},
		doc: "sets d to a new value p describes, or to nil when every field of p is undefined.\n" +
			"//\n" +
			"// The zero value of a partial type has every field undefined, and partial\n" +
			"// types are not comparable in general, so reflect tells the zero value.",
		src: `func kamaPartialToEmbeddedPointer[V any, P interface{ ToValue() V }](d **V, p P) {
	if $reflect.ValueOf(&p).Elem().IsZero() {
		*d = nil
		return
	}
	v := p.ToValue()
	*d = &v
}`,
	},
	"kamaPartialMakeSlice": {
		doc: "sets d to nil for a nil s and to a new slice of the length of s otherwise.",
		src: `func kamaPartialMakeSlice[D ~[]DE, S ~[]SE, DE, SE any](d *D, s S) {
	if s == nil {
		*d = nil
		return
	}
	*d = make(D, len(s))
}`,
	},
	"kamaPartialMakeMap": {
		doc: "sets d to nil for a nil s and to a new empty map otherwise.",
		src: `func kamaPartialMakeMap[D ~map[K]DV, S ~map[K]SV, K comparable, DV, SV any](d *D, s S) {
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
		src: `func kamaPartialMergeOverwrite[T any](l, r $und.Und[T]) $und.Und[T] {
	if r.IsUndefined() {
		return l
	}
	return r
}`,
	},
	"kamaPartialMergeOverwriteElastic": {
		imports: []string{elasticPath},
		doc:     "returns l when r is undefined and r otherwise.",
		src: `func kamaPartialMergeOverwriteElastic[T any](
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
			"// when both are defined, and r otherwise.",
		src: `func kamaPartialMergeAppendSlice[S ~[]E, E any](l, r $und.Und[S]) $und.Und[S] {
	if r.IsUndefined() {
		return l
	}
	if !l.IsDefined() || !r.IsDefined() {
		return r
	}
	return $und.Defined(kamaPartialAppendSlice(l.Value(), r.Value()))
}`,
	},
	"kamaPartialMergeAppendMap": {
		imports: []string{undPath},
		doc: "returns l when r is undefined, a new map of the entries of l with the\n" +
			"// entries of r set when both are defined, and r otherwise.",
		src: `func kamaPartialMergeAppendMap[M ~map[K]V, K comparable, V any](
	l, r $und.Und[M],
) $und.Und[M] {
	if r.IsUndefined() {
		return l
	}
	if !l.IsDefined() || !r.IsDefined() {
		return r
	}
	return $und.Defined(kamaPartialAppendMap(l.Value(), r.Value()))
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
		src: `func kamaPartialOverwriteSlice[S ~[]E, E any](l, r S) S {
	if r == nil {
		return l
	}
	return r
}`,
	},
	"kamaPartialOverwriteMap": {
		doc: "returns l when r is nil and r otherwise.",
		src: `func kamaPartialOverwriteMap[M ~map[K]V, K comparable, V any](l, r M) M {
	if r == nil {
		return l
	}
	return r
}`,
	},
	"kamaPartialAppendSlice": {
		doc: "returns l when r is nil, r when l is nil, and a new slice of the\n" +
			"// elements of l then r otherwise.",
		src: `func kamaPartialAppendSlice[S ~[]E, E any](l, r S) S {
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
	"kamaPartialAppendMap": {
		imports: []string{mapsPath},
		doc: "returns l when r is nil, r when l is nil, and a new map of the entries\n" +
			"// of l with the entries of r set otherwise.",
		src: `func kamaPartialAppendMap[M ~map[K]V, K comparable, V any](l, r M) M {
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

// helperDeps are the helpers other helpers call.
var helperDeps = map[string][]string{
	"kamaPartialMergeAppendSlice": {"kamaPartialAppendSlice"},
	"kamaPartialMergeAppendMap":   {"kamaPartialAppendMap"},
}

func helperNames() []string { return slices.Sorted(maps.Keys(helpers)) }

// renderHelpers prints the helpers file of pkg holding used and the
// helpers they call, sorted by name.
func renderHelpers(pkg *packages.Package, used map[string]bool) ([]byte, error) {
	names := map[string]bool{}
	for name := range used {
		names[name] = true
		for _, dep := range helperDeps[name] {
			names[dep] = true
		}
	}

	paths := map[string]bool{}
	for name := range names {
		for _, path := range helpers[name].imports {
			paths[path] = true
		}
	}

	parser := importdecl.NewParser(pkg.Types)
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
	for _, name := range slices.Sorted(maps.Keys(names)) {
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
