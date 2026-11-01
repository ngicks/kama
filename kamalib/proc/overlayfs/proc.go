// Package overlayfs provides the procs that open and copy down an
// overlayfs.Fs.
package overlayfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() {
	workflow.Register("overlayfs.open", func() workflow.Proc { return &OpenProc{} })
	workflow.Register("overlayfs.copy_down", func() workflow.Proc { return CopyDownProc{} })
}

var fsType = reflect.TypeFor[*overlayfs.Fs]()

// OpenParam configures OpenProc.
type OpenParam struct {
	// Root is the project directory. Empty means the working directory.
	Root string `json:"root"`
}

// OpenProc opens the project directory and offers it to other steps.
//
// A graph constructs one OpenProc, and it holds one filesystem. The first
// step to run opens it, and every later step gets the same one, so files
// staged through any of them reach the final copy down. A later step naming
// another root fails. CleanUp closes the filesystem.
//
// The zero value is ready to use. A graph must have an OpenProc of its own,
// so register a constructor returning a new one rather than a shared value.
//
//	Param(OpenParam) error
//	In()
//	Out()
//	AsInput() *overlayfs.Fs
type OpenProc struct {
	mu   sync.Mutex
	root string
	fs   *overlayfs.Fs
}

func (*OpenProc) ParamType() reflect.Type   { return reflect.TypeFor[OpenParam]() }
func (*OpenProc) InTypes() []reflect.Type   { return nil }
func (*OpenProc) OutTypes() []reflect.Type  { return nil }
func (*OpenProc) AsInputType() reflect.Type { return fsType }

func (p *OpenProc) Instantiate(_ context.Context, dryRun bool) (workflow.Instance, error) {
	return &openInstance{proc: p, dryRun: dryRun}, nil
}

func (p *OpenProc) CleanUp(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.fs == nil {
		return nil
	}

	err := p.fs.Close()
	p.root, p.fs = "", nil
	return err
}

// open returns the filesystem for root, opening it on first use.
func (p *OpenProc) open(root string) (*overlayfs.Fs, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.fs != nil {
		if root != p.root {
			return nil, fmt.Errorf("overlayfs: %q is open, cannot open %q", p.root, root)
		}

		return p.fs, nil
	}

	f, err := overlayfs.New(root)
	if err != nil {
		return nil, err
	}

	p.root, p.fs = root, f
	return f, nil
}

type openInstance struct {
	proc   *OpenProc
	dryRun bool
	root   string
	fs     *overlayfs.Fs
}

func (i *openInstance) Param(p OpenParam) error {
	root := p.Root
	if root == "" {
		root = "."
	}

	if !i.dryRun {
		abs, err := filepath.Abs(root)
		if err != nil {
			return err
		}

		info, err := os.Stat(abs)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("overlayfs: %q is not a directory", abs)
		}

		root = abs
	}

	i.root = root
	return nil
}

func (i *openInstance) In() {}

func (i *openInstance) Out() {}

func (i *openInstance) Run(context.Context) error {
	f, err := i.proc.open(i.root)
	if err != nil {
		return err
	}

	i.fs = f
	return nil
}

func (i *openInstance) AsInput() *overlayfs.Fs { return i.fs }

// CopyDownProc copies every staged file of its in into the project
// directory. The zero value is ready to use.
//
//	Param() error
//	In(*overlayfs.Fs)
//	Out()
type CopyDownProc struct{}

func (CopyDownProc) ParamType() reflect.Type   { return nil }
func (CopyDownProc) InTypes() []reflect.Type   { return []reflect.Type{fsType} }
func (CopyDownProc) OutTypes() []reflect.Type  { return nil }
func (CopyDownProc) AsInputType() reflect.Type { return nil }

func (CopyDownProc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &copyDownInstance{}, nil
}

func (CopyDownProc) CleanUp(context.Context) error { return nil }

type copyDownInstance struct {
	in *overlayfs.Fs
}

func (i *copyDownInstance) Param() error { return nil }

func (i *copyDownInstance) In(in *overlayfs.Fs) { i.in = in }

func (i *copyDownInstance) Out() {}

func (i *copyDownInstance) Run(ctx context.Context) error { return i.in.CopyDown(ctx) }
