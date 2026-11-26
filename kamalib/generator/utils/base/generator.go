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

// Output is a file a run yields. Whole and Composed implement it, and so
// do pointers to them.
//
// The path of an output is slash-separated and relative to the root of
// Env.Out, as fs.ValidPath accepts it, and names a file, not the root.
type Output interface{ isOutput() }

// Whole is a file a generator finished itself, such as a file edited in
// place or a file in a language other than Go. The framework stages its
// Content as it is, except that the output formats a Whole named *.go as
// it formats every Go file unless Verbatim is set. A Whole may replace any
// file.
type Whole struct {
	Path    string
	Content []byte
	// Verbatim stages Content byte for byte, a Whole named *.go included.
	// A generator editing a hand-written file in place sets it. The
	// formatting pass would rewrite the parts of the file it did not edit.
	Verbatim bool
}

// Composed is a Go file the framework assembles: the header naming the
// generator, Params and the time of the run, BuildConstraint, the package
// clause of Package, the import declaration of Imports, then Parts with an
// empty line between each two.
//
// A Composed replaces only a file its generator generated: one starting
// with the header naming the generator or holding a declaration carrying
// its mark.
type Composed struct {
	// Path names a Go file: it ends in .go.
	Path string
	// Params are the settings of the run the header of the file records.
	Params map[string]any
	// BuildConstraint is the //go:build line of the file, or empty.
	BuildConstraint string
	Package         string
	// Imports are the imports the Parts use. The framework declares every
	// import Imports holds in one declaration: the standard library first,
	// then, after an empty line, the paths whose first element holds a dot,
	// as goimports groups them. Nil declares none. A blank or a dot import
	// goes in a leading Text holding an import declaration of its own.
	Imports *importdecl.Map
	Parts   []Part
}

func (Whole) isOutput()    {}
func (Composed) isOutput() {}

// Part is a piece of a Composed. Decl and Text implement it.
type Part interface{ isPart() }

// Decl is one top-level declaration with its doc comment attached. The
// framework prepends the generated mark of the generator to it with
// [Stamp], and a formatter moves the mark, a directive comment, to the end
// of the doc comment.
//
// Stamp does not parse a Decl, so the mark goes on whatever the Decl
// starts with: a comment detached from the declaration by an empty line
// takes the mark and leaves the declaration unmarked, and of two
// declarations only the first is marked. An empty Decl is an error.
type Decl []byte

// Text is a piece of a Composed the framework copies as it is, such as a
// comment or a declaration of no generator.
type Text []byte

func (Decl) isPart() {}
func (Text) isPart() {}
