package genopt

import (
	"errors"
	"fmt"
	"io/fs"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

// NewLoader returns the loader of the generator of tool. optionFile finds the
// central option files relative to the root; none are read when it is nil.
// A package without //tool:option-file generator=<path> reads
// tool_generator.toml, .yaml, .yml or .json in its directory, the first one
// present in that order.
func NewLoader[P, T, F any](
	tool, generator string,
	version int,
	optionFile fileloader.Input,
) Loader[P, T, F] {
	return Loader[P, T, F]{
		ToolName:  tool,
		Generator: generator,
		Version:   version,
		File:      optionFile,
		PackageFile: fileloader.PathInput{
			Base:    tool + "_" + generator,
			Formats: fileloader.DefaultFormats(),
		},
	}
}

// LoadOptionNamed loads as LoadOption does, with File set to optionFile in
// place of the File of l. A nil optionFile reads no central option file. A
// named optionFile that finds no file is an error wrapping ErrOptionFile and
// fileloader.ErrNotFound.
func (l Loader[P, T, F]) LoadOptionNamed(
	fsys fs.FS,
	root string,
	pkgs []*packages.Package,
	optionFile *fileloader.PathInput,
) (*Loaded[P, T, F], error) {
	l.File = nil
	if optionFile != nil {
		l.File = *optionFile
	}

	loaded, err := l.LoadOption(fsys, root, pkgs)
	if err != nil {
		return nil, err
	}

	if optionFile != nil && len(loaded.Files) == 0 {
		return nil, fmt.Errorf(
			"%w: %w: %q", ErrOptionFile, fileloader.ErrNotFound, optionFile.Base,
		)
	}

	return loaded, nil
}

// HeaderParams returns the option_file and package_option_file params of
// the header of a generated file of the package at pkgPath. option_file is
// the central option file read, the list of them when several are read, or
// nil. package_option_file is the package option file of the package, or
// nil. A nil l holds no file.
func (l *Loaded[P, T, F]) HeaderParams(pkgPath string) map[string]any {
	var optionFile, packageOptionFile any

	if l != nil {
		switch len(l.Files) {
		case 0:
		case 1:
			optionFile = l.Files[0]
		default:
			optionFile = l.Files
		}

		if name, ok := l.PackageFiles[pkgPath]; ok {
			packageOptionFile = name
		}
	}

	return map[string]any{
		"option_file":         optionFile,
		"package_option_file": packageOptionFile,
	}
}

// WrapLoadError marks err, an error of a Loader, with the errors of a
// generator: optionFile for ErrOptionFile and ErrPackageFile, and directive
// for ErrDirective.
func WrapLoadError(err, optionFile, directive error) error {
	if errors.Is(err, ErrOptionFile) || errors.Is(err, ErrPackageFile) {
		err = fmt.Errorf("%w: %w", optionFile, err)
	}

	if errors.Is(err, ErrDirective) {
		err = fmt.Errorf("%w: %w", directive, err)
	}

	return err
}
