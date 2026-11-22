package validator

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// updating reports whether the test runs with -update, the flag
// gotest.tools registers for golden files.
func updating() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

// fixtureDirs are the directories of the packages the runtime tests under
// internal/tests call the generated code of.
var fixtureDirs = []string{"internal/fixture"}

// TestFixtureUpToDate generates each fixture, whose generated files are
// loaded as input like any other file, and compares the output with the
// files on disk. With -update it writes the files that differ beyond the
// generated: line instead.
func TestFixtureUpToDate(t *testing.T) {
	for _, dir := range fixtureDirs {
		t.Run(dir, func(t *testing.T) {
			ctx := context.Background()

			f, err := overlayfs.New(dir)
			assert.NilError(t, err)

			defer f.Close()

			opts := gopackages.Options{Patterns: []string{"./..."}}
			pkgs, err := gopackages.Load(ctx, f, opts)
			assert.NilError(t, err)

			files, warnings, err := Generate(pkgs, Options{})
			assert.NilError(t, err)
			assert.Equal(t, len(warnings), 0, "%v", warnings)

			for _, file := range files {
				want, err := os.ReadFile(file.Path)
				if updating() {
					if err != nil || !genhelper.SameExceptGenerated(want, file.Content) {
						assert.NilError(t, os.WriteFile(file.Path, file.Content, 0o644))
					}

					continue
				}

				assert.NilError(t, err)
				assert.Assert(
					t,
					genhelper.SameExceptGenerated(want, file.Content),
					"%s differs; run go test ./generator/validator -run TestFixtureUpToDate -update",
					filepath.Base(file.Path),
				)

				rel, err := filepath.Rel(f.Root(), file.Path)
				assert.NilError(t, err)
				assert.NilError(t, f.Write(ctx, filepath.ToSlash(rel), file.Content))
			}

			// Load fails on any package error, so this checks the output builds.
			_, err = gopackages.Load(ctx, f, opts)
			assert.NilError(t, err)
		})
	}
}
