package genopt

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"go/types"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/opt"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

// filePackage is the package entry of an option file.
type filePackage[P, T, F any] struct {
	src Source
	// hasPackage tells whether the entry holds keys of P.
	hasPackage bool
	p          P
	types      map[string]TypeOption[T, F]
}

// fileDecoder decodes one option file.
type fileDecoder[P, T, F any] struct {
	version int
	name    string
	kind    SourceKind
	errs    []error
}

// decodeCentral decodes doc, a central option file, for the packages of
// byPath.
func (l Loader[P, T, F]) decodeCentral(
	doc fileloader.Document,
	byPath map[string]*packages.Package,
) (map[string]filePackage[P, T, F], error) {
	d := &fileDecoder[P, T, F]{version: l.Version, name: doc.Name, kind: SourceFile}
	out := map[string]filePackage[P, T, F]{}

	top, ok := d.document(doc.Data)
	if !ok {
		return nil, errors.Join(d.errs...)
	}

	d.unknownKeys(top, nil, "version", "packages")

	pkgsPath := []string{"packages"}

	pkgs, _ := d.table(top["packages"], pkgsPath)
	for _, pkgPath := range slices.Sorted(maps.Keys(pkgs)) {
		path := appendPath(pkgsPath, pkgPath)

		t, ok := d.table(pkgs[pkgPath], path)
		if !ok {
			continue
		}

		pkg := byPath[pkgPath]
		if pkg == nil {
			d.fail(path, "package %s is not loaded", pkgPath)
			continue
		}

		if fp, ok := d.packageEntry(t, path, pkg); ok {
			out[pkgPath] = fp
		}
	}

	return out, errors.Join(d.errs...)
}

// decodePackageFile decodes doc, the package option file of pkg.
func (l Loader[P, T, F]) decodePackageFile(
	doc fileloader.Document,
	pkg *packages.Package,
) (filePackage[P, T, F], error) {
	d := &fileDecoder[P, T, F]{version: l.Version, name: doc.Name, kind: SourcePackageFile}

	top, ok := d.document(doc.Data)
	if !ok {
		return filePackage[P, T, F]{}, errors.Join(d.errs...)
	}

	delete(top, "version")

	fp, _ := d.packageEntry(top, nil, pkg)

	return fp, errors.Join(d.errs...)
}

// document returns the top-level table of data after checking its version.
func (d *fileDecoder[P, T, F]) document(data any) (map[string]any, bool) {
	data, ok := d.normalize(data, nil)
	if !ok {
		return nil, false
	}

	top, ok := data.(map[string]any)
	if !ok {
		d.errs = append(
			d.errs,
			fmt.Errorf("%w: %s: the document is not a table", ErrOptionFile, d.name),
		)

		return nil, false
	}

	path := []string{"version"}

	if v, ok := top["version"]; !ok {
		d.fail(path, "version is required; want %d", d.version)
	} else if n, ok := integer(v); !ok {
		d.fail(path, "%v is not an integer", v)
	} else if n != int64(d.version) {
		d.fail(path, "unsupported version %d; want %d", n, d.version)
	}

	return top, true
}

// packageEntry decodes t, the entry of pkg at path.
func (d *fileDecoder[P, T, F]) packageEntry(
	t map[string]any,
	path []string,
	pkg *packages.Package,
) (filePackage[P, T, F], bool) {
	if pkg.Types == nil {
		d.fail(path, "package %s is loaded without types", pkg.PkgPath)
		return filePackage[P, T, F]{}, false
	}

	rest := without(t, "types")
	fp := filePackage[P, T, F]{
		src: Source{
			Kind:    d.kind,
			Name:    d.name,
			KeyPath: path,
			Target:  Target{Kind: TargetPackage, PkgPath: pkg.PkgPath},
		},
		hasPackage: len(rest) > 0,
		types:      map[string]TypeOption[T, F]{},
	}
	d.leaf(&fp.p, rest, fp.src)

	typesPath := appendPath(path, "types")

	ts, _ := d.table(t["types"], typesPath)
	for _, name := range slices.Sorted(maps.Keys(ts)) {
		if to, ok := d.typeEntry(ts[name], appendPath(typesPath, name), pkg, name); ok {
			fp.types[name] = to
		}
	}

	return fp, true
}

// typeEntry decodes v, the entry at path of the type name of pkg.
func (d *fileDecoder[P, T, F]) typeEntry(
	v any,
	path []string,
	pkg *packages.Package,
	name string,
) (TypeOption[T, F], bool) {
	t, ok := d.table(v, path)
	if !ok {
		return TypeOption[T, F]{}, false
	}

	tn, ok := pkg.Types.Scope().Lookup(name).(*types.TypeName)
	if !ok {
		d.fail(path, "type %s is not declared in %s", name, pkg.PkgPath)
		return TypeOption[T, F]{}, false
	}

	target := Target{
		Kind:    TargetType,
		PkgPath: pkg.PkgPath,
		File:    pkg.Fset.Position(tn.Pos()).Filename,
		Type:    name,
	}
	to := TypeOption[T, F]{Source: d.source(path, target)}
	d.leaf(&to.Type, without(t, "fields"), to.Source)

	fieldsPath := appendPath(path, "fields")
	fieldsValue, hasFields := t["fields"]

	st, isStruct := structOf(tn)
	if !isStruct {
		if hasFields {
			d.fail(fieldsPath, "%s.%s is not a struct type", pkg.PkgPath, name)
		}

		return to, true
	}

	fields := map[string]F{}
	to.Fields = opt.Some(fields)

	fs, _ := d.table(fieldsValue, fieldsPath)
	for _, fieldName := range slices.Sorted(maps.Keys(fs)) {
		fieldPath := appendPath(fieldsPath, fieldName)

		declared := opiter.WrapSeq(st.Fields()).
			ContainsFunc(func(v *types.Var) bool { return v.Name() == fieldName })

		if !declared {
			d.fail(fieldPath, "field %s is not declared in %s.%s", fieldName, pkg.PkgPath, name)
			continue
		}

		ft, ok := d.table(fs[fieldName], fieldPath)
		if !ok {
			continue
		}

		fieldTarget := target
		fieldTarget.Kind, fieldTarget.Field = TargetField, fieldName

		var f F

		d.leaf(&f, ft, d.source(fieldPath, fieldTarget))
		fields[fieldName] = f
	}

	return to, true
}

func (d *fileDecoder[P, T, F]) source(path []string, target Target) Source {
	return Source{Kind: d.kind, Name: d.name, KeyPath: path, Target: target}
}

// leaf decodes rest into v, a pointer to a leaf, through JSON, and tells v
// its source.
func (d *fileDecoder[P, T, F]) leaf(v any, rest map[string]any, src Source) {
	if rest == nil {
		rest = map[string]any{}
	}

	b, err := json.Marshal(rest)
	if err != nil {
		d.fail(src.KeyPath, "%w", err)
		return
	}

	if err := json.Unmarshal(b, v, json.RejectUnknownMembers(true)); err != nil {
		d.leafError(src.KeyPath, err)
		return
	}

	if s, ok := v.(SourceSetter); ok {
		s.SetSource(src)
	}
}

// leafError reports err, an error decoding the entry at path, at the key
// path it names.
func (d *fileDecoder[P, T, F]) leafError(path []string, err error) {
	var serr *json.SemanticError
	if !errors.As(err, &serr) {
		d.fail(path, "%w", err)
		return
	}

	full := slices.AppendSeq(slices.Clip(path), serr.JSONPointer.Tokens())

	switch {
	case errors.Is(serr.Err, json.ErrUnknownName):
		d.fail(full, "unknown key")
	case serr.Err != nil:
		d.fail(full, "%w", serr.Err)
	default:
		d.fail(full, "cannot decode %s into %v", kindName(serr.JSONKind), serr.GoType)
	}
}

// kindName names a JSON kind in the words of option files, whatever their
// format.
func kindName(k jsontext.Kind) string {
	switch k {
	case 'n':
		return "a null"
	case 't', 'f':
		return "a boolean"
	case '"':
		return "a string"
	case '0':
		return "a number"
	case '{':
		return "a table"
	case '[':
		return "an array"
	}

	return "a value"
}

func (d *fileDecoder[P, T, F]) fail(path []string, format string, args ...any) {
	at := d.name
	if len(path) > 0 {
		at += ": " + keyPath(path)
	}

	d.errs = append(d.errs, fmt.Errorf(
		"%w: %s: "+format,
		append([]any{ErrOptionFile, at}, args...)...,
	))
}

// table returns v as a table. A missing or null value is an empty table, so
// that a YAML key without value lists an entry without options.
func (d *fileDecoder[P, T, F]) table(v any, path []string) (map[string]any, bool) {
	switch v := v.(type) {
	case nil:
		return nil, true
	case map[string]any:
		return v, true
	}

	d.fail(path, "%v is not a table", v)

	return nil, false
}

// unknownKeys reports the keys of t other than known.
func (d *fileDecoder[P, T, F]) unknownKeys(t map[string]any, path []string, known ...string) {
	unknown := opiter.WrapSeq(maps.Keys(t)).
		Filter(func(k string) bool { return !slices.Contains(known, k) }).
		Collect(slices.Sorted)

	for _, k := range unknown {
		d.fail(appendPath(path, k), "unknown key; want %s", strings.Join(known, ", "))
	}
}

// normalize returns v with every mapping a map[string]any. YAML decodes a
// mapping holding a non-string key into a map[any]any, which is an error.
func (d *fileDecoder[P, T, F]) normalize(v any, path []string) (any, bool) {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			n, ok := d.normalize(e, appendPath(path, k))
			if !ok {
				return nil, false
			}

			out[k] = n
		}

		return out, true
	case map[any]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			s, ok := k.(string)
			if !ok {
				d.fail(path, "key %v is not a string", k)
				return nil, false
			}

			n, ok := d.normalize(e, appendPath(path, s))
			if !ok {
				return nil, false
			}

			out[s] = n
		}

		return out, true
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			n, ok := d.normalize(e, appendPath(path, strconv.Itoa(i)))
			if !ok {
				return nil, false
			}

			out[i] = n
		}

		return out, true
	}

	return v, true
}

// without returns t without the keys peeled.
func without(t map[string]any, peeled ...string) map[string]any {
	out := maps.Clone(t)
	for _, k := range peeled {
		delete(out, k)
	}

	return out
}

// integer reports the integer value of a number as the decoders give it:
// float64 for JSON, int for YAML and int64 for TOML.
func integer(v any) (int64, bool) {
	switch v := v.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case uint64:
		return int64(v), v <= 1<<63-1
	case float64:
		return int64(v), v == float64(int64(v))
	}

	return 0, false
}

func appendPath(path []string, keys ...string) []string {
	return append(slices.Clip(path), keys...)
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// keyPath spells path as a TOML dotted key, quoting the keys that need it
// such as package import paths.
func keyPath(path []string) string {
	return strings.Join(
		opiter.WrapSeq(slices.Values(path)).
			Map(quoteKey).
			CollectSlice(),
		".",
	)
}

// quoteKey quotes k unless it is a bare TOML key.
func quoteKey(k string) string {
	if bareKey.MatchString(k) {
		return k
	}

	return strconv.Quote(k)
}
