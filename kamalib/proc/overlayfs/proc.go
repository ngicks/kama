// Package overlayfs provides the procs that open and copy down an
// overlayfs.Fs.
package overlayfs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs/goformat"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() {
	workflow.Register("overlayfs.open", NewOpenProc)
	workflow.Register("overlayfs.copy_down", NewCopyDownProc)
}

var fsType = reflect.TypeFor[*overlayfs.Fs]()

// OpenParam configures OpenProc.
type OpenParam struct {
	// Root is the project directory. Empty means the working directory.
	Root string `json:"root"`
	// Formatter is the argv of the command that formats Go files on write.
	// It reads the source on stdin and writes the result to stdout, and it
	// runs in Root. Empty means goformat.Imports, in process.
	Formatter []string `json:"formatter"`
}

// OpenProc opens the project directory and offers it to other steps.
//
// A graph constructs one OpenProc, and it holds one filesystem. The first
// step to run opens it, and every later step gets the same one, so files
// staged through any of them reach the final copy down. A later step naming
// another root or formatter fails. CleanUp closes the filesystem.
//
// A graph must have an OpenProc of its own, so register NewOpenProc rather
// than a shared value.
//
//	Param(OpenParam) error
//	In()
//	Out()
//	AsInput() *overlayfs.Fs
type OpenProc struct {
	mu        sync.Mutex
	root      string
	formatter []string
	fs        *overlayfs.Fs
}

// NewOpenProc returns a new OpenProc.
func NewOpenProc() workflow.Proc { return &OpenProc{} }

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
	p.root, p.formatter, p.fs = "", nil, nil
	return err
}

// open returns the filesystem for root, opening it on first use.
func (p *OpenProc) open(root string, formatter []string) (*overlayfs.Fs, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.fs != nil {
		if root != p.root {
			return nil, fmt.Errorf("overlayfs: %q is open, cannot open %q", p.root, root)
		}
		if !slices.Equal(formatter, p.formatter) {
			return nil, fmt.Errorf(
				"overlayfs: %q is open with formatter %q, cannot use %q",
				p.root, p.formatter, formatter,
			)
		}

		return p.fs, nil
	}

	var opts []overlayfs.Option
	if len(formatter) > 0 {
		opts = append(opts, overlayfs.WithFormatter(goformat.Command{Argv: formatter, Dir: root}))
	}

	f, err := overlayfs.New(root, opts...)
	if err != nil {
		return nil, err
	}

	p.root, p.formatter, p.fs = root, formatter, f
	return f, nil
}

type openInstance struct {
	proc      *OpenProc
	dryRun    bool
	root      string
	formatter []string
	fs        *overlayfs.Fs
}

func (i *openInstance) Param(p OpenParam) error {
	root := p.Root
	if root == "" {
		root = "."
	}

	if len(p.Formatter) > 0 && p.Formatter[0] == "" {
		return fmt.Errorf("overlayfs: formatter: %w", goformat.ErrEmptyArgv)
	}

	if !i.dryRun {
		// A path with a separator is resolved against Root when the command
		// runs, so only a bare name can be looked up here.
		if len(p.Formatter) > 0 && !strings.ContainsRune(p.Formatter[0], filepath.Separator) {
			if _, err := exec.LookPath(p.Formatter[0]); err != nil {
				return fmt.Errorf("overlayfs: formatter: %w", err)
			}
		}

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

	i.root, i.formatter = root, p.Formatter
	return nil
}

func (i *openInstance) In() {}

func (i *openInstance) Out() {}

func (i *openInstance) Run(context.Context, workflow.Reporter) error {
	f, err := i.proc.open(i.root, i.formatter)
	if err != nil {
		return err
	}

	i.fs = f
	return nil
}

func (i *openInstance) AsInput() *overlayfs.Fs { return i.fs }

// CopyDownProc copies every staged file of its in into the project
// directory.
//
//	Param() error
//	In(*overlayfs.Fs)
//	Out()
type CopyDownProc struct{}

// NewCopyDownProc returns a new CopyDownProc.
func NewCopyDownProc() workflow.Proc { return CopyDownProc{} }

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

func (i *copyDownInstance) Run(ctx context.Context, _ workflow.Reporter) error {
	return i.in.CopyDown(ctx)
}
