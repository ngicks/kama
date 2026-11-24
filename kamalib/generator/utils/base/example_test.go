package base_test

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"iter"
	"os"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
)

// greetingId and greeting are the generator the package doc writes. Their
// code must stay textually the same as the doc example.
var greetingId = base.Id{Namespace: "acme", Name: "greeting"}

func init() { base.MustRegister(greetingId) }

var greeting = base.Generator[[]string]{
	Id: greetingId,
	Generate: func(
		_ context.Context,
		_ base.Env,
		names []string,
	) iter.Seq2[base.Output, error] {
		return func(yield func(base.Output, error) bool) {
			var parts []base.Part

			for _, name := range names {
				if !token.IsExported(name) {
					// The run goes on, so that every bad name is reported.
					if !yield(nil, fmt.Errorf("greeting: %q is not exported", name)) {
						return
					}

					continue
				}

				parts = append(parts, base.Decl(fmt.Sprintf(
					"// %s greets %s.\nconst %s = %q\n", name, name, name, "hello, "+name,
				)))
			}

			yield(base.Composed{
				Path:    "greeting/greeting.go",
				Package: "greeting",
				Parts:   parts,
			}, nil)
		}
	},
}

// TestExampleMatchesDoc keeps the doc example compiled: the declarations of
// the doc are those of this file.
func TestExampleMatchesDoc(t *testing.T) {
	f, err := parser.ParseFile(
		token.NewFileSet(), "doc.go", nil, parser.PackageClauseOnly|parser.ParseComments,
	)
	assert.NilError(t, err)

	doc := f.Doc.Text()
	start := strings.Index(doc, "\tvar greetingId = ")
	end := strings.Index(doc, "\n# Helpers\n")
	assert.Assert(t, start >= 0 && end > start, "no example in\n%s", doc)

	example := strings.ReplaceAll(strings.TrimRight(doc[start:end], "\n"), "\n\t", "\n")
	example = strings.TrimPrefix(example, "\t")

	src, err := os.ReadFile("example_test.go")
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(src), example), "want\n%s", example)
}

func TestExampleGreeting(t *testing.T) {
	ctx := context.Background()
	env := base.Env{
		Reporter: gentest.NopReporter{},
		Now:      gentest.Now(),
		Registry: base.DefaultRegistry,
	}

	_, err := base.Assemble(ctx, greeting, env, []string{"alice", "Bob", "carol"})
	assert.ErrorContains(t, err, `greeting: "alice" is not exported`)
	assert.ErrorContains(t, err, `greeting: "carol" is not exported`)

	files, err := base.Assemble(ctx, greeting, env, []string{"Bob"})
	assert.NilError(t, err)
	assert.Equal(t, len(files), 1)
	assert.Equal(t, files[0].Path, "greeting/greeting.go")
	assert.Assert(t, strings.HasSuffix(
		string(files[0].Content),
		"package greeting\n\n//kama:generated acme:greeting\n"+
			"// Bob greets Bob.\nconst Bob = \"hello, Bob\"\n",
	), string(files[0].Content))
}
