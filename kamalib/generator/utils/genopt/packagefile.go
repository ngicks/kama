package genopt

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
)

// optionFileDirective is the tool-wide directive naming the option files of
// a package, as in //kama:option-file partial=opts.yaml.
const optionFileDirective = "option-file"

// fromFiles loads the central option files and the package option files
// of the packages of ws and keeps the options of each type from the file
// that comes first.
func (l Loader[P, T, F]) fromFiles(fsys fs.FS, root string, ws []walked) (*Loaded[P, T, F], error) {
	byPath := map[string]*packages.Package{}
	for _, w := range ws {
		byPath[w.pkg.PkgPath] = w.pkg
	}

	loaded := &Loaded[P, T, F]{
		Option:       Option[P, T, F]{Packages: map[string]PackageOption[P, T, F]{}},
		PackageFiles: map[string]string{},
	}

	var (
		layers []map[string]filePackage[P, T, F]
		errs   []error
	)

	if l.File != nil {
		docs, err := fileloader.LoadAll(fsys, l.File)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrOptionFile, err)
		}

		for _, doc := range docs {
			loaded.Files = append(loaded.Files, doc.Name)

			layer, warnings, err := l.decodeCentral(doc, byPath)
			loaded.Warnings = append(loaded.Warnings, warnings...)

			if err != nil {
				errs = append(errs, err)
				continue
			}

			layers = append(layers, layer)
		}
	}

	pkgLayer := map[string]filePackage[P, T, F]{}

	for _, w := range ws {
		doc, ok, warnings, err := l.packageFile(fsys, root, w)
		loaded.Warnings = append(loaded.Warnings, warnings...)

		if err != nil {
			errs = append(errs, err)
			continue
		}

		if !ok {
			continue
		}

		loaded.PackageFiles[w.pkg.PkgPath] = doc.Name

		fp, warnings, err := l.decodePackageFile(doc, w.pkg)
		loaded.Warnings = append(loaded.Warnings, warnings...)

		if err != nil {
			errs = append(errs, err)
			continue
		}

		pkgLayer[w.pkg.PkgPath] = fp
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	pkgSources := map[string]Source{}

	for _, layer := range append(layers, pkgLayer) {
		for _, pkgPath := range slices.Sorted(maps.Keys(layer)) {
			fp := layer[pkgPath]
			po := loaded.Option.Packages[pkgPath]

			if fp.hasPackage {
				if prev, ok := pkgSources[pkgPath]; ok {
					loaded.Warnings = append(
						loaded.Warnings,
						overridden("package "+pkgPath, fp.src, prev),
					)
				}

				pkgSources[pkgPath] = fp.src
				po.Package = fp.p
			}

			types, dropped := overlayTypes(po.Types, pkgPath, fp.types)
			po.Types = types

			loaded.Warnings = append(loaded.Warnings, dropped...)
			loaded.Option.Packages[pkgPath] = po
		}
	}

	return loaded, nil
}

// namedFile is the path an option-file directive gives the generator.
type namedFile struct {
	path string
	pos  string
}

// packageFile reads the package option file of the package of w: the file
// its option-file directives name, or else the last file PackageFile finds
// in its directory.
func (l Loader[P, T, F]) packageFile(
	fsys fs.FS,
	root string,
	w walked,
) (fileloader.Document, bool, []error, error) {
	named, warnings, err := l.namedFiles(w)
	if err != nil {
		return fileloader.Document{}, false, nil, err
	}

	pkg := w.pkg

	if len(named) == 0 {
		// The default file of a directory names the types of the package
		// there. An external test package shares the directory, and its
		// types are given by an option-file directive of its own or by a
		// central option file instead.
		if l.PackageFile == nil || gopackages.IsExternalTest(pkg) {
			return fileloader.Document{}, false, warnings, nil
		}

		dir, err := packageDir(root, pkg)
		if err != nil {
			// A package outside root cannot hold a file of fsys, which a
			// default file does not require.
			return fileloader.Document{}, false, warnings, nil
		}

		sub, err := fs.Sub(fsys, dir)
		if err != nil {
			return fileloader.Document{}, false, nil, fmt.Errorf(
				"%w: package %s: %w", ErrPackageFile, pkg.PkgPath, err,
			)
		}

		names, err := l.PackageFile.Find(sub)
		if err != nil {
			return fileloader.Document{}, false, nil, fmt.Errorf(
				"%w: package %s: %w", ErrPackageFile, pkg.PkgPath, err,
			)
		}

		if len(names) == 0 {
			return fileloader.Document{}, false, warnings, nil
		}

		names = slices.Sorted(slices.Values(names))
		for i := range names {
			names[i] = path.Join(dir, names[i])
		}

		// Every file is read so that a file the last one overrides still
		// fails to load when it does not decode.
		var doc fileloader.Document

		last := names[len(names)-1]
		for _, name := range names {
			doc, err = loadDocument(fsys, name)
			if err != nil {
				return fileloader.Document{}, false, nil, fmt.Errorf(
					"%w: package %s: %w", ErrPackageFile, pkg.PkgPath, err,
				)
			}

			if name != last {
				warnings = append(warnings, fmt.Errorf(
					"%w: package %s: %s overrides %s", ErrOverridden, pkg.PkgPath, last, name,
				))
			}
		}

		return doc, true, warnings, nil
	}

	last := named[len(named)-1]

	dir, err := packageDir(root, pkg)
	if err != nil {
		return fileloader.Document{}, false, nil, fmt.Errorf(
			"%w: %s: %w", ErrPackageFile, last.pos, err,
		)
	}

	name, err := resolve(dir, last.path)
	if err != nil {
		return fileloader.Document{}, false, nil, fmt.Errorf(
			"%w: %s: %w", ErrPackageFile, last.pos, err,
		)
	}

	doc, err := loadDocument(fsys, name)
	if err != nil {
		return fileloader.Document{}, false, nil, fmt.Errorf(
			"%w: %s: %w", ErrPackageFile, last.pos, err,
		)
	}

	return doc, true, warnings, nil
}

// namedFiles returns the paths the option-file directives of w give the
// generator in position order, with a warning for each but the last.
func (l Loader[P, T, F]) namedFiles(w walked) ([]namedFile, []error, error) {
	var (
		out  []namedFile
		errs []error
	)

	spelled := "//" + l.ToolName + ":" + optionFileDirective
	isOptionFile := func(ln line) (string, bool) { return "", ln.directive == optionFileDirective }

	for _, tl := range groupLines(w.lines, isOptionFile) {
		pos := tl.names[0].lines[0].pos
		fail := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s: "+format,
				append([]any{ErrPackageFile, pos, spelled}, args...)...,
			))
		}

		if tl.target.Kind != TargetFreeFloating {
			fail("applies to free-floating comments only")
			continue
		}

		kvs, err := dircmnt.ParseArgsKeyValue(joinArgs(tl.names[0].lines))
		if err != nil {
			fail("%w", err)
			continue
		}

		for _, kv := range kvs {
			switch {
			case !kv.HasValue:
				fail("%q has no path; want <generator>=<path>", kv.Key)
			case kv.Key != l.Generator:
			case kv.Value == "":
				fail("empty path for %s", l.Generator)
			default:
				out = append(out, namedFile{path: kv.Value, pos: pos.String()})
			}
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, nil, err
	}

	var warnings []error

	if len(out) > 1 {
		last := out[len(out)-1]
		for _, n := range out[:len(out)-1] {
			warnings = append(warnings, fmt.Errorf(
				"%w: package %s: %s %s=%s (%s) overrides %s=%s (%s)",
				ErrOverridden, w.pkg.PkgPath, spelled,
				l.Generator, last.path, last.pos, l.Generator, n.path, n.pos,
			))
		}
	}

	return out, warnings, nil
}

// joinArgs joins the args of lines, the lines of one directive on one
// target, as if they were written on one line.
func joinArgs(lines []line) string {
	return strings.Join(
		opiter.WrapSeq(slices.Values(lines)).
			Map(func(ln line) string { return ln.args }).
			CollectSlice(),
		" ",
	)
}

func loadDocument(fsys fs.FS, name string) (fileloader.Document, error) {
	var data any
	if err := fileloader.LoadFile(fsys, name, &data); err != nil {
		return fileloader.Document{}, err
	}

	return fileloader.Document{Name: name, Data: data}, nil
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
