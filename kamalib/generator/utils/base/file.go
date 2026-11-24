package base

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

// File is a file a generator produced.
type File struct {
	// Path locates the file. A generator documents whether its paths are
	// absolute or relative; Stage takes slash-separated paths relative to
	// the root of the Fs it writes to.
	Path    string
	Content []byte
}

// inside reports whether rel, a path relative to a directory, stays in it.
func inside(rel string) bool {
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Stage writes files, whose paths are relative to the root of fsys, into
// fsys and reports to r how many it wrote and how many it left unchanged.
//
// A file of fsys that would change in the generated: line of its header
// only is left as it is, so a run without changes leaves every file byte
// for byte as it was.
func Stage(ctx context.Context, fsys *overlayfs.Fs, files []File, r workflow.Reporter) error {
	var written, kept int

	for _, f := range files {
		changed, err := stageFile(ctx, fsys, f)
		if err != nil {
			return err
		}

		if changed {
			written++
		} else {
			kept++
		}
	}

	r.Progress(fmt.Sprintf("%s written, %s unchanged", fileCount(written), fileCount(kept)))

	return nil
}

func fileCount(n int) string {
	if n == 1 {
		return "1 file"
	}

	return fmt.Sprintf("%d files", n)
}

// stageFile writes f unless the file in fsys differs from it in the
// generated: line only, and reports whether it wrote.
//
// Generators return unformatted content, and fsys.Write formats what it
// stages. The comparison uses the formatted content so it holds for any
// formatter fsys runs. An unchanged file is left untouched: restoring it
// with fsys.Write would format the existing, already formatted file again,
// and not every formatter is idempotent.
func stageFile(ctx context.Context, fsys *overlayfs.Fs, f File) (bool, error) {
	existing, err := fs.ReadFile(fsys.FS(), f.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("base: read %q: %w", f.Path, err)
	}

	if existing != nil {
		formatted, err := fsys.Format(ctx, f.Path, f.Content)
		if err != nil {
			return false, fmt.Errorf("base: format %q: %w", f.Path, err)
		}

		if genhelper.SameExceptGenerated(existing, formatted) {
			return false, nil
		}
	}

	if err := fsys.Write(ctx, f.Path, f.Content); err != nil {
		return false, fmt.Errorf("base: write %q: %w", f.Path, err)
	}

	return true, nil
}
