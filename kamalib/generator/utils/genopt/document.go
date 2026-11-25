package genopt

import (
	"fmt"
	"io/fs"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

// DocumentLoader finds the documents of a package for a generator that
// reads its package file as a document of its own rather than as an option
// tree. It locates them as Loader locates the package option file, and it
// decodes nothing.
type DocumentLoader struct {
	// ToolName is the tool part of the option-file directive, as kama in
	// //kama:option-file.
	ToolName string
	// Generator selects the args of //ToolName:option-file.
	Generator string
	// PackageFile finds the document of a package whose directives name
	// none, relative to the package directory. No file is found when
	// PackageFile is nil.
	PackageFile fileloader.Input
}

// NewDocumentLoader returns the DocumentLoader of the generator of tool. A
// package without //tool:option-file generator=<path> has the file
// tool_generator in its directory with the extension of the first of
// formats present. formats defaults to fileloader.DefaultFormats.
func NewDocumentLoader(tool, generator string, formats ...fileloader.Format) DocumentLoader {
	if len(formats) == 0 {
		formats = fileloader.DefaultFormats()
	}

	return DocumentLoader{
		ToolName:  tool,
		Generator: generator,
		PackageFile: fileloader.PathInput{
			Base:    tool + "_" + generator,
			Formats: formats,
		},
	}
}

// Document is a file a DocumentLoader finds.
type Document struct {
	// Name is the path of the file, slash-separated and relative to the
	// root.
	Name string
	// Content is the content of the file as it is.
	Content []byte
}

// Documents returns the documents of pkg. They are the files the
// option-file directives of pkg name for the generator, in file name and
// position order, or else the files PackageFile finds in the directory of
// pkg, in name order. A package naming no file and holding no default file
// has none.
//
// fsys is rooted at root, an absolute directory of the OS. pkg must be
// loaded with packages.NeedName, packages.NeedCompiledGoFiles and
// packages.NeedSyntax, and lie under root when its directives name a file.
// An external test package has the files its own directives name only, as
// for Loader.
func (l DocumentLoader) Documents(
	fsys fs.FS,
	root string,
	pkg *packages.Package,
) ([]Document, error) {
	ws, err := walk([]*packages.Package{pkg}, l.ToolName)
	if err != nil {
		return nil, err
	}

	files, err := l.locate(fsys, root, ws[0])
	if err != nil {
		return nil, err
	}

	docs := make([]Document, 0, len(files))

	for _, f := range files {
		b, err := fs.ReadFile(fsys, f.name)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrPackageFile, f.at, err)
		}

		docs = append(docs, Document{Name: f.name, Content: b})
	}

	return docs, nil
}
