// Package overlayfs is the rooted target filesystem a graph runs against.
//
// It stacks a writable in-memory layer over the project directory. Nodes read
// the merged view, so generated output is visible to later nodes as if it
// were on disk, and the sink node writes routed files into the writable
// layer. Nothing reaches the project directory until CopyDown.
package overlayfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/ngicks/go-fsys-helper/vroot"
	upstream "github.com/ngicks/go-fsys-helper/vroot-adapter/overlayfs"
	"github.com/ngicks/go-fsys-helper/vroot/memfs"
	"github.com/ngicks/go-fsys-helper/vroot/osfs"

	"github.com/ngicks/kama/kama/workflow"
)

var ErrClosed = errors.New("overlayfs: closed")

// Fs is the target filesystem value. It is provided by Provide, read by
// loaders, written by Sink, and flushed by the caller once the graph
// succeeded.
type Fs struct {
	root   string
	top    *upstream.DataSource
	merged *upstream.Fs
}

// DefaultNode returns nil: the project root must be given, so a graph
// provides *Fs explicitly.
func (*Fs) DefaultNode() workflow.Node { return nil }

// New opens the project directory root as the read-only lower layer with an
// empty in-memory top.
func New(root string) (*Fs, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	// An os.Root-backed lower confines traversal and keeps a rename after
	// open from escaping the project directory.
	lowerRoot, err := osfs.NewRoot(root)
	if err != nil {
		return nil, fmt.Errorf("overlayfs: open %q: %w", root, err)
	}
	lower := upstream.NewDataSource(lowerRoot)

	top, err := upstream.NewDataSourceCanonical(memfs.New("top"))
	if err != nil {
		lower.Close()
		return nil, fmt.Errorf("overlayfs: top layer: %w", err)
	}

	merged, err := upstream.New(top, []*upstream.DataSource{lower}, nil)
	if err != nil {
		top.Close()
		lower.Close()
		return nil, fmt.Errorf("overlayfs: mount: %w", err)
	}

	return &Fs{root: root, top: top, merged: merged}, nil
}

// Root is the absolute project directory.
func (f *Fs) Root() string { return f.root }

// FS is the merged view: the project with staged output on top.
func (f *Fs) FS() fs.FS { return vroot.ToIoFsRoot(f.merged) }

// Write stages content at name, a slash-separated path relative to Root.
func (f *Fs) Write(name string, content []byte) error {
	if f.merged == nil {
		return ErrClosed
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

// CopyDown copies every staged file into the project directory.
func (f *Fs) CopyDown(ctx context.Context) error {
	dst, err := os.OpenRoot(f.root)
	if err != nil {
		return err
	}
	defer dst.Close()

	return f.walkTop(func(name string, content []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		if dir := path.Dir(name); dir != "." {
			if err := dst.MkdirAll(filepath.FromSlash(dir), 0o755); err != nil {
				return err
			}
		}

		return dst.WriteFile(filepath.FromSlash(name), content, 0o644)
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
	f.merged, f.top = nil, nil
	return err
}
