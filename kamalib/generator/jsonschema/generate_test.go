package jsonschema

import (
	"context"
	"iter"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// File is a file Generate yields, as an overlay stages it.
type File = base.File

// generateFiles runs Generator on doc with opts at gentest.Now and returns
// its files and its warnings. A file holds its content as the default
// formatter of an overlay formats it on staging.
func generateFiles(doc *jsonschema.Schema, opts Options) ([]File, []error, error) {
	r := &gentest.RecordReporter{}
	g := Generator
	generate := g.Generate
	// The reporter of gentest.Prepare drops the warnings, which the tests
	// read.
	g.Generate = func(ctx context.Context, env base.Env, in Input) iter.Seq2[base.Output, error] {
		env.Reporter = r
		return generate(ctx, env, in)
	}

	files, err := gentest.Prepare(
		context.Background(), g, base.DefaultRegistry, Input{Doc: doc, Options: opts},
	)
	if err != nil {
		return nil, nil, err
	}

	return files, r.Warnings, nil
}
