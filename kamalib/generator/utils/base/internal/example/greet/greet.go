// Package greet is a generator generating from a document: it reads names
// from a document and declares a greeting function per name.
package greet

import (
	"context"
	"fmt"
	"go/token"
	"io/fs"
	"iter"
	"path"
	"strings"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	"github.com/ngicks/kama/kamalib/workflow"
)

var id = base.Id{Namespace: "acme", Name: "greet"}

func init() {
	base.MustRegister(id)
	workflow.Register(id.ProcKind(), func() workflow.Proc { return proc })
}

// Param is the param of a step: the document holding the names, and the
// directory and the package of the generated file.
type Param struct {
	Input   string         `json:"input"`
	Dir     string         `json:"dir"`
	Package string         `json:"package"`
	Unknown map[string]any `json:",embed"`
}

// Default returns the values of the keys a step leaves out.
func (Param) Default() Param {
	return Param{Input: "names.yaml", Dir: "greet", Package: "greet"}
}

// UnknownKeys reports the keys of a step Param does not know.
func (p *Param) UnknownKeys() [][]string { return unknownkey.Paths(p.Unknown) }

// Input is the input of a run: the param of the step and the names of the
// document.
type Input struct {
	Param Param
	Names []string
}

var proc = base.Proc[base.ParamOf[Param], Param, base.None, Input]{
	Generator: base.Generator[Input]{Id: id, Generate: generate},
	Check: func(p Param) error {
		if !fs.ValidPath(p.Input) || !fs.ValidPath(p.Dir) {
			return fmt.Errorf("greet: input and dir must be slash-separated relative paths")
		}

		return nil
	},
	Input: func(
		_ context.Context,
		p Param,
		_ base.None,
		out *overlayfs.Fs,
	) (Input, base.Scope, error) {
		var doc struct {
			Names []string `json:"names"`
		}
		if err := fileloader.LoadFile(out.FS(), p.Input, &doc); err != nil {
			return Input{}, base.Scope{}, fmt.Errorf("greet: %w", err)
		}

		return Input{Param: p, Names: doc.Names}, base.Scope{Dirs: []string{p.Dir}}, nil
	},
}

func generate(_ context.Context, _ base.Env, in Input) iter.Seq2[base.Output, error] {
	return func(yield func(base.Output, error) bool) {
		// greet never refers to the package of the file, so the package
		// name stands for its path.
		imports, err := importdecl.NewFile(in.Param.Package, in.Param.Package,
			importdecl.Import{Package: importdecl.Package{Path: "fmt", Name: "fmt"}},
		)
		if err != nil {
			yield(nil, fmt.Errorf("greet: %w", err))
			return
		}

		fmtId, _ := imports.Ident("fmt")
		parts := []base.Part{
			base.Text("// The functions below greet the names of " + in.Param.Input + "."),
		}

		for _, name := range in.Names {
			if !token.IsExported(name) {
				// The run goes on, so that every bad name is reported.
				if !yield(nil, fmt.Errorf("greet: %q is not exported", name)) {
					return
				}

				continue
			}

			parts = append(parts, base.Decl(fmt.Sprintf(
				"// Greet%[1]s greets %[1]s.\n"+
					"func Greet%[1]s() string { return %[2]s.Sprintf(%[3]q, %[1]q) }\n",
				name,
				fmtId,
				"hello, %s",
			)))
		}

		c := base.Composed{
			Path:    path.Join(in.Param.Dir, id.Naming().SharedFile("greet")),
			Params:  map[string]any{"input": in.Param.Input},
			Package: in.Param.Package,
			Imports: imports,
			Parts:   parts,
		}
		if !yield(c, nil) {
			return
		}

		yield(base.Whole{
			Path:    path.Join(in.Param.Dir, "names.txt"),
			Content: []byte(strings.Join(in.Names, "\n") + "\n"),
		}, nil)
	}
}
