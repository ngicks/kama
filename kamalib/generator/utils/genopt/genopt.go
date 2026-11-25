// Package genopt loads the options of a code generator into a typed tree,
// keyed by package, type and field.
//
// Options come from three sources: directive comments //tool:generator args
// in the source, central option files naming the options of any package,
// and the option file of a single package. Loader reads all of them and
// returns an Option whose leaves are the generator's own types P, T and F.
// The loader owns the structure of the tree; the leaves decode the
// directives and the option file entries of one package, type or field.
//
// # Directives
//
// A directive //tool:generator or //tool:generator:<name> applies to the
// target its comment is attached to:
//
//   - a type, in the doc comment of a top-level type spec, or of its type
//     declaration when the declaration has a single spec.
//   - a type group, in the doc comment of a type declaration with several
//     specs. Every type of the declaration gets the same Group value.
//   - a field, in the doc comment or the line comment of a field of a
//     top-level struct type, parenthesized or not. A field with several
//     names is a target per name, and an embedded field is named after its
//     type.
//   - a file, in a free-floating comment: a top-level comment group that is
//     not the doc or line comment of any declaration or spec. The package
//     clause doc comment is free-floating. A group starting after a
//     top-level declaration on the line the declaration ends on is the line
//     comment of the declaration and is not free-floating.
//
// Directives anywhere else, such as in a function body or in a nested
// struct, are ignored.
//
// The leaf of a target is allocated once as its zero value. Its
// UnmarshalKeyValue is called once per directive name present on the
// target, with the name after the generator: "" for //tool:generator and
// "merge" for //tool:generator:merge. Several lines of one directive on one
// target count as one line: their args are joined in source order and
// parsed by dircmnt.ParseArgsKeyValue. A leaf type whose pointer does not
// implement KeyValueUnmarshaler takes no directives.
//
// # Option files
//
// A central option file holds a version and a table of packages:
//
//	version: 1
//	packages:
//	  example.com/m/model:   # package import path
//	    <keys of P>
//	    types:
//	      User:
//	        <keys of T>
//	        fields:
//	          Name:
//	            <keys of F>
//
// A package option file holds version and one entry of packages at its top
// level. A key of a central option file other than version and packages is
// a warning wrapping ErrUnknownKey. The loader peels version, packages,
// types and fields, and decodes the remaining keys of each table into its
// leaf through encoding/json/v2, whatever the format of the file. A leaf
// uses json struct tags or a JSON unmarshaler of its own. A leaf whose
// pointer implements SourceSetter learns the source of its entry. A leaf
// whose pointer implements UnknownKeyer tells the keys of its entry it does
// not know, and each of them is a warning wrapping ErrUnknownKey that names
// the file and the full key path. A leaf whose pointer does not implement
// UnknownKeyer drops the keys it does not know without a warning. An entry
// holding unknown keys only sets nothing beyond its warnings. An empty type
// entry still lists its type.
//
// The tool-wide directive //tool:option-file <generator>=<path> in a
// free-floating comment names the package option file of a generator. The
// path is slash-separated and relative to the package directory. It may
// hold "..", but it must not be absolute or leave the root. Its extension
// gives the format. A named file that does not exist is an error. The
// directive joins its lines as other directives do, and Filter does not
// apply to it. A generator named more than once for a package, in one
// comment or in several files, takes the last path in file name and
// position order, with a warning for each other one. Loader.PackageFile
// finds the file of a package without the directive in the package
// directory. Of several files it finds, the last in name order is read,
// with a warning for each other one.
//
// Of a package and its test variant, the loader reads the test variant,
// which holds the directives of the _test.go files declaring the package
// too. An external test package x_test is a package of its own at the
// import path of x followed by _test. It shares the directory of x, and
// the file Loader.PackageFile finds there belongs to x: x_test reads a
// package option file only when an option-file directive of its own files
// names one.
//
// # Documents
//
// A generator whose input is a document of its own format, such as a
// schema, reads the package file as that document rather than as an
// option tree. DocumentLoader finds the documents of a package by the same
// directive and the same default file, and returns their bytes without
// decoding them. A generator named several times for a package by
// option-file directives has a document per path, in file name and
// position order, without a warning. A generator uses either Loader or
// DocumentLoader: one file cannot be both.
//
// # Precedence
//
// The Group, Type and Fields of one type come from exactly one source. A package option file comes
// before a central option
// file, and a central option file before the directive comments. Of several
// central option files, the later one in name order comes first. The
// options of a source that comes later are dropped with a warning naming
// both sources. An entry naming no field whose leaf implements Overlayer
// adds to them instead when its OverlayOn says so, without a warning. The
// type then keeps the source, the group and the fields of the source that
// comes later. The package entry P follows the same rule among the option
// files. The free-floating directives of the files of a package are kept
// beside it.
package genopt

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"iter"
	"maps"
	"slices"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/opt"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// Loader loads the options of one generator.
type Loader[P, T, F any] struct {
	// ToolName is the tool part of the directives, as kama in //kama:partial.
	ToolName string
	// Generator selects the directives //ToolName:Generator and
	// //ToolName:Generator:<name>, and the args of //ToolName:option-file.
	Generator string
	// Version is the required value of the version key of option files.
	Version int
	// File finds the central option files, relative to the root. No file
	// is read when File is nil.
	File fileloader.Input
	// PackageFile finds the option file of a package whose directives name
	// none, relative to the package directory. No file is read when
	// PackageFile is nil.
	PackageFile fileloader.Input
	// Filter returning false skips the directives of a target. Nil keeps
	// all of them. It does not apply to //ToolName:option-file.
	Filter func(Target) bool
}

// Loaded is the result of a Loader.
type Loaded[P, T, F any] struct {
	Option   Option[P, T, F]
	Warnings []error
	// Files are the names of the central option files read, in name order.
	Files []string
	// PackageFiles are the names of the package option files whose options
	// are used, keyed by package path.
	PackageFiles map[string]string
}

// LoadOption loads the directives of pkgs as LoadPackages does and the option
// files as LoadFiles does, and keeps the options of each type from the
// source that comes first.
//
// fsys is rooted at root, an absolute directory of the OS. A package path
// given twice, as by a test variant, counts once.
func (l Loader[P, T, F]) LoadOption(
	fsys fs.FS,
	root string,
	pkgs []*packages.Package,
) (*Loaded[P, T, F], error) {
	pkgs = uniquePackages(pkgs)

	ws, err := walk(pkgs, l.ToolName)
	if err != nil {
		return nil, err
	}

	dirs, dirErr := l.fromDirectives(ws)
	files, fileErr := l.fromFiles(fsys, root, ws)

	if err := errors.Join(dirErr, fileErr); err != nil {
		return nil, err
	}

	option, dropped := overlay(dirs, files.Option)

	return &Loaded[P, T, F]{
		Option:       option,
		Files:        files.Files,
		PackageFiles: files.PackageFiles,
		Warnings:     slices.Concat(files.Warnings, dropped),
	}, nil
}

// LoadPackages loads the directive comments of pkgs only. It reads no file.
//
// pkgs must be loaded with packages.NeedName, packages.NeedCompiledGoFiles
// and packages.NeedSyntax. With packages.NeedTypes too, whether a type is a
// struct type is taken from its type rather than from its declaration.
func (l Loader[P, T, F]) LoadPackages(pkgs []*packages.Package) (*Loaded[P, T, F], error) {
	ws, err := walk(uniquePackages(pkgs), l.ToolName)
	if err != nil {
		return nil, err
	}

	o, err := l.fromDirectives(ws)
	if err != nil {
		return nil, err
	}

	return &Loaded[P, T, F]{Option: o}, nil
}

// LoadFiles loads the option files of pkgs only: the central option files
// File finds in fsys and the package option file of each package.
//
// fsys is rooted at root, an absolute directory of the OS. pkgs must be
// loaded as LoadPackages requires and with packages.NeedTypes, and the
// directories of the packages with option files must lie under root.
func (l Loader[P, T, F]) LoadFiles(
	fsys fs.FS,
	root string,
	pkgs []*packages.Package,
) (*Loaded[P, T, F], error) {
	pkgs = uniquePackages(pkgs)

	ws, err := walk(pkgs, l.ToolName)
	if err != nil {
		return nil, err
	}

	return l.fromFiles(fsys, root, ws)
}

// Option is the option tree of a generator.
type Option[P, T, F any] struct {
	// Packages are keyed by package import path.
	Packages map[string]PackageOption[P, T, F]
}

// Type returns the options of the type id.
func (o Option[P, T, F]) Type(id typegraph.QualifiedType) (TypeOption[T, F], bool) {
	to, ok := o.Packages[id.PackagePath].Types[id.TypeName]
	return to, ok
}

// Field returns the options of the field named field of the type id.
func (o Option[P, T, F]) Field(id typegraph.QualifiedType, field string) F {
	to, _ := o.Type(id)
	fields, _ := to.Fields.Unpack()

	return fields[field]
}

// Types yields the options of every type ordered by package path and type
// name.
func (o Option[P, T, F]) Types() iter.Seq2[typegraph.QualifiedType, TypeOption[T, F]] {
	return func(yield func(typegraph.QualifiedType, TypeOption[T, F]) bool) {
		for pkgPath, po := range opiter.MapsSorted(o.Packages) {
			for name, to := range opiter.MapsSorted(po.Types) {
				if !yield(typegraph.NewQualifiedType(pkgPath, name), to) {
					return
				}
			}
		}
	}
}

// PackageOption holds the options of a package.
type PackageOption[P, T, F any] struct {
	// Package is the package entry of an option file.
	Package P
	// Files hold the free-floating directives, keyed by the Go file they are
	// written in.
	Files map[string]P
	// Types are keyed by type name.
	Types map[string]TypeOption[T, F]
}

// TypeOption holds the options of a type.
type TypeOption[T, F any] struct {
	// Source is the one source the options of the type come from. For
	// directives, its Pos is the first directive of the type, its group or
	// its fields.
	Source Source
	// Group holds the directives of the type group of the type. Every type
	// of the group gets the same value.
	Group T
	Type  T
	// Fields are keyed by field name, an embedded field named after its
	// type. It is none for a type that is not a struct type.
	Fields opt.Option[map[string]F]
}

// KeyValueUnmarshaler is implemented by the pointer types of leaf option
// types that take directives.
type KeyValueUnmarshaler interface {
	// UnmarshalKeyValue decodes the args of the directive of the given name
	// on the target of src. directive is "" for //tool:generator and the
	// part after the generator otherwise.
	UnmarshalKeyValue(src Source, directive string, kv []dircmnt.KeyValue) error
}

// SourceSetter is implemented by the pointer types of leaf option types
// that record where an option file entry comes from.
type SourceSetter interface {
	// SetSource is called after the entry of src is decoded into the leaf.
	SetSource(src Source)
}

// Overlayer is implemented by leaf option types T of types whose entry in
// a source that comes first may add to the options of a source that comes
// later instead of dropping them.
type Overlayer[T any] interface {
	// OverlayOn returns lower with the options of the receiver set on it. It
	// returns false when the receiver holds an option that does not add to
	// lower, and the receiver then drops lower as Precedence describes.
	OverlayOn(lower T) (T, bool)
}

// UnknownKeyer is implemented by the pointer types of leaf option types
// that capture the keys of an option file entry they do not know, as a
// field Unknown map[string]any `json:",embed"` does. The loader warns of
// no unknown key of a leaf whose pointer does not implement it.
type UnknownKeyer interface {
	// UnknownKeys returns the key paths of the unknown keys, relative to the
	// entry. They include the unknown keys of nested tables.
	UnknownKeys() [][]string
}

// SourceKind tells the kind of a Source.
type SourceKind int

const (
	// SourceDirective is a directive comment.
	SourceDirective SourceKind = iota + 1
	// SourceFile is a central option file.
	SourceFile
	// SourcePackageFile is the option file of a single package.
	SourcePackageFile
)

// Source tells where an option value comes from, for error messages.
type Source struct {
	Kind SourceKind
	// Name is the Go file of a directive or the name of an option file.
	Name string
	// Pos is the position of the first directive comment. It is zero for
	// option files.
	Pos token.Position
	// KeyPath is the key path of the entry in an option file. It is nil for
	// directives and for the package entry of a package option file.
	KeyPath []string
	Target  Target
}

// String locates s: the position of a directive, or the option file name
// followed by the key path of its entry spelled as a TOML dotted key.
func (s Source) String() string {
	if s.Kind == SourceDirective {
		return s.Pos.String()
	}

	if len(s.KeyPath) == 0 {
		return s.Name
	}

	return s.Name + ": " + keyPath(s.KeyPath)
}

// Origin locates an option of s: the position of a directive, or the
// option file and the key path of its entry followed by keys. name is the
// key when keys is nil, and no key follows when name is empty too.
func (s Source) Origin(name string, keys []string) string {
	if s.Kind == SourceDirective {
		return s.Pos.String()
	}

	if keys == nil && name != "" {
		keys = []string{name}
	}

	s.KeyPath = append(slices.Clip(s.KeyPath), keys...)

	return s.String()
}

// Spelling names an option of s as its source spells it. For a directive,
// name follows //tool:generator after sep; an empty name leaves the
// directive bare. For an option file, name is the key of the option within
// the entry of s; an empty name stands for the type entry.
func (s Source) Spelling(tool, generator, sep, name string) string {
	if s.Kind == SourceDirective {
		if name == "" {
			return "//" + tool + ":" + generator
		}

		return "//" + tool + ":" + generator + sep + name
	}

	what := "type entry"
	if name != "" {
		what = name + " option"
	}

	if s.Kind == SourcePackageFile {
		return "package option file " + what
	}

	return "option file " + what
}

// TargetKind tells the kind of a Target.
type TargetKind int

const (
	// TargetFreeFloating is a Go file, the target of its free-floating
	// comments.
	TargetFreeFloating TargetKind = iota + 1
	// TargetTypeGroup is a type declaration with several specs.
	TargetTypeGroup
	// TargetType is a type.
	TargetType
	// TargetField is a field of a struct type.
	TargetField
	// TargetPackage is the package entry of an option file.
	TargetPackage
)

// Target is what an option applies to.
type Target struct {
	Kind    TargetKind
	PkgPath string
	// File is the Go file a directive is written in, or the file declaring
	// the type of an option file entry.
	File  string
	Type  string
	Field string
	// Node is the node a directive is attached to: the *ast.File of a
	// free-floating comment, the *ast.GenDecl of a type group, the
	// *ast.TypeSpec of a type or the *ast.Field of a field. It is nil for
	// option file entries.
	Node ast.Node
}

// Describe names t in error messages.
func (t Target) Describe() string {
	switch t.Kind {
	case TargetFreeFloating:
		return "a free-floating comment"
	case TargetTypeGroup:
		return "a type group"
	case TargetType:
		return "type " + t.PkgPath + "." + t.Type
	case TargetField:
		return "field " + t.FieldName()
	case TargetPackage:
		return "package " + t.PkgPath
	}

	return "an unknown target"
}

// FieldName spells the field of t qualified by its package and type.
func (t Target) FieldName() string { return t.PkgPath + "." + t.Type + "." + t.Field }

// IgnoreFreeFloating is a Loader.Filter that skips the directives of
// free-floating comments.
func IgnoreFreeFloating(t Target) bool { return t.Kind != TargetFreeFloating }

var (
	// ErrDirective reports a directive whose args do not parse, or a
	// directive on a target whose leaf takes none.
	ErrDirective = errors.New("genopt: invalid directive")
	// ErrOptionFile reports an option file whose structure is invalid or
	// whose entries name packages, types or fields that are not loaded.
	ErrOptionFile = errors.New("genopt: invalid option file")
	// ErrPackageFile reports an invalid option-file directive or a package
	// option file that cannot be loaded.
	ErrPackageFile = errors.New("genopt: invalid package option file")
	// ErrOverridden reports options dropped for those of a source that
	// comes first. It is a warning.
	ErrOverridden = errors.New("genopt: options overridden")
	// ErrUnknownKey reports a key of an option file that the loader or the
	// leaf of its entry does not know. It is a warning.
	ErrUnknownKey = errors.New("genopt: unknown key")
)

// uniquePackages returns pkgs sorted by path with each path once. A test
// variant shares the path of its package and holds the test files too, so
// the variant is preferred, as base.SourcePackages prefers it.
func uniquePackages(pkgs []*packages.Package) []*packages.Package {
	byPath := map[string]*packages.Package{}
	for _, pkg := range pkgs {
		if _, ok := byPath[pkg.PkgPath]; !ok || gopackages.IsTestVariant(pkg) {
			byPath[pkg.PkgPath] = pkg
		}
	}

	return opiter.WrapSeq2(opiter.MapsSorted(byPath)).
		OmitF().
		CollectSlice()
}

// overlay returns lower with the package entries and the types of upper set
// on it, and a warning for each type of lower that upper drops. It writes to
// neither lower nor upper. The result shares the options of the packages,
// types and fields with them.
func overlay[P, T, F any](lower, upper Option[P, T, F]) (Option[P, T, F], []error) {
	out := Option[P, T, F]{Packages: maps.Clone(lower.Packages)}
	if out.Packages == nil {
		out.Packages = map[string]PackageOption[P, T, F]{}
	}

	var warnings []error

	for _, pkgPath := range slices.Sorted(maps.Keys(upper.Packages)) {
		up := upper.Packages[pkgPath]
		po := out.Packages[pkgPath]
		po.Package = up.Package

		types, dropped := overlayTypes(po.Types, pkgPath, up.Types)
		po.Types = types

		warnings = append(warnings, dropped...)
		out.Packages[pkgPath] = po
	}

	return out, warnings
}

// overlayTypes returns lower, the types of the package at pkgPath, with
// upper set on it, and a warning for each type of lower that upper drops.
// It writes to neither.
func overlayTypes[T, F any](
	lower map[string]TypeOption[T, F],
	pkgPath string,
	upper map[string]TypeOption[T, F],
) (map[string]TypeOption[T, F], []error) {
	if len(upper) == 0 {
		return lower, nil
	}

	out := maps.Clone(lower)
	if out == nil {
		out = map[string]TypeOption[T, F]{}
	}

	var warnings []error

	for _, name := range slices.Sorted(maps.Keys(upper)) {
		prev, ok := out[name]
		if !ok {
			out[name] = upper[name]
			continue
		}

		if added, ok := addTo(prev, upper[name]); ok {
			out[name] = added
			continue
		}

		warnings = append(warnings, overridden(
			"type "+pkgPath+"."+name, upper[name].Source, prev.Source,
		))
		out[name] = upper[name]
	}

	return out, warnings
}

// addTo returns lower with the type options of upper set on it, when upper
// names no field and its leaf adds to the type options of lower as
// Overlayer tells. The result keeps the source, the group and the fields
// of lower.
func addTo[T, F any](lower, upper TypeOption[T, F]) (TypeOption[T, F], bool) {
	if fields, _ := upper.Fields.Unpack(); len(fields) > 0 {
		return TypeOption[T, F]{}, false
	}

	o, ok := any(upper.Type).(Overlayer[T])
	if !ok {
		return TypeOption[T, F]{}, false
	}

	t, ok := o.OverlayOn(lower.Type)
	if !ok {
		return TypeOption[T, F]{}, false
	}

	lower.Type = t

	return lower, true
}

func overridden(what string, winner, loser Source) error {
	return fmt.Errorf("%w: %s: %s overrides %s", ErrOverridden, what, winner, loser)
}
