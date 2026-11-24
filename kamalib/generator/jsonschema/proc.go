package jsonschema

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"reflect"
	"slices"

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
	// to the root of the out of Proc. The file holds JSON.
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

	if p.Schema == "" || !fs.ValidPath(p.Schema) {
		return fmt.Errorf("%w: invalid schema %q", ErrOptions, p.Schema)
	}

	dir := p.Dir
	if dir == "" {
		dir = path.Dir(p.Schema)
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

// generate decodes the schema file of i from its out and returns the files
// and the warnings Generate gives, with slash-separated paths relative to
// the root of the out.
func (i *instance) generate() ([]File, []error, error) {
	src, err := fs.ReadFile(i.fs.FS(), i.opts.Schema)
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
