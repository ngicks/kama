package jsonschema

import (
	"context"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/gentest"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs/goformat"
)

// File is a file Generate yields, as an overlay stages it.
type File = base.File

// generateFiles runs Generator on doc with opts at gentest.Now and returns
// its files and its warnings. A file holds its content as the default
// formatter of an overlay formats it on staging.
func generateFiles(doc *jsonschema.Schema, opts Options) ([]File, []error, error) {
	ctx := context.Background()
	r := &gentest.RecordReporter{}
	env := base.Env{Reporter: r, Now: gentest.Now(), Registry: base.DefaultRegistry}

	files, err := base.Assemble(ctx, Generator, env, Input{Doc: doc, Options: opts})
	if err != nil {
		return nil, nil, err
	}

	for i, f := range files {
		content, err := goformat.Imports{}.Format(ctx, f.Path, f.Content)
		if err != nil {
			return nil, nil, err
		}

		files[i].Content = content
	}

	return files, r.Warnings, nil
}
