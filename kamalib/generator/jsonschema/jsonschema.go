package jsonschema

import (
	"errors"
	"fmt"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// GeneratorId identifies the generator.
var GeneratorId = base.Id{Namespace: "kama", Name: "jsonschema"}

// naming names the files the generator writes.
var naming = GeneratorId.Naming()

// generatorPath is the import path of the generator, named by the first
// line of the header of every generated file.
const generatorPath = "github.com/ngicks/kama/kamalib/generator/jsonschema"

func init() { base.MustRegister(GeneratorId) }

var (
	// ErrUnsupported reports a keyword or a shape of a schema the generator
	// cannot express in Go, and a name that gives no Go identifier. The
	// error names the JSON pointer of the schema or of the keyword.
	ErrUnsupported = errors.New("jsonschema: unsupported schema")
	// ErrConflict reports two schemas whose Go names collide in one scope.
	ErrConflict = errors.New("jsonschema: name conflict")
	// ErrOptions reports options Generate cannot work with.
	ErrOptions = errors.New("jsonschema: invalid options")
	// ErrUnknownKeyword is the warning of a member of a schema that names no
	// keyword of draft 2020-12, such as a typo or a keyword of an older
	// draft. The warning names its JSON pointer. A member whose name starts
	// with "x-" is an extension keyword and gives none.
	ErrUnknownKeyword = errors.New("jsonschema: unknown keyword")
)

// Options configures Generate.
type Options struct {
	// Package is the name of the package the generated file belongs to.
	Package string
	// TypeName names the Go type of the root schema as it is, so it must be
	// an exported Go identifier. Empty takes the title of the root schema.
	TypeName string
	// Schema is the path of the schema file, optionally followed by # and
	// the JSON pointer of the schema document in the file. The header of
	// the generated file records it as it is.
	Schema string
	// Dir is the directory the generated file lands in. Empty means the
	// directory of the file of Schema.
	Dir string
	// Load returns the document whose retrieval URI is uri, for a reference
	// naming a schema outside the document Generate is given. Generate
	// embeds the documents it loads, as described under References in the
	// package documentation. A nil Load makes such a reference an error.
	Load func(uri string) (*jsonschema.Schema, error)
	// Validate makes the decoders call Validate of the types the validator
	// generator gives one, as described under Validation in the package
	// documentation.
	Validate bool
	// Now gives the time the header of the generated file records. It is
	// time.Now when nil.
	Now func() time.Time
}

// WithDefaults returns o with empty fields set to their defaults.
func (o Options) WithDefaults() Options {
	if o.Now == nil {
		o.Now = time.Now
	}

	return o
}

// File is a generated file. Its Path is <Package>.kama_jsonschema.go in
// Options.Dir.
type File = base.File

// Generate returns the Go declarations of doc, one file holding a type for
// the root schema and for every schema under it that needs one.
//
// The warnings wrap ErrUnknownKeyword, one per unknown keyword of a schema
// the generator reads, in the order of the schemas and of the keyword
// names, then ErrUnenforced, one per validation keyword the generated code
// does not check, in the same order. The error wraps ErrUnsupported,
// ErrConflict or ErrOptions.
func Generate(doc *jsonschema.Schema, opts Options) ([]File, []error, error) {
	opts = opts.WithDefaults()

	if err := checkPackage(opts.Package); err != nil {
		return nil, nil, err
	}

	if err := checkTypeName(opts.TypeName); err != nil {
		return nil, nil, err
	}

	doc, err := withVocabularies(doc, opts)
	if err != nil {
		return nil, nil, err
	}

	doc, err = bundle(doc, opts)
	if err != nil {
		return nil, nil, err
	}

	if err := check(doc, opts); err != nil {
		return nil, nil, err
	}

	content, err := render(doc, opts)
	if err != nil {
		return nil, nil, err
	}

	dir := opts.Dir
	if dir == "" {
		file, _, _ := strings.Cut(opts.Schema, "#")
		dir = filepath.Dir(file)
	}

	path := naming.SourceFile(filepath.Join(dir, opts.Package+".go"))

	warnings := slices.Concat(unknownKeywords(doc), unenforcedKeywords(doc))

	return []File{{Path: path, Content: content}}, warnings, nil
}

// unknownKeywords returns a warning for each unknown keyword of the
// schemas of doc.
func unknownKeywords(doc *jsonschema.Schema) []error {
	var warnings []error

	for ptr, s := range schemas(doc) {
		warnings = append(warnings, opiter.WrapSeq2(opiter.MapsSorted(s.Unknown)).
			OmitL().
			Filter(func(k string) bool { return !strings.HasPrefix(k, "x-") }).
			Map(func(k string) error { return fmt.Errorf("%w: #%s", ErrUnknownKeyword, child(ptr, k)) }).
			CollectSlice()...)
	}

	return warnings
}

// checkTypeName reports name when it is neither empty nor an exported Go
// identifier.
func checkTypeName(name string) error {
	if name != "" && (!token.IsIdentifier(name) || !token.IsExported(name)) {
		return fmt.Errorf("%w: type name %q is no exported Go identifier", ErrOptions, name)
	}

	return nil
}

// checkPackage reports pkg when it is no package name.
func checkPackage(pkg string) error {
	if !token.IsIdentifier(pkg) || pkg == "_" {
		return fmt.Errorf("%w: package name %q", ErrOptions, pkg)
	}

	return nil
}
