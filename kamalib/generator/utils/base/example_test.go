package base_test

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"iter"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
)

// greetingId, greeting, countId, countInput and count are the generators
// the package doc writes. Their code must stay textually the same as the
// doc examples.
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

var countId = base.Id{Namespace: "acme", Name: "count"}

func init() { base.MustRegister(countId) }

type countInput = base.Input[struct{}, struct{}, struct{}, struct{}]

var count = base.Generator[countInput]{
	Id: countId,
	Generate: func(
		_ context.Context,
		_ base.Env,
		in countInput,
	) iter.Seq2[base.Output, error] {
		return func(yield func(base.Output, error) bool) {
			for _, pkg := range in.Packages {
				dir, err := in.Rel(filepath.Dir(pkg.GoFiles[0]))
				if err != nil {
					if !yield(nil, err) {
						return
					}

					continue
				}

				c := base.Composed{
					Path:    path.Join(dir, countId.Naming().SharedFile("count")),
					Package: pkg.Name,
					Parts: []base.Part{base.Decl(fmt.Sprintf(
						"// NumFiles counts the files of the package.\nconst NumFiles = %d\n",
						len(pkg.GoFiles),
					))},
				}
				if !yield(c, nil) {
					return
				}
			}
		}
	},
}

// TestExampleMatchesDoc keeps the doc examples compiled: each code block of
// the section Example of the doc is code of this file.
func TestExampleMatchesDoc(t *testing.T) {
	f, err := parser.ParseFile(
		token.NewFileSet(), "doc.go", nil, parser.PackageClauseOnly|parser.ParseComments,
	)
	assert.NilError(t, err)

	doc := f.Doc.Text()
	start := strings.Index(doc, "\n# Example\n")
	end := strings.Index(doc, "\n# Helpers\n")
	assert.Assert(t, start >= 0 && end > start, "no example in\n%s", doc)

	src, err := os.ReadFile("example_test.go")
	assert.NilError(t, err)

	blocks := codeBlocks(doc[start:end])
	assert.Equal(t, len(blocks), 2)

	for _, block := range blocks {
		assert.Assert(t, strings.Contains(string(src), block), "want\n%s", block)
	}
}

// codeBlocks returns the code blocks of section, a part of a doc comment,
// without their indentation.
func codeBlocks(section string) []string {
	var (
		blocks []string
		block  []string
	)

	flush := func() {
		if len(block) > 0 {
			blocks = append(blocks, strings.TrimRight(strings.Join(block, "\n"), "\n"))
		}

		block = nil
	}

	for line := range strings.SplitSeq(section, "\n") {
		switch {
		case strings.HasPrefix(line, "\t"):
			block = append(block, strings.TrimPrefix(line, "\t"))
		case line == "" && len(block) > 0:
			block = append(block, "")
		default:
			flush()
		}
	}

	flush()

	return blocks
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

func TestExampleCount(t *testing.T) {
	f, pkgs := gentest.LoadFs(t, map[string]string{
		"a.go":   "package x\n",
		"b.go":   "package x\n",
		"y/y.go": "package y\n",
	})
	env := base.Env{
		Reporter: gentest.NopReporter{},
		Now:      gentest.Now(),
		Registry: base.DefaultRegistry,
	}

	files, err := base.Assemble(t.Context(), count, env, countInput{
		Packages: base.SourcePackages(pkgs),
		Root:     f.Root(),
	})
	assert.NilError(t, err)

	got := map[string]string{}
	for _, file := range files {
		got[file.Path] = string(file.Content)
	}

	dir := gentest.InlineDir(t)
	assert.Equal(t, len(got), 2)
	assert.Assert(t, strings.HasSuffix(
		got[dir+"/zz_gen.count.acme_count.go"],
		"package x\n\n//kama:generated acme:count\n"+
			"// NumFiles counts the files of the package.\nconst NumFiles = 2\n",
	), got)
	assert.Assert(t, strings.Contains(got[dir+"/y/zz_gen.count.acme_count.go"], "NumFiles = 1"))
}
