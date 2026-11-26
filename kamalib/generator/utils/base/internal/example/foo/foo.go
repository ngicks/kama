// Package foo is a generator generating from loaded packages: for each
// type marked //acme:foo it declares a method returning a label.
package foo

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ngicks/option/opt"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	"github.com/ngicks/kama/kamalib/workflow"
)

var id = base.Id{Namespace: "acme", Name: "foo"}

func init() {
	base.MustRegister(id)
	workflow.Register(id.ProcKind(), func() workflow.Proc { return proc })
}

// Param is the param of a step: the name of the generated method.
type Param struct {
	Method  string         `json:"method"`
	Unknown map[string]any `json:",embed"`
}

// Default returns the values of the keys a step leaves out.
func (Param) Default() Param { return Param{Method: "Label"} }

// UnknownKeys reports the keys of a step Param does not know.
func (p *Param) UnknownKeys() [][]string { return unknownkey.Paths(p.Unknown) }

// TypeOptions are the options of a type: //acme:foo label="<label>", or the
// key label of its entry in an option file.
type TypeOptions struct {
	Label   string         `json:"label"`
	Unknown map[string]any `json:",embed"`
}

// UnmarshalExpr decodes the items of the directive //acme:foo.
func (o *TypeOptions) UnmarshalExpr(src genopt.Source, directive string, args []expr.Arg) error {
	for _, a := range args {
		if directive != "" || a.Name != "label" {
			return fmt.Errorf("%s: unknown arg %s at %d:%d", src, a, a.Start.Line, a.Start.Column)
		}

		label, err := genopt.KeywordString(a)
		if err != nil {
			return fmt.Errorf("%s: arg at %d:%d: %w", src, a.Start.Line, a.Start.Column, err)
		}

		o.Label = label
	}

	return nil
}

// UnknownKeys reports the keys of an option file entry TypeOptions does
// not know.
func (o *TypeOptions) UnknownKeys() [][]string { return unknownkey.Paths(o.Unknown) }

// Input is the input of a run. foo takes no package and no field options.
type Input = base.Input[Param, struct{}, TypeOptions, struct{}]

var proc = base.SourceProc[base.ParamOf[Param], Param, Param, struct{}, TypeOptions, struct{}]{
	Generator: base.Generator[Input]{Id: id, Generate: generate},
	Options: func(p Param) (Param, opt.Option[fileloader.PathInput], error) {
		return p, opt.None[fileloader.PathInput](), nil
	},
	Loader: base.NewOptionLoader[struct{}, TypeOptions, struct{}](id, 1, nil),
}

func generate(_ context.Context, _ base.Env, in Input) iter.Seq2[base.Output, error] {
	return func(yield func(base.Output, error) bool) {
		for _, pkg := range in.Packages {
			types := in.Loaded.Option.Packages[pkg.PkgPath].Types
			if len(types) == 0 {
				continue
			}

			var parts []base.Part

			for _, name := range slices.Sorted(maps.Keys(types)) {
				label := types[name].Type.Label
				if label == "" {
					label = strings.ToLower(name)
				}

				parts = append(parts, base.Decl(fmt.Sprintf(
					"// %[2]s returns the label of %[1]s.\nfunc (%[1]s) %[2]s() string { return %[3]q }\n",
					name,
					in.Options.Method,
					label,
				)))
			}

			dir, err := in.Rel(filepath.Dir(pkg.GoFiles[0]))
			if err != nil {
				if !yield(nil, err) {
					return
				}

				continue
			}

			c := base.Composed{
				Path:    path.Join(dir, base.PackageFile(pkg, id.Naming().SharedFile("foo"))),
				Params:  in.Loaded.HeaderParams(pkg.PkgPath),
				Package: pkg.Name,
				Parts:   parts,
			}
			if !yield(c, nil) {
				return
			}
		}
	}
}
