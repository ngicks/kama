package contextkey

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"go/token"
	"iter"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register(GeneratorId().ProcKind(), NewProc) }

// ProcParam configures the proc. A spec decodes into ProcParamPartial, which
// the proc merges onto DefaultProcParam.
//
//kama:partial
type ProcParam struct {
	// KeyTypeFunc names the function of a package that creates keys, for
	// the template files that name none. See Options.KeyTypeFunc.
	KeyTypeFunc string `json:"key_type_func"`
	// Unknown captures the keys of a param that name no field.
	Unknown map[string]any `json:",embed"`
}

// DefaultProcParam returns the values a ProcParam takes for the keys a spec
// leaves out.
func DefaultProcParam() ProcParamPartial { return ProcParamPartial{} }

// UnknownKeys implements workflow.UnknownKeyer.
func (p *ProcParamPartial) UnknownKeys() [][]string { return unknownkey.Paths(p.Unknown) }

// proc runs Generator on the template files of the packages its in loaded
// and stages the files in its out, as base.Proc does. The template files
// are read from its out, so a file staged by an earlier step is read too.
// Its stale check covers the Go files of the directories of the packages.
//
//	Param(ProcParamPartial) error
//	In(*gopackages.Packages)
//	Out(*overlayfs.Fs)
var proc = base.Proc[ProcParamPartial, ProcParam, *gopackages.Packages, procIn]{
	Generator: base.Generator[procIn]{Id: GeneratorId(), Generate: generatePackages},
	Default:   DefaultProcParam,
	Check:     checkParam,
	Input:     procInput,
}

// NewProc returns the proc of the generator. It is the constructor of the kind
// contextkey.generate in workflow.DefaultRegistry.
func NewProc() workflow.Proc { return proc }

// documents finds the template files of a package.
var documents = base.NewDocumentLoader(GeneratorId())

// procIn is the input of a run of the proc.
type procIn struct {
	// pkgs are the packages the run reads, as base.SourcePackages picks
	// them.
	pkgs []*packages.Package
	// root is the absolute directory of the root of the out.
	root  string
	param ProcParam
}

func checkParam(p ProcParam) error {
	if p.KeyTypeFunc != "" && !token.IsIdentifier(p.KeyTypeFunc) {
		return fmt.Errorf("contextkey: key type func %q is not an identifier", p.KeyTypeFunc)
	}

	return nil
}

func procInput(
	_ context.Context,
	p ProcParam,
	pkgs *gopackages.Packages,
	out *overlayfs.Fs,
) (procIn, base.Scope, error) {
	in := procIn{pkgs: base.SourcePackages(pkgs.Pkgs), root: out.Root(), param: p}

	var scope base.Scope

	// A package naming no template file still counts, so that the files
	// generated from a template file it dropped are stale.
	for _, pkg := range in.pkgs {
		if dir, err := base.PackageDir(in.root, pkg); err == nil {
			scope.Dirs = append(scope.Dirs, dir)
		}
	}

	return in, scope, nil
}

// generatePackages runs Generate on each template file of each package of
// in, generating into the directory of the package.
func generatePackages(
	ctx context.Context,
	env base.Env,
	in procIn,
) iter.Seq2[base.Output, error] {
	return func(yield func(base.Output, error) bool) {
		for _, pkg := range in.pkgs {
			for out, err := range generatePackage(ctx, env, in, pkg) {
				if !yield(out, err) {
					return
				}
			}
		}
	}
}

// generatePackage runs Generate on each template file of pkg.
func generatePackage(
	ctx context.Context,
	env base.Env,
	in procIn,
	pkg *packages.Package,
) iter.Seq2[base.Output, error] {
	return func(yield func(base.Output, error) bool) {
		docs, err := documents.Documents(env.Out, in.root, pkg)
		if err != nil {
			yield(nil, fmt.Errorf("contextkey: %w", err))
			return
		}

		if len(docs) == 0 {
			return
		}

		dir, err := base.PackageDir(in.root, pkg)
		if err != nil {
			yield(nil, fmt.Errorf("contextkey: %w", err))
			return
		}

		for _, doc := range docs {
			tmpl, err := decodeDocument(doc)
			if err != nil {
				if !yield(nil, fmt.Errorf("contextkey: %s: %w", doc.Name, err)) {
					return
				}

				continue
			}

			opts := Options{Package: pkg.Name, KeyTypeFunc: in.param.KeyTypeFunc}

			for out, err := range Generate(ctx, env, Input{
				Path: doc.Name, Template: tmpl, Dir: dir, Options: opts,
			}) {
				if !yield(out, err) {
					return
				}
			}
		}
	}
}

// decodeDocument decodes doc, a template file in the format its extension
// tells, into a Template.
func decodeDocument(doc genopt.Document) (Template, error) {
	format, ok := fileloader.FormatOf(doc.Name)
	if !ok {
		return Template{}, fmt.Errorf("%w: extension of %q", fileloader.ErrUnknownFormat, doc.Name)
	}

	var data any
	if err := fileloader.Decode(bytes.NewReader(doc.Content), format, &data); err != nil {
		return Template{}, err
	}

	return decodeTemplate(data)
}

// decodeTemplate converts the format-neutral data fileloader decodes into
// a Template with encoding/json/v2. Keys match the JSON names of the fields
// of Template and Entry case-sensitively. Unknown keys are captured in the
// Unknown fields of Template and Entry.
func decodeTemplate(data any) (Template, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return Template{}, err
	}

	var tmpl Template
	if err := json.Unmarshal(b, &tmpl); err != nil {
		return Template{}, err
	}

	return tmpl, nil
}
