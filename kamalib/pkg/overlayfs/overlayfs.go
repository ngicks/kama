// Package overlayfs is the rooted target filesystem a graph runs against.
//
// It stacks a writable in-memory layer over the project directory. Steps read
// the merged view, so generated output is visible to later steps as if it
// were on disk, and steps that output to the filesystem write into the
// writable layer. Nothing reaches the project directory until CopyDown.
//
// Write runs a formatter on every Go file before staging it. The default
// formatter rejects source that does not parse, so generators must emit
// valid Go.
package overlayfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/ngicks/go-fsys-helper/vroot"
	upstream "github.com/ngicks/go-fsys-helper/vroot-adapter/overlayfs"
	"github.com/ngicks/go-fsys-helper/vroot/memfs"
	"github.com/ngicks/go-fsys-helper/vroot/osfs"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs/goformat"
)

var (
	ErrClosed      = errors.New("overlayfs: closed")
	ErrAlreadyOpen = errors.New("overlayfs: already open")
)

// Formatter formats the content of a Go file. path is the file's absolute
// path in the project directory.
type Formatter interface {
	Format(ctx context.Context, path string, src []byte) ([]byte, error)
}

// Option configures an Fs on open.
type Option func(*Fs)

// WithFormatter sets the formatter Write runs on Go files. The default is
// goformat.Imports, and a nil fm keeps it.
func WithFormatter(fm Formatter) Option {
	return func(f *Fs) {
		if fm != nil {
			f.formatter = fm
		}
	}
}

// Fs is the target filesystem. Its zero value is closed; Open fills it in
// place so a workflow can allocate it as a step's slot.
type Fs struct {
	root string
	// lower is the project directory, opened once. CopyDown writes through
	// it so a rename of the directory after open cannot redirect the write.
	lower     *osfs.Root
	top       *upstream.DataSource
	merged    *upstream.Fs
	formatter Formatter
}

// New opens the project directory root as the lower layer with an empty
// in-memory top.
func New(root string, opts ...Option) (*Fs, error) {
	f := &Fs{}
	if err := f.Open(root, opts...); err != nil {
		return nil, err
	}

	return f, nil
}

// Open opens the project directory root into f, which must be closed.
func (f *Fs) Open(root string, opts ...Option) error {
	if f.merged != nil {
		return ErrAlreadyOpen
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	// An os.Root-backed lower confines traversal and keeps a rename after
	// open from escaping the project directory.
	lowerRoot, err := osfs.NewRoot(root)
	if err != nil {
		return fmt.Errorf("overlayfs: open %q: %w", root, err)
	}
	lower := upstream.NewDataSource(lowerRoot)

	top, err := upstream.NewDataSourceCanonical(memfs.New("top"))
	if err != nil {
		lower.Close()
		return fmt.Errorf("overlayfs: top layer: %w", err)
	}

	merged, err := upstream.New(top, []*upstream.DataSource{lower}, nil)
	if err != nil {
		top.Close()
		lower.Close()
		return fmt.Errorf("overlayfs: mount: %w", err)
	}

	*f = Fs{
		root:      root,
		lower:     lowerRoot,
		top:       top,
		merged:    merged,
		formatter: goformat.Imports{},
	}
	for _, opt := range opts {
		opt(f)
	}
	return nil
}

// Root is the absolute project directory.
func (f *Fs) Root() string { return f.root }

// FS is the merged view: the project with staged output on top.
func (f *Fs) FS() fs.FS { return vroot.ToIoFsRoot(f.merged) }

// Format returns what Write stages for content at name: content formatted
// when name ends in .go, content itself otherwise. It stages nothing.
func (f *Fs) Format(ctx context.Context, name string, content []byte) ([]byte, error) {
	if f.merged == nil {
		return nil, ErrClosed
	}

	if !strings.HasSuffix(name, ".go") {
		return content, nil
	}

	abs := filepath.Join(f.root, filepath.FromSlash(name))
	formatted, err := f.formatter.Format(ctx, abs, content)
	if err != nil {
		return nil, fmt.Errorf("overlayfs: format %q: %w", name, err)
	}
	return formatted, nil
}

// Write stages content at name, a slash-separated path relative to Root.
// The content is formatted by Format first, and a formatter error stages
// nothing.
func (f *Fs) Write(ctx context.Context, name string, content []byte) error {
	content, err := f.Format(ctx, name, content)
	if err != nil {
		return err
	}

	if dir := path.Dir(name); dir != "." {
		if err := f.merged.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return vroot.WriteFile(f.merged, name, content, 0o644)
}

// Overlay returns the staged files keyed by absolute path, the form
// packages.Config.Overlay requires.
func (f *Fs) Overlay() (map[string][]byte, error) {
	out := map[string][]byte{}

	err := f.walkTop(func(name string, content []byte) error {
		out[filepath.Join(f.root, filepath.FromSlash(name))] = content
		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

// CopyDown copies every staged file into the project directory through the
// lower Root opened by New.
func (f *Fs) CopyDown(ctx context.Context) error {
	if f.lower == nil {
		return ErrClosed
	}

	return f.walkTop(func(name string, content []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		if dir := path.Dir(name); dir != "." {
			if err := f.lower.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}

		return vroot.WriteFile(f.lower, name, content, 0o644)
	})
}

// walkTop calls fn for every regular file in the top layer with its path
// relative to Root.
func (f *Fs) walkTop(fn func(name string, content []byte) error) error {
	if f.top == nil {
		return ErrClosed
	}

	topFs := f.top.Fsys()
	// The canonical layout keeps content under one directory; ContentPath of
	// any name reveals it.
	contentRoot := path.Dir(f.top.ContentPath("x"))

	return vroot.WalkDir(
		topFs,
		contentRoot,
		nil,
		func(p, _ string, d fs.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !d.Mode().IsRegular() {
				return nil
			}

			content, err := vroot.ReadFile(topFs, p)
			if err != nil {
				return err
			}

			rel, err := filepath.Rel(contentRoot, p)
			if err != nil {
				return err
			}

			return fn(filepath.ToSlash(rel), content)
		},
	)
}

// Close releases both layers.
func (f *Fs) Close() error {
	if f.merged == nil {
		return nil
	}

	err := f.merged.Close()
	f.merged, f.top, f.lower, f.formatter = nil, nil, nil, nil
	return err
}
