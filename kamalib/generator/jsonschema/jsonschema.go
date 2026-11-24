package jsonschema

import (
	"context"
	"errors"
	"fmt"
	"go/token"
	"iter"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// GeneratorId identifies the generator.
var GeneratorId = base.Id{Namespace: "kama", Name: "jsonschema"}

// naming names the files the generator writes.
var naming = GeneratorId.Naming()

func init() { base.MustRegister(GeneratorId) }

// Generator generates the Go types of the schema of an Input.
var Generator = base.Generator[Input]{Id: GeneratorId, Generate: Generate}

// Input is the input of a run of Generator.
type Input struct {
	// Doc is the schema document.
	Doc     *jsonschema.Schema
	Options Options
}

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
	// AssertFormats makes the format keyword assert the formats the
	// validator generator has a helper for, as assertedFormats lists.
	// Without it format is an annotation, as draft 2020-12 has it by
	// default.
	AssertFormats bool
	// Validate makes the decoders call Validate of the types the validator
	// generator gives one, as described under Validation in the package
	// documentation.
	Validate bool
}

// Generate yields the Go declarations of the schema of in, one file
// holding a type for the root schema and for every schema under it that
// needs one. The path of the file is <Package>.kama_jsonschema.go in
// Options.Dir.
//
// Generate warns about each unknown keyword of a schema the generator
// reads with an error wrapping ErrUnknownKeyword, in the order of the
// schemas and of the keyword names, then about each validation keyword the
// generated code does not check with an error wrapping ErrUnenforced, in
// the same order. The error it yields wraps ErrUnsupported, ErrConflict or
// ErrOptions, and names Options.Schema unless it is empty.
func Generate(_ context.Context, env base.Env, in Input) iter.Seq2[base.Output, error] {
	return func(yield func(base.Output, error) bool) {
		out, warnings, err := generate(in.Doc, in.Options)
		if err != nil {
			if in.Options.Schema != "" {
				err = fmt.Errorf("jsonschema: %s: %w", in.Options.Schema, err)
			}

			yield(nil, err)

			return
		}

		for _, w := range warnings {
			env.Reporter.Warn(w)
		}

		yield(out, nil)
	}
}

// generate returns the file of Generate and the warnings.
func generate(doc *jsonschema.Schema, opts Options) (base.Composed, []error, error) {
	if err := checkPackage(opts.Package); err != nil {
		return base.Composed{}, nil, err
	}

	if err := checkTypeName(opts.TypeName); err != nil {
		return base.Composed{}, nil, err
	}

	doc, err := withVocabularies(doc, opts)
	if err != nil {
		return base.Composed{}, nil, err
	}

	doc, err = bundle(doc, opts)
	if err != nil {
		return base.Composed{}, nil, err
	}

	if err := check(doc, opts); err != nil {
		return base.Composed{}, nil, err
	}

	parts, imports, err := render(doc, opts)
	if err != nil {
		return base.Composed{}, nil, err
	}

	dir := opts.Dir
	if dir == "" {
		file, _, _ := strings.Cut(opts.Schema, "#")
		dir = path.Dir(file)
	}

	typeName := any(nil)
	if opts.TypeName != "" {
		typeName = opts.TypeName
	}

	out := base.Composed{
		Path: filepath.ToSlash(naming.SourceFile(path.Join(dir, opts.Package+".go"))),
		Params: map[string]any{
			"schema":    opts.Schema,
			"type_name": typeName,
			"validate":  opts.Validate,
		},
		Package: opts.Package,
		Imports: imports,
		Parts:   parts,
	}

	return out, slices.Concat(unknownKeywords(doc), unenforcedKeywords(doc, opts)), nil
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
