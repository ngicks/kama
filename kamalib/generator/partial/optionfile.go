package partial

import (
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"io/fs"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/kamaopt"
)

// OptionFileVersion is the only version of the option file format.
const OptionFileVersion = 1

// OptionFile holds the options of an option file. Each entry stands for the
// directive the package documentation names under Option file.
type OptionFile struct {
	// Name names the file in error messages and is the option_file value
	// of the header of every generated file. Pass a stable name relative to
	// the project, so that committed output holds no path specific to one
	// machine.
	Name string
	// Packages are keyed by package import path.
	Packages map[string]PackageOptions
}

// PackageOptions are the options of a package.
type PackageOptions struct {
	// AllTypes stands for //kama:partial package.
	AllTypes bool
	// Types are keyed by type name. Listing a type stands for //kama:partial
	// on it, or for //kama:partial ignore when Ignore is set.
	Types map[string]TypeOptions
}

// TypeOptions are the options of a type.
type TypeOptions struct {
	Ignore bool
	// FieldMask stands for //kama:partial:fieldmask on the type.
	FieldMask bool
	// Fields are keyed by field name, an embedded field named after its
	// type.
	Fields map[string]FieldOptions
}

// FieldOptions are the options of a struct field.
type FieldOptions struct {
	// Merge stands for //kama:partial:merge. Empty sets no strategy.
	Merge string
	// Flag stands for //kama:partial:flag, or for none when nil.
	Flag *FlagOptions
	// FieldMask stands for //kama:partial:fieldmask on the field, or for
	// none when nil.
	FieldMask *FieldMaskOptions
}

// FieldMaskOptions are the args of //kama:partial:fieldmask on a field.
type FieldMaskOptions struct {
	Name string
}

// FlagOptions are the args of //kama:partial:flag. An empty Short sets no
// shorthand.
type FlagOptions struct {
	Name, Short, Usage string
}

// LoadOptionFile decodes the first file of in present in fsys, as
// fileloader.Load does, and returns its options named by the file name.
func LoadOptionFile(fsys fs.FS, in fileloader.PathInput) (*OptionFile, error) {
	var data any
	name, err := fileloader.Load(fsys, in, &data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOptionFile, err)
	}
	return DecodeOptionFile(name, data)
}

// DecodeOptionFile returns the options of data, a document decoded by a JSON,
// YAML or TOML decoder into maps, slices and scalars. name names the file
// in error messages.
//
// An unknown key, a value of the wrong type and a version other than
// OptionFileVersion are errors naming the file and the key path. Whether
// the packages, types and fields exist is checked by Generate.
func DecodeOptionFile(name string, data any) (*OptionFile, error) {
	d := &optionDecoder{name: name}
	f := &OptionFile{Name: name, Packages: map[string]PackageOptions{}}
	top, ok := d.table(data, nil)
	if !ok {
		return nil, errors.Join(d.errs...)
	}
	d.keys(top, nil, "version", "packages")
	if v, ok := top["version"]; !ok {
		d.fail([]string{"version"}, "version is required; want %d", OptionFileVersion)
	} else if n, ok := integer(v); !ok {
		d.fail([]string{"version"}, "%v is not an integer", v)
	} else if n != OptionFileVersion {
		d.fail([]string{"version"}, "unsupported version %d; want %d", n, OptionFileVersion)
	}

	pkgsPath := []string{"packages"}
	pkgs, _ := d.table(top["packages"], pkgsPath)
	for _, pkgPath := range slices.Sorted(maps.Keys(pkgs)) {
		f.Packages[pkgPath] = d.pkg(pkgs[pkgPath], appendPath(pkgsPath, pkgPath))
	}
	if err := errors.Join(d.errs...); err != nil {
		return nil, err
	}
	return f, nil
}

type optionDecoder struct {
	name string
	errs []error
}

func (d *optionDecoder) fail(path []string, format string, args ...any) {
	d.errs = append(d.errs, fmt.Errorf(
		"%w: %s: %s: "+format,
		append([]any{ErrOptionFile, d.name, keyPath(path)}, args...)...,
	))
}

// table returns v as a table. A missing or null value is an empty table, so
// that a YAML key without value lists an entry without options.
func (d *optionDecoder) table(v any, path []string) (map[string]any, bool) {
	switch v := v.(type) {
	case nil:
		return nil, true
	case map[string]any:
		return v, true
	case map[any]any:
		// YAML decodes a mapping holding a non-string key so.
		out := make(map[string]any, len(v))
		for k, e := range v {
			s, ok := k.(string)
			if !ok {
				d.fail(path, "key %v is not a string", k)
				return nil, false
			}
			out[s] = e
		}
		return out, true
	}
	if len(path) == 0 {
		d.errs = append(
			d.errs,
			fmt.Errorf("%w: %s: the document is not a table", ErrOptionFile, d.name),
		)
	} else {
		d.fail(path, "%v is not a table", v)
	}
	return nil, false
}

// keys reports the keys of t other than known.
func (d *optionDecoder) keys(t map[string]any, path []string, known ...string) {
	isUnknown := func(k string) bool { return !slices.Contains(known, k) }
	for _, k := range slices.Sorted(opiter.Filter(isUnknown, maps.Keys(t))) {
		d.fail(appendPath(path, k), "unknown key; want %s", strings.Join(known, ", "))
	}
}

func (d *optionDecoder) bool(t map[string]any, path []string, key string) bool {
	v, ok := t[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		d.fail(appendPath(path, key), "%v is not a boolean", v)
	}
	return b
}

func (d *optionDecoder) string(t map[string]any, path []string, key string) (string, bool) {
	v, ok := t[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		d.fail(appendPath(path, key), "%v is not a string", v)
	}
	return s, ok
}

func (d *optionDecoder) pkg(v any, path []string) PackageOptions {
	t, ok := d.table(v, path)
	if !ok {
		return PackageOptions{}
	}
	d.keys(t, path, "all_types", "types")
	o := PackageOptions{AllTypes: d.bool(t, path, "all_types"), Types: map[string]TypeOptions{}}
	typesPath := appendPath(path, "types")
	ts, _ := d.table(t["types"], typesPath)
	for _, name := range slices.Sorted(maps.Keys(ts)) {
		o.Types[name] = d.typ(ts[name], appendPath(typesPath, name))
	}
	return o
}

func (d *optionDecoder) typ(v any, path []string) TypeOptions {
	t, ok := d.table(v, path)
	if !ok {
		return TypeOptions{}
	}
	d.keys(t, path, "ignore", "fieldmask", "fields")
	o := TypeOptions{
		Ignore:    d.bool(t, path, "ignore"),
		FieldMask: d.bool(t, path, "fieldmask"),
		Fields:    map[string]FieldOptions{},
	}
	fieldsPath := appendPath(path, "fields")
	fs, _ := d.table(t["fields"], fieldsPath)
	for _, name := range slices.Sorted(maps.Keys(fs)) {
		o.Fields[name] = d.field(fs[name], appendPath(fieldsPath, name))
	}
	return o
}

func (d *optionDecoder) field(v any, path []string) FieldOptions {
	t, ok := d.table(v, path)
	if !ok {
		return FieldOptions{}
	}
	d.keys(t, path, "merge", "flag", "fieldmask")
	var o FieldOptions
	o.Merge, _ = d.string(t, path, "merge")
	o.FieldMask = d.fieldMask(t, path)
	if _, ok := t["flag"]; !ok {
		return o
	}
	flagPath := appendPath(path, "flag")
	ft, ok := d.table(t["flag"], flagPath)
	if !ok {
		return o
	}
	d.keys(ft, flagPath, "name", "short", "usage")
	o.Flag = &FlagOptions{}
	o.Flag.Name, _ = d.string(ft, flagPath, "name")
	o.Flag.Usage, _ = d.string(ft, flagPath, "usage")
	// The directive rejects an empty short=, and FlagOptions cannot tell an
	// empty Short from none, so an empty short is rejected here.
	if s, ok := d.string(ft, flagPath, "short"); ok && s == "" {
		d.fail(appendPath(flagPath, "short"), "empty shorthand; omit short or give one character")
	} else {
		o.Flag.Short = s
	}
	return o
}

// fieldMask decodes the fieldmask entry of the field table t, or returns
// nil when t has none.
func (d *optionDecoder) fieldMask(t map[string]any, path []string) *FieldMaskOptions {
	if _, ok := t["fieldmask"]; !ok {
		return nil
	}
	maskPath := appendPath(path, "fieldmask")
	mt, ok := d.table(t["fieldmask"], maskPath)
	if !ok {
		return nil
	}
	d.keys(mt, maskPath, "name")
	o := &FieldMaskOptions{}
	o.Name, _ = d.string(mt, maskPath, "name")
	return o
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
	return strings.Join(slices.Collect(opiter.Map(quoteKey, slices.Values(path))), ".")
}

// quoteKey quotes k unless it is a bare TOML key.
func quoteKey(k string) string {
	if bareKey.MatchString(k) {
		return k
	}
	return strconv.Quote(k)
}

// raws returns the options of f as the Raw values of the directives they
// stand for, ordered by package, type and field. A package not in pkgs, a
// type not declared in its package and a field not declared in its struct
// type are errors.
func (f *OptionFile) raws(pkgs []*packages.Package) ([]kamaopt.Raw, error) {
	if f == nil {
		return nil, nil
	}
	byPath := map[string]*packages.Package{}
	for _, p := range pkgs {
		byPath[p.PkgPath] = p
	}
	origin := kamaopt.Origin{Source: kamaopt.SourceFile, Pos: token.Position{Filename: f.Name}}
	fail := func(errs *[]error, path []string, format string, args ...any) {
		*errs = append(*errs, fmt.Errorf(
			"%w: %s: %s: "+format,
			append([]any{ErrOptionFile, f.Name, keyPath(path)}, args...)...,
		))
	}

	var (
		out  []kamaopt.Raw
		errs []error
	)
	for _, pkgPath := range slices.Sorted(maps.Keys(f.Packages)) {
		po := f.Packages[pkgPath]
		path := []string{"packages", pkgPath}
		pkg := byPath[pkgPath]
		if pkg == nil {
			fail(&errs, path, "package %s is not loaded", pkgPath)
			continue
		}
		if po.AllTypes {
			out = append(out, kamaopt.Raw{
				Key: kamaopt.Key{PkgPath: pkgPath}, Directive: directive, Args: "package",
				Value: true, Origin: origin,
			})
		}
		for _, typeName := range slices.Sorted(maps.Keys(po.Types)) {
			to := po.Types[typeName]
			typePath := appendPath(path, "types", typeName)
			obj, ok := pkg.Types.Scope().Lookup(typeName).(*types.TypeName)
			if !ok {
				fail(&errs, typePath, "type %s is not declared in %s", typeName, pkgPath)
				continue
			}
			key := kamaopt.Key{PkgPath: pkgPath, Type: typeName}
			r := kamaopt.Raw{Key: key, Directive: directive, Value: to.Ignore, Origin: origin}
			if to.Ignore {
				r.Args = "ignore"
			}
			out = append(out, r)
			if to.FieldMask {
				out = append(out, kamaopt.Raw{
					Key: key, Directive: directive + ":fieldmask", Value: true, Origin: origin,
				})
			}

			if len(to.Fields) == 0 {
				continue
			}
			st, ok := types.Unalias(obj.Type()).Underlying().(*types.Struct)
			if !ok {
				fail(
					&errs,
					appendPath(typePath, "fields"),
					"%s.%s is not a struct type",
					pkgPath,
					typeName,
				)
				continue
			}
			for _, fieldName := range slices.Sorted(maps.Keys(to.Fields)) {
				fo := to.Fields[fieldName]
				isField := func(v *types.Var) bool { return v.Name() == fieldName }
				if !opiter.ContainsFunc(isField, st.Fields()) {
					fail(&errs, appendPath(typePath, "fields", fieldName),
						"field %s is not declared in %s.%s", fieldName, pkgPath, typeName)
					continue
				}
				key := kamaopt.Key{PkgPath: pkgPath, Type: typeName, Field: fieldName}
				if fo.Merge != "" {
					out = append(out, kamaopt.Raw{
						Key: key, Directive: directive + ":merge", Args: fo.Merge, Value: fo.Merge,
						Origin: origin,
					})
				}
				if fo.Flag != nil {
					out = append(out, kamaopt.Raw{
						Key: key, Directive: directive + ":flag", Value: *fo.Flag, Origin: origin,
					})
				}
				if fo.FieldMask != nil {
					out = append(out, kamaopt.Raw{
						Key: key, Directive: directive + ":fieldmask", Value: *fo.FieldMask,
						Origin: origin,
					})
				}
			}
		}
	}
	return out, errors.Join(errs...)
}

// optionPath returns the key path of the option file entry r stands for.
func optionPath(r kamaopt.Raw) []string {
	path := []string{"packages", r.Key.PkgPath}
	if r.Key.Type == "" {
		return append(path, "all_types")
	}
	path = append(path, "types", r.Key.Type)
	if r.Key.Field != "" {
		path = append(path, "fields", r.Key.Field)
	} else if r.Directive == directive {
		if strings.TrimSpace(r.Args) == "ignore" {
			path = append(path, "ignore")
		}
		return path
	}
	if sub, ok := strings.CutPrefix(r.Directive, directive+":"); ok {
		path = append(path, sub)
	}
	return path
}

// origin locates r for error messages: the position of a directive, or the
// option file and key path of a file entry.
func origin(r kamaopt.Raw) string {
	if r.Origin.Source == kamaopt.SourceFile {
		return r.Origin.Pos.Filename + ": " + keyPath(optionPath(r))
	}
	return r.Origin.Pos.String()
}

// spelling names the option r sets as its source spells it.
func spelling(r kamaopt.Raw) string {
	if r.Origin.Source == kamaopt.SourceFile {
		name := "type entry"
		if sub, ok := strings.CutPrefix(r.Directive, directive+":"); ok {
			name = sub + " option"
		}
		return "option file " + name
	}
	return "//" + toolName + ":" + r.Directive
}

// sourceConflict reports the option what set to av by a and to bv by b, which
// come from different sources.
func sourceConflict(what string, a kamaopt.Raw, av string, b kamaopt.Raw, bv string) error {
	return fmt.Errorf(
		"%w: %s is %s by %s (%s) and %s by %s (%s)",
		kamaopt.ErrConflict, what, av, spelling(a), origin(a), bv, spelling(b), origin(b),
	)
}
