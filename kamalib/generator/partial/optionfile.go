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

	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

// OptionFileVersion is the only version of the option file format.
const OptionFileVersion = 1

// OptionFile holds the options of an option file. Each entry stands for the
// directive the package documentation names under Option file.
type OptionFile struct {
	// Name names the file in error messages. It is the option_file value of
	// the header of every generated file for a central option file, and the
	// package_option_file value of the files of its package for a package
	// option file. Pass a stable name relative to the project, so that
	// committed output holds no path specific to one machine.
	Name string
	// Package is the import path of the package a package option file
	// belongs to, the only key of Packages then. It is empty for a central
	// option file.
	Package string
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
	d.version(top)

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

// DecodePackageOptionFile returns the options of data, the document of the
// package option file of the package at pkgPath, decoded as for
// DecodeOptionFile. name names the file in error messages.
//
// The document holds version, all_types and types at the top level, as one
// entry of packages of a central option file holds all_types and types.
// The key paths of errors start at that level.
func DecodePackageOptionFile(name, pkgPath string, data any) (*OptionFile, error) {
	d := &optionDecoder{name: name}

	top, ok := d.table(data, nil)
	if !ok {
		return nil, errors.Join(d.errs...)
	}

	d.keys(top, nil, "version", "all_types", "types")
	d.version(top)

	o := d.pkgEntries(top, nil)
	if err := errors.Join(d.errs...); err != nil {
		return nil, err
	}

	return &OptionFile{
		Name:     name,
		Package:  pkgPath,
		Packages: map[string]PackageOptions{pkgPath: o},
	}, nil
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
	unknown := opiter.WrapSeq(maps.Keys(t)).
		Filter(func(k string) bool { return !slices.Contains(known, k) }).
		Collect(slices.Sorted)

	for _, k := range unknown {
		d.fail(appendPath(path, k), "unknown key; want %s", strings.Join(known, ", "))
	}
}

// version reports a version of the top-level table t other than
// OptionFileVersion.
func (d *optionDecoder) version(t map[string]any) {
	path := []string{"version"}

	if v, ok := t["version"]; !ok {
		d.fail(path, "version is required; want %d", OptionFileVersion)
	} else if n, ok := integer(v); !ok {
		d.fail(path, "%v is not an integer", v)
	} else if n != OptionFileVersion {
		d.fail(path, "unsupported version %d; want %d", n, OptionFileVersion)
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

	return d.pkgEntries(t, path)
}

// pkgEntries decodes all_types and types of the package table t.
func (d *optionDecoder) pkgEntries(t map[string]any, path []string) PackageOptions {
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

// raws returns the options of f as the Raw values of the directives they
// stand for, ordered by package, type and field. A package not in pkgs, a
// type not declared in its package and a field not declared in its struct
// type are errors.
func (f *OptionFile) raws(pkgs []*packages.Package) ([]genopt.Raw, error) {
	if f == nil {
		return nil, nil
	}

	byPath := map[string]*packages.Package{}
	for _, p := range pkgs {
		byPath[p.PkgPath] = p
	}

	origin := genopt.Origin{Source: f.source(), Pos: token.Position{Filename: f.Name}}
	fail := func(errs *[]error, path []string, format string, args ...any) {
		at := f.Name
		// The whole of a package option file stands for its package.
		if len(path) > 0 {
			at += ": " + keyPath(path)
		}

		*errs = append(*errs, fmt.Errorf(
			"%w: %s: "+format,
			append([]any{ErrOptionFile, at}, args...)...,
		))
	}

	var (
		out  []genopt.Raw
		errs []error
	)

	for _, pkgPath := range slices.Sorted(maps.Keys(f.Packages)) {
		po := f.Packages[pkgPath]
		path := packagePath(f.source(), pkgPath)

		pkg := byPath[pkgPath]
		if pkg == nil {
			fail(&errs, path, "package %s is not loaded", pkgPath)
			continue
		}

		if po.AllTypes {
			out = append(out, genopt.Raw{
				Key: genopt.Key{PkgPath: pkgPath}, Directive: directive, Args: "package",
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

			key := genopt.Key{PkgPath: pkgPath, Type: typeName}

			r := genopt.Raw{Key: key, Directive: directive, Value: to.Ignore, Origin: origin}
			if to.Ignore {
				r.Args = "ignore"
			}

			out = append(out, r)
			if to.FieldMask {
				out = append(out, genopt.Raw{
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

				declared := opiter.WrapSeq(st.Fields()).
					ContainsFunc(func(v *types.Var) bool { return v.Name() == fieldName })

				if !declared {
					fail(&errs, appendPath(typePath, "fields", fieldName),
						"field %s is not declared in %s.%s", fieldName, pkgPath, typeName)

					continue
				}

				key := genopt.Key{PkgPath: pkgPath, Type: typeName, Field: fieldName}
				if fo.Merge != "" {
					out = append(out, genopt.Raw{
						Key: key, Directive: directive + ":merge", Args: fo.Merge, Value: fo.Merge,
						Origin: origin,
					})
				}

				if fo.Flag != nil {
					out = append(out, genopt.Raw{
						Key: key, Directive: directive + ":flag", Value: *fo.Flag, Origin: origin,
					})
				}

				if fo.FieldMask != nil {
					out = append(out, genopt.Raw{
						Key: key, Directive: directive + ":fieldmask", Value: *fo.FieldMask,
						Origin: origin,
					})
				}
			}
		}
	}

	return out, errors.Join(errs...)
}

// source tells the kind of f.
func (f *OptionFile) source() genopt.Source {
	if f.Package != "" {
		return genopt.SourcePackageFile
	}

	return genopt.SourceFile
}

// packagePath returns the key path of the entry of the package at pkgPath
// in an option file of source. A package option file is that entry itself.
func packagePath(source genopt.Source, pkgPath string) []string {
	if source == genopt.SourcePackageFile {
		return nil
	}

	return []string{"packages", pkgPath}
}

// optionPath returns the key path of the option file entry r stands for.
func optionPath(r genopt.Raw) []string {
	path := packagePath(r.Origin.Source, r.Key.PkgPath)
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
func origin(r genopt.Raw) string {
	if r.Origin.Source == genopt.SourceDirective {
		return r.Origin.Pos.String()
	}

	return r.Origin.Pos.Filename + ": " + keyPath(optionPath(r))
}

// spelling names the option r sets as its source spells it.
func spelling(r genopt.Raw) string {
	if r.Origin.Source == genopt.SourceDirective {
		return "//" + toolName + ":" + r.Directive
	}

	name := "type entry"
	if sub, ok := strings.CutPrefix(r.Directive, directive+":"); ok {
		name = sub + " option"
	}

	if r.Origin.Source == genopt.SourcePackageFile {
		return "package option file " + name
	}

	return "option file " + name
}

// sourceConflict reports the option what set to av by a and to bv by b, which
// come from different sources.
func sourceConflict(what string, a genopt.Raw, av string, b genopt.Raw, bv string) error {
	return fmt.Errorf(
		"%w: %s is %s by %s (%s) and %s by %s (%s)",
		genopt.ErrConflict, what, av, spelling(a), origin(a), bv, spelling(b), origin(b),
	)
}
