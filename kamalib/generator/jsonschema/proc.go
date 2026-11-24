package jsonschema

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register("jsonschema.generate", NewProc) }

// ProcParam configures Proc. A spec decodes into ProcParamPartial, which
// Proc merges onto DefaultProcParam.
//
//kama:partial
type ProcParam struct {
	// Schema is the path of the schema file, slash-separated and relative
	// to the root of the out of Proc. The file holds JSON. The path may be
	// followed by # and a JSON pointer, as in a.json#/0/schema, to take
	// the value it points to in the file as the schema document.
	Schema string `json:"schema"`
	// Dir is the output directory, slash-separated and relative to the
	// root of the out of Proc. Empty means the directory of Schema.
	Dir string `json:"dir"`
	// Package is the package clause of the generated file. Empty means the
	// base name of Dir.
	Package string `json:"package"`
	// TypeName names the Go type of the root schema as it is written, so it
	// must be an exported Go identifier. Empty takes the title of the root
	// schema.
	TypeName string `json:"type_name"`
	// Unknown captures the keys of a param that name no field.
	Unknown map[string]any `json:",embed"`
}

// DefaultProcParam returns the values a ProcParam takes for the keys a spec
// leaves out.
func DefaultProcParam() ProcParamPartial {
	return ProcParamPartial{}
}

// UnknownKeys implements workflow.UnknownKeyer.
func (p *ProcParamPartial) UnknownKeys() [][]string { return unknownkey.Paths(p.Unknown) }

// Proc generates the Go types of the schema file its param names and
// stages the file in its out. The schema file is read from its out, so a
// file staged by an earlier step is read too. A file of its out that would
// change in the generated: line of its header only is not staged. Proc
// reports a warning for each stale file of the output directory, and the
// number of files written and unchanged.
//
//	Param(ProcParamPartial) error
//	In()
//	Out(*overlayfs.Fs)
type Proc struct{}

// NewProc returns a new Proc. It is the constructor of the kind
// jsonschema.generate in workflow.DefaultRegistry.
func NewProc() workflow.Proc { return Proc{} }

func (Proc) ParamType() reflect.Type { return reflect.TypeFor[ProcParamPartial]() }

func (Proc) InTypes() []reflect.Type { return nil }

func (Proc) OutTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (Proc) AsInputType() reflect.Type { return nil }

func (Proc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &instance{}, nil
}

func (Proc) CleanUp(context.Context) error { return nil }

type instance struct {
	// opts has Schema and Dir set. Its Package is empty when the base name
	// of the root of fs gives it, which is not known before Out.
	opts Options
	fs   *overlayfs.Fs
}

func (i *instance) Param(partial ProcParamPartial) error {
	p := DefaultProcParam().Merge(partial).ToValue()

	file, _, err := splitSchema(p.Schema)
	if err != nil {
		return fmt.Errorf("%w: invalid schema %q: %w", ErrOptions, p.Schema, err)
	}

	if file == "" || !fs.ValidPath(file) {
		return fmt.Errorf("%w: invalid schema %q", ErrOptions, p.Schema)
	}

	dir := p.Dir
	if dir == "" {
		dir = path.Dir(file)
	}

	if !fs.ValidPath(dir) {
		return fmt.Errorf("%w: invalid dir %q", ErrOptions, dir)
	}

	pkg := p.Package

	switch {
	case pkg != "":
		if err := checkPackage(pkg); err != nil {
			return err
		}
	case dir != ".":
		pkg = path.Base(dir)
		if err := checkPackage(pkg); err != nil {
			return fmt.Errorf("%w; set package", err)
		}
	}

	if err := checkTypeName(p.TypeName); err != nil {
		return err
	}

	i.opts = Options{Package: pkg, TypeName: p.TypeName, Schema: p.Schema, Dir: dir}

	return nil
}

func (i *instance) In() {}

func (i *instance) Out(f *overlayfs.Fs) { i.fs = f }

func (i *instance) Run(ctx context.Context, r workflow.Reporter) error {
	files, warnings, err := i.generate()
	if err != nil {
		return err
	}

	stale := base.StaleCheck{Registry: base.DefaultRegistry, Generator: GeneratorId}

	staleWarnings, err := stale.Dir(i.fs.FS(), i.opts.Dir, files)
	if err != nil {
		return fmt.Errorf("jsonschema: %w", err)
	}

	return base.StageWithWarnings(ctx, i.fs, files, slices.Concat(warnings, staleWarnings), r)
}

// generate decodes the schema of i from its out and returns the files and
// the warnings Generate gives, with slash-separated paths relative to the
// root of the out.
func (i *instance) generate() ([]File, []error, error) {
	src, err := schemaValue(i.fs.FS(), i.opts.Schema)
	if err != nil {
		return nil, nil, fmt.Errorf("jsonschema: %w", err)
	}

	// The document is decoded from the raw bytes rather than taken from
	// file.load, whose decoded values lose the order of members and the
	// text of numbers.
	var doc jsonschema.Schema
	if err := jsonv2.Unmarshal(src, &doc); err != nil {
		return nil, nil, fmt.Errorf("jsonschema: %s: %w", i.opts.Schema, err)
	}

	opts := i.opts
	if opts.Package == "" {
		opts.Package = filepath.Base(i.fs.Root())
		if err := checkPackage(opts.Package); err != nil {
			return nil, nil, fmt.Errorf("%w; set package", err)
		}
	}

	files, warnings, err := Generate(&doc, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("jsonschema: %s: %w", i.opts.Schema, err)
	}

	slashed := opiter.WrapSeq(slices.Values(files)).
		Map(func(f File) File { return File{Path: filepath.ToSlash(f.Path), Content: f.Content} }).
		CollectSlice()

	return slashed, warnings, nil
}

// splitSchema splits a schema param at its first # into the path of the
// file and the JSON pointer of its fragment. The fragment is
// percent-decoded, as the fragment of a $ref is. A param without # points
// to the whole file.
func splitSchema(schema string) (string, jsontext.Pointer, error) {
	file, frag, _ := strings.Cut(schema, "#")

	frag, err := url.PathUnescape(frag)
	if err != nil {
		return "", "", err
	}

	ptr := jsontext.Pointer(frag)
	if !ptr.IsValid() {
		return "", "", fmt.Errorf("invalid JSON pointer %q", frag)
	}

	return file, ptr, nil
}

// schemaValue reads the file of a schema param from fsys and returns the
// raw value its pointer points to. The value is a slice of the file, so the
// order of members and the text of numbers survive.
func schemaValue(fsys fs.FS, schema string) (jsontext.Value, error) {
	file, ptr, err := splitSchema(schema)
	if err != nil {
		return nil, err
	}

	src, err := fs.ReadFile(fsys, file)
	if err != nil {
		return nil, err
	}

	v := jsontext.Value(src)

	for tok := range ptr.Tokens() {
		v, err = pointee(v, tok)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", schema, err)
		}
	}

	// A whole file is left for the decoder to report, as it may not even
	// be JSON.
	if k := v.Kind(); ptr != "" && k != '{' && k != 't' && k != 'f' {
		return nil, fmt.Errorf("%s: points to a value of kind %s, not to a schema", schema, k)
	}

	return v, nil
}

// pointee returns the member of v that the token of a JSON pointer names:
// an object member by name, or an array element by index.
func pointee(v jsontext.Value, tok string) (jsontext.Value, error) {
	switch v.Kind() {
	case '{':
		var members map[string]jsontext.Value
		if err := jsonv2.Unmarshal(v, &members); err != nil {
			return nil, err
		}

		m, ok := members[tok]
		if !ok {
			return nil, fmt.Errorf("no member %q", tok)
		}

		return m, nil
	case '[':
		var elems []jsontext.Value
		if err := jsonv2.Unmarshal(v, &elems); err != nil {
			return nil, err
		}

		// RFC 6901 spells an index in decimal without leading zeros.
		i, err := strconv.Atoi(tok)
		if err != nil || i < 0 || strconv.Itoa(i) != tok {
			return nil, fmt.Errorf("%q is no array index", tok)
		}

		if i >= len(elems) {
			return nil, fmt.Errorf("index %d out of range of %d elements", i, len(elems))
		}

		return elems[i], nil
	default:
		return nil, fmt.Errorf("token %q steps into a value of kind %s", tok, v.Kind())
	}
}
