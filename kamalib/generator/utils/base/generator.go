package base

import (
	"context"
	"io/fs"
	"iter"
	"time"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/workflow"
)

// Generator is a code generator the framework drives. I is the input of a
// run.
//
// Generate yields the outputs of the run in any order. It keeps yielding
// after an error, so that one run reports every problem it finds. A run
// that yields an error stages nothing.
type Generator[I any] struct {
	Id       Id
	Generate func(ctx context.Context, env Env, in I) iter.Seq2[Output, error]
}

// Env is what the framework provides to a run of a generator.
type Env struct {
	// Reporter takes the warnings of the run and messages on its progress.
	Reporter workflow.Reporter
	// Now is the time of the run. The header of every Composed records it.
	Now time.Time
	// Registry tells the generators that generated marks name.
	Registry *genmark.Registry
	// Out holds the files of the output as they are before the run, with
	// paths relative to its root. A generator editing a file in place reads
	// it from there.
	Out fs.FS
}

// Output is a file a run yields. Whole and Composed implement it.
//
// The path of an output is slash-separated and relative to the root of
// Env.Out, as fs.ValidPath accepts it.
type Output interface{ isOutput() }

// Whole is a file a generator finished itself, such as a file edited in
// place or a file in a language other than Go. The framework stages its
// Content as it is.
type Whole struct {
	Path    string
	Content []byte
}

// Composed is a Go file the framework assembles: the header naming the
// generator, Params and the time of the run, BuildConstraint, the package
// clause of Package, the import declaration of Imports, then Parts with an
// empty line between each two.
type Composed struct {
	Path string
	// Params are the settings of the run the header of the file records.
	Params map[string]any
	// BuildConstraint is the //go:build line of the file, or empty.
	BuildConstraint string
	Package         string
	// Imports are the imports the Parts use. The framework declares every
	// import Imports holds, the standard library first. Nil declares none.
	Imports *importdecl.Map
	Parts   []Part
}

func (Whole) isOutput()    {}
func (Composed) isOutput() {}

// Part is a piece of a Composed. Decl and Text implement it.
type Part interface{ isPart() }

// Decl is one top-level declaration with its doc comment. The framework
// prepends the generated mark of the generator to it. The mark is a
// directive comment, so a formatter moves it to the end of the doc
// comment.
type Decl []byte

// Text is a piece of a Composed the framework copies as it is, such as a
// comment or a declaration of no generator.
type Text []byte

func (Decl) isPart() {}
func (Text) isPart() {}
