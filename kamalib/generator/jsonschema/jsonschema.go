package jsonschema

import (
	"errors"
	"fmt"
	"go/token"
	"path/filepath"
	"time"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// GeneratorName is the name the generator marks its declarations with.
const GeneratorName = "kama-jsonschema"

// naming names the files the generator writes.
var naming = base.Naming{Name: "kama_jsonschema"}

// generatorPath is the import path of the generator, named by the first
// line of the header of every generated file.
const generatorPath = "github.com/ngicks/kama/kamalib/generator/jsonschema"

func init() { base.MustRegister(GeneratorName) }

var (
	// ErrUnsupported reports a keyword or a shape of a schema the generator
	// cannot express in Go, and a name that gives no Go identifier. The
	// error names the JSON pointer of the schema or of the keyword.
	ErrUnsupported = errors.New("jsonschema: unsupported schema")
	// ErrConflict reports two schemas whose Go names collide in one scope.
	ErrConflict = errors.New("jsonschema: name conflict")
	// ErrOptions reports options Generate cannot work with.
	ErrOptions = errors.New("jsonschema: invalid options")
)

// Options configures Generate.
type Options struct {
	// Package is the name of the package the generated file belongs to.
	Package string
	// TypeName names the Go type of the root schema. Empty takes the title
	// of the root schema.
	TypeName string
	// Schema is the path of the schema file. The header of the generated
	// file records it as it is.
	Schema string
	// Dir is the directory the generated file lands in. Empty means the
	// directory of Schema.
	Dir string
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
// No warning is defined yet; the slice is there for the proc, which
// reports the warnings of every generator the same way.
//
// The error wraps ErrUnsupported, ErrConflict or ErrOptions.
func Generate(doc *jsonschema.Schema, opts Options) ([]File, []error, error) {
	opts = opts.WithDefaults()

	if err := checkPackage(opts.Package); err != nil {
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
		dir = filepath.Dir(opts.Schema)
	}

	path := naming.SourceFile(filepath.Join(dir, opts.Package+".go"))

	return []File{{Path: path, Content: content}}, nil, nil
}

// checkPackage reports pkg when it is no package name.
func checkPackage(pkg string) error {
	if !token.IsIdentifier(pkg) || pkg == "_" {
		return fmt.Errorf("%w: package name %q", ErrOptions, pkg)
	}

	return nil
}
