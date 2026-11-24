package jsonschema

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
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
	"github.com/ngicks/option/und"

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
	// Remotes map the URIs of other documents a reference may name to the
	// files of the out of Proc holding them. The generator never fetches a
	// document from the network.
	Remotes []Remote `json:"remotes"`
	// Validate makes the generated decoders call the Validate methods the
	// validator generator writes. A spec running Proc with it chains
	// packages.load and validate.generate after Proc. False leaves the calls
	// out, and decoding then checks no validation keyword.
	Validate bool `json:"validate"`
	// Unknown captures the keys of a param that name no field.
	Unknown map[string]any `json:",embed"`
}

// Remote maps a URI to a file of the out of Proc. With File, the URI
// names that file. With Dir, the URI is a prefix: a URI starting with it
// names the file of Dir at the rest of the URI, as
// http://example.com/schemas/ with schemas names
// schemas/pet.json for http://example.com/schemas/pet.json. The paths are
// slash-separated and relative to the root of the out.
type Remote struct {
	Uri  string `json:"uri"`
	Dir  string `json:"dir"`
	File string `json:"file"`
	// Unknown captures the keys of a remote that name no field.
	Unknown map[string]any `json:",embed"`
}

// DefaultProcParam returns the values a ProcParam takes for the keys a spec
// leaves out.
func DefaultProcParam() ProcParamPartial {
	return ProcParamPartial{Validate: und.Defined(true)}
}

// UnknownKeys implements workflow.UnknownKeyer.
func (p *ProcParamPartial) UnknownKeys() [][]string {
	paths := unknownkey.Paths(p.Unknown)

	for i, r := range p.Remotes.Value() {
		paths = append(paths, unknownkey.Paths(r.Unknown, "remotes", strconv.Itoa(i))...)
	}

	return paths
}

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
	opts    Options
	remotes []Remote
	fs      *overlayfs.Fs
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

	if err := checkRemotes(p.Remotes); err != nil {
		return err
	}

	i.remotes = p.Remotes
	i.opts = Options{
		Package:  pkg,
		TypeName: p.TypeName,
		Schema:   p.Schema,
		Dir:      dir,
		Validate: p.Validate,
	}

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
	opts.Load = remoteLoader(i.fs.FS(), i.remotes)

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

// checkRemotes reports a remote without a URI, or naming not exactly one of
// a file and a directory, or naming an invalid path.
func checkRemotes(remotes []Remote) error {
	for i, r := range remotes {
		target := r.Dir + r.File

		switch {
		case r.Uri == "":
			return fmt.Errorf("%w: remote %d has no uri", ErrOptions, i)
		case (r.Dir == "") == (r.File == ""):
			return fmt.Errorf("%w: remote %q needs either dir or file", ErrOptions, r.Uri)
		case !fs.ValidPath(target):
			return fmt.Errorf("%w: remote %q: invalid path %q", ErrOptions, r.Uri, target)
		}
	}

	return nil
}

// remoteLoader returns a function loading the document of a URI from the
// file of fsys the remotes map it to. A remote with File takes its URI
// alone, and one with Dir every URI it prefixes. The longest URI of the
// remotes covering a URI wins.
func remoteLoader(fsys fs.FS, remotes []Remote) func(string) (*jsonschema.Schema, error) {
	return func(uri string) (*jsonschema.Schema, error) {
		var (
			file  string
			match int
		)

		for _, r := range remotes {
			rest, ok := strings.CutPrefix(uri, r.Uri)
			if !ok || len(r.Uri) <= match || r.File != "" && rest != "" {
				continue
			}

			file, match = r.File, len(r.Uri)
			if r.Dir != "" {
				file = path.Join(r.Dir, rest)
			}
		}

		if file == "" {
			return nil, errors.New("no remote covers the URI")
		}

		if !fs.ValidPath(file) {
			return nil, fmt.Errorf("invalid path %q", file)
		}

		b, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, err
		}

		var doc jsonschema.Schema
		if err := jsonv2.Unmarshal(b, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}

		return &doc, nil
	}
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
