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

// packageFile reads the package option file of the package of w: the last
// file its option-file directives name, or else the last file PackageFile
// finds in its directory. Each other file is a warning.
func (l Loader[P, T, F]) packageFile(
	fsys fs.FS,
	root string,
	w walked,
) (fileloader.Document, bool, []error, error) {
	files, err := l.documents().locate(fsys, root, w)
	if err != nil || len(files) == 0 {
		return fileloader.Document{}, false, nil, err
	}

	last := files[len(files)-1]

	read := files
	if last.path != "" {
		read = files[len(files)-1:]
	}

	var (
		doc      fileloader.Document
		warnings []error
	)

	// Every default file is read so that a file the last one overrides
	// still fails to load when it does not decode. Of the files directives
	// name, the last one alone is read.
	for _, f := range read {
		doc, err = loadDocument(fsys, f.name)
		if err != nil {
			return fileloader.Document{}, false, nil, fmt.Errorf(
				"%w: %s: %w", ErrPackageFile, f.at, err,
			)
		}
	}

	for _, f := range files[:len(files)-1] {
		warnings = append(warnings, l.overridden(w, last, f))
	}

	return doc, true, warnings, nil
}

// documents returns the DocumentLoader finding the package option files of
// l.
func (l Loader[P, T, F]) documents() DocumentLoader {
	return DocumentLoader{ToolName: l.ToolName, Generator: l.Generator, PackageFile: l.PackageFile}
}

// overridden returns the warning of the package option file loser of the
// package of w that winner overrides.
func (l Loader[P, T, F]) overridden(w walked, winner, loser namedFile) error {
	if winner.path == "" {
		return fmt.Errorf(
			"%w: package %s: %s overrides %s",
			ErrOverridden,
			w.pkg.PkgPath,
			winner.name,
			loser.name,
		)
	}

	return fmt.Errorf(
		"%w: package %s: %s %s=%s (%s) overrides %s=%s (%s)",
		ErrOverridden, w.pkg.PkgPath, "//"+l.ToolName+":"+optionFileDirective,
		l.Generator, winner.path, winner.at, l.Generator, loser.path, loser.at,
	)
}

// namedFile is a file of a package: one an option-file directive names, or
// one PackageFile finds.
type namedFile struct {
	// name is the path of the file relative to the root.
	name string
	// path is the path the directive spells, without its fragment. It is
	// empty for a file PackageFile finds.
	path string
	// fragment is the fragment the directive gives the path, when the
	// DocumentLoader takes fragments.
	fragment string
	// at locates the file in messages: the position of the directive, or
	// the package.
	at string
}

// locate returns the files of the package of w: those its option-file
// directives name for the generator in position order, each file and
// fragment once, or else
// those PackageFile finds in its directory in name order.
func (l DocumentLoader) locate(fsys fs.FS, root string, w walked) ([]namedFile, error) {
	named, err := l.namedFiles(w)
	if err != nil {
		return nil, err
	}

	if len(named) == 0 {
		return l.defaultFiles(fsys, root, w.pkg)
	}

	dir, dirErr := PackageDir(root, w.pkg)

	var files []namedFile

	for _, n := range named {
		if dirErr != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrPackageFile, n.at, dirErr)
		}

		name, err := resolve(dir, n.path)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrPackageFile, n.at, err)
		}

		// A file named again, through any spelling of its path, is the same
		// file, which the first naming locates.
		if !slices.ContainsFunc(files, func(f namedFile) bool {
			return f.name == name && f.fragment == n.fragment
		}) {
			n.name = name
			files = append(files, n)
		}
	}

	return files, nil
}

// defaultFiles returns the files PackageFile finds in the directory of pkg,
// sorted by name.
func (l DocumentLoader) defaultFiles(
	fsys fs.FS,
	root string,
	pkg *packages.Package,
) ([]namedFile, error) {
	// The default file of a directory names the types of the package
	// there. An external test package shares the directory, and its types
	// are given by an option-file directive of its own or by a central
	// option file instead.
	if l.PackageFile == nil || gopackages.IsExternalTest(pkg) {
		return nil, nil
	}

	dir, err := PackageDir(root, pkg)
	if err != nil {
		// A package outside root cannot hold a file of fsys, which a
		// default file does not require.
		return nil, nil
	}

	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("%w: package %s: %w", ErrPackageFile, pkg.PkgPath, err)
	}

	names, err := l.PackageFile.Find(sub)
	if err != nil {
		return nil, fmt.Errorf("%w: package %s: %w", ErrPackageFile, pkg.PkgPath, err)
	}

	return opiter.WrapSeq(slices.Values(slices.Sorted(slices.Values(names)))).
		Map(func(name string) namedFile {
			return namedFile{name: path.Join(dir, name), at: "package " + pkg.PkgPath}
		}).
		CollectSlice(), nil
}

// namedFiles returns the paths the option-file directives of w give the
// generator in position order.
func (l DocumentLoader) namedFiles(w walked) ([]namedFile, error) {
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
			default:
				p, frag := kv.Value, ""
				if l.Fragments {
					p, frag, _ = strings.Cut(p, "#")
				}

				if p == "" {
					fail("empty path for %s", l.Generator)
					continue
				}

				out = append(out, namedFile{path: p, fragment: frag, at: pos.String()})
			}
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return out, nil
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

// PackageDir returns the directory of pkg as a slash-separated path
// relative to root, an absolute directory. A package outside root, or one
// whose directory is unknown, is an error.
func PackageDir(root string, pkg *packages.Package) (string, error) {
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
