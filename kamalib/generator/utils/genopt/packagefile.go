package genopt

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"

	"github.com/ngicks/opiter"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

// OptionFileDirective is the tool-wide directive naming the option files of
// a package, as in //kama:option-file partial=opts.yaml.
const OptionFileDirective = "option-file"

// ErrPackageFile reports an invalid option-file directive or a package
// option file that cannot be loaded.
var ErrPackageFile = errors.New("genopt: invalid package option file")

// PackageFileInput selects the package option files of one generator.
type PackageFileInput struct {
	// Generator is the key of the generator in the args of the option-file
	// directive.
	Generator string
	// Default is probed in the directory of each package whose directives
	// name no file for Generator. Its Base is relative to the package
	// directory. An empty Base probes nothing.
	Default fileloader.PathInput
}

// PackageFile is the option file of a package.
type PackageFile struct {
	PkgPath string
	// Name is the slash-separated path of the file relative to the root of
	// the fs.FS it is read from.
	Name string
	// Data is the document decoded into an any.
	Data any
}

// LoadPackageFiles returns the option files of in.Generator for pkgs, at
// most one per package, ordered by package path.
//
// fsys is rooted at root, an absolute directory of the OS. pkgs must be
// loaded as Collect requires, and their directories must lie under root.
// A package path given twice, as by a test variant, counts once.
//
// A free-floating comment //toolName:option-file <generator>=<path> ...
// names the option file of each generator for the package of its file.
// path is slash-separated and relative to the package directory. It may
// hold "..", but it must not be absolute or leave root. Its extension
// gives the format, as fileloader.FormatOf does. Keys other than
// in.Generator are left to other generators.
//
// A package without the directive for in.Generator gets the first file of
// in.Default present in its directory, or none.
//
// The directive on a type or a field, an arg without "=", an empty path,
// in.Generator named twice for one package, and a named file that is
// missing or does not decode are errors wrapping ErrPackageFile.
func LoadPackageFiles(
	fsys fs.FS,
	root string,
	pkgs []*packages.Package,
	toolName string,
	in PackageFileInput,
) ([]PackageFile, error) {
	if in.Default.Base != "" {
		if len(in.Default.Formats) == 0 {
			return nil, fmt.Errorf("%w: default %q has no formats", ErrPackageFile, in.Default.Base)
		}

		if _, err := in.Default.Candidates(); err != nil {
			return nil, fmt.Errorf("%w: default: %w", ErrPackageFile, err)
		}
	}

	// A test variant shares the path of its package and holds the test
	// files too, so the package itself is preferred.
	byPath := map[string]*packages.Package{}
	for _, pkg := range pkgs {
		if prev, ok := byPath[pkg.PkgPath]; !ok ||
			(prev.ID != prev.PkgPath && pkg.ID == pkg.PkgPath) {
			byPath[pkg.PkgPath] = pkg
		}
	}

	sorted := opiter.WrapSeq2(opiter.MapsSorted(byPath)).
		OmitF().
		CollectSlice()

	raws, err := Collect(sorted, toolName)
	if err != nil {
		return nil, err
	}

	named, errs := namedFiles(raws, toolName, in.Generator)

	var out []PackageFile

	for _, pkg := range sorted {
		f, ok, err := loadPackageFile(fsys, root, pkg, named[pkg.PkgPath], in.Default)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if ok {
			out = append(out, f)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return out, nil
}

// namedFile is the path an option-file directive gives a generator.
type namedFile struct {
	raw  Raw
	path string
}

// namedFiles returns the paths the option-file directives of raws give
// generator, keyed by package path.
func namedFiles(raws []Raw, toolName, generator string) (map[string]namedFile, []error) {
	out := map[string]namedFile{}

	var errs []error

	fail := func(r Raw, format string, args ...any) {
		errs = append(errs, fmt.Errorf(
			"%w: %s: //%s:%s: "+format,
			append([]any{ErrPackageFile, r.Origin.Pos, toolName, OptionFileDirective}, args...)...,
		))
	}

	for _, r := range raws {
		if r.Directive != OptionFileDirective {
			continue
		}

		if r.Key.Type != "" {
			fail(r, "applies to free-floating comments only")
			continue
		}

		kvs, err := dircmnt.ParseArgsKeyValue(r.Args)
		if err != nil {
			fail(r, "%w", err)
			continue
		}

		for _, kv := range kvs {
			switch {
			case !kv.HasValue:
				fail(r, "%q has no path; want <generator>=<path>", kv.Key)
				continue
			case kv.Key != generator:
				continue
			case kv.Value == "":
				fail(r, "empty path for %s", generator)
				continue
			}

			prev, ok := out[r.Key.PkgPath]
			if !ok {
				out[r.Key.PkgPath] = namedFile{raw: r, path: kv.Value}
				continue
			}

			fail(r, "second file for %s in package %s; the first is at %s",
				generator, r.Key.PkgPath, prev.raw.Origin.Pos)
		}
	}

	return out, errs
}

// loadPackageFile loads the file named by the directive, or the default
// file of pkg when named is the zero value.
func loadPackageFile(
	fsys fs.FS,
	root string,
	pkg *packages.Package,
	named namedFile,
	def fileloader.PathInput,
) (PackageFile, bool, error) {
	if named.path == "" && def.Base == "" {
		return PackageFile{}, false, nil
	}

	dir, err := packageDir(root, pkg)

	if named.path == "" {
		if err != nil {
			// A package outside root cannot hold a file of fsys, which
			// probing does not require.
			return PackageFile{}, false, nil
		}

		var data any

		name, err := fileloader.Load(
			fsys,
			fileloader.PathInput{Base: path.Join(dir, def.Base), Formats: def.Formats},
			&data,
		)
		if errors.Is(err, fileloader.ErrNotFound) {
			return PackageFile{}, false, nil
		}

		if err != nil {
			return PackageFile{}, false, fmt.Errorf(
				"%w: package %s: %w",
				ErrPackageFile,
				pkg.PkgPath,
				err,
			)
		}

		return PackageFile{PkgPath: pkg.PkgPath, Name: name, Data: data}, true, nil
	}

	pos := named.raw.Origin.Pos
	if err != nil {
		return PackageFile{}, false, fmt.Errorf("%w: %s: %w", ErrPackageFile, pos, err)
	}

	name, err := resolve(dir, named.path)
	if err != nil {
		return PackageFile{}, false, fmt.Errorf("%w: %s: %w", ErrPackageFile, pos, err)
	}

	var data any
	if err := fileloader.LoadFile(fsys, name, &data); err != nil {
		return PackageFile{}, false, fmt.Errorf("%w: %s: %w", ErrPackageFile, pos, err)
	}

	return PackageFile{PkgPath: pkg.PkgPath, Name: name, Data: data}, true, nil
}

// resolve returns the path p names relative to the root, p being relative
// to dir, a directory relative to the root.
func resolve(dir, p string) (string, error) {
	if path.IsAbs(p) || filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return "", fmt.Errorf("absolute path %q; want a path relative to the package directory", p)
	}

	name := path.Join(dir, p)
	if !fs.ValidPath(name) {
		return "", fmt.Errorf("path %q leaves the root", p)
	}

	return name, nil
}

// packageDir returns the directory of pkg as a slash-separated path
// relative to root.
func packageDir(root string, pkg *packages.Package) (string, error) {
	dir := pkg.Dir
	// CompiledGoFiles are not consulted: a file cgo processed lies in the
	// build cache, outside the package directory.
	if dir == "" && len(pkg.GoFiles) > 0 {
		dir = filepath.Dir(pkg.GoFiles[0])
	}

	if dir == "" {
		return "", fmt.Errorf("directory of package %s is unknown", pkg.PkgPath)
	}

	rel, err := filepath.Rel(root, dir)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("package %s in %s lies outside the root %s", pkg.PkgPath, dir, root)
	}

	return filepath.ToSlash(rel), nil
}
