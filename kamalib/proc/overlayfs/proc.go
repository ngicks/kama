// Package overlayfs provides the procs that open and copy down an
// overlayfs.Fs.
package overlayfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() {
	workflow.Register("overlayfs.open", &OpenProc{})
	workflow.Register("overlayfs.copy_down", CopyDownProc{})
}

var fsType = reflect.TypeFor[*overlayfs.Fs]()

// OpenParam configures OpenProc.
type OpenParam struct {
	// Root is the project directory. Empty means the working directory.
	Root string `json:"root"`
}

// OpenProc opens the project directory and offers it to other steps. Close
// closes every filesystem its instances opened.
//
//	Param(OpenParam) error
//	In()
//	Out()
//	AsInput() *overlayfs.Fs
type OpenProc struct {
	mu     sync.Mutex
	opened []*overlayfs.Fs
}

func (*OpenProc) ParamType() reflect.Type   { return reflect.TypeFor[OpenParam]() }
func (*OpenProc) InTypes() []reflect.Type   { return nil }
func (*OpenProc) OutTypes() []reflect.Type  { return nil }
func (*OpenProc) AsInputType() reflect.Type { return fsType }

func (p *OpenProc) Instantiate(_ context.Context, dryRun bool) (workflow.Instance, error) {
	f := &overlayfs.Fs{}

	// A dry-run instance never opens its filesystem, so there is nothing to
	// close.
	if !dryRun {
		p.mu.Lock()
		p.opened = append(p.opened, f)
		p.mu.Unlock()
	}

	return &openInstance{dryRun: dryRun, fs: f}, nil
}

func (p *OpenProc) Close(context.Context) error {
	p.mu.Lock()
	opened := p.opened
	p.opened = nil
	p.mu.Unlock()

	var errs []error
	for _, f := range opened {
		errs = append(errs, f.Close())
	}

	return errors.Join(errs...)
}

type openInstance struct {
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

func (i *openInstance) Run(context.Context) error { return i.fs.Open(i.root) }

func (i *openInstance) AsInput() *overlayfs.Fs { return i.fs }

// CopyDownProc copies every staged file of its in into the project
// directory.
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

func (CopyDownProc) Close(context.Context) error { return nil }

type copyDownInstance struct {
	in *overlayfs.Fs
}

func (i *copyDownInstance) Param() error { return nil }

func (i *copyDownInstance) In(in *overlayfs.Fs) { i.in = in }

func (i *copyDownInstance) Out() {}

func (i *copyDownInstance) Run(ctx context.Context) error { return i.in.CopyDown(ctx) }
