package overlayfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/ngicks/kama/kamalib/workflow"
)

var fsType = reflect.TypeFor[*Fs]()

// OpenParam configures OpenProc.
type OpenParam struct {
	// Root is the project directory. Empty means the working directory.
	Root string `json:"root"`
}

// OpenProc opens the project directory into its out slot.
//
//	Out(*Fs)
type OpenProc struct{}

func (OpenProc) InTypes() []reflect.Type  { return nil }
func (OpenProc) OutTypes() []reflect.Type { return []reflect.Type{fsType} }

func (OpenProc) BuildContext(
	_ context.Context,
	param workflow.Param,
	dryRun bool,
) (workflow.Instance, error) {
	var p OpenParam
	if err := param.Decode(&p); err != nil {
		return nil, err
	}

	root := p.Root
	if root == "" {
		root = "."
	}

	if !dryRun {
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}

		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("overlayfs: %q is not a directory", abs)
		}

		root = abs
	}

	return &openInstance{root: root}, nil
}

type openInstance struct {
	root string
	out  *Fs
}

func (i *openInstance) Out(out *Fs) { i.out = out }

func (i *openInstance) Run(context.Context) error { return i.out.Open(i.root) }

// CopyDownProc copies every staged file of its in into the project
// directory.
//
//	In(*Fs)
type CopyDownProc struct{}

func (CopyDownProc) InTypes() []reflect.Type  { return []reflect.Type{fsType} }
func (CopyDownProc) OutTypes() []reflect.Type { return nil }

func (CopyDownProc) BuildContext(
	_ context.Context,
	param workflow.Param,
	_ bool,
) (workflow.Instance, error) {
	if err := param.Decode(&struct{}{}); err != nil {
		return nil, err
	}

	return &copyDownInstance{}, nil
}

type copyDownInstance struct {
	in *Fs
}

func (i *copyDownInstance) In(in *Fs) { i.in = in }

func (i *copyDownInstance) Run(ctx context.Context) error { return i.in.CopyDown(ctx) }
