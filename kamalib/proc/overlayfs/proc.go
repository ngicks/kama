// Package overlayfs provides the procs that open and copy down an
// overlayfs.Fs.
package overlayfs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/gopackages"
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

// CopyDownParam configures CopyDownProc.
type CopyDownParam struct {
	// FastIgnoreCompileErrors skips the check that the staged Go files
	// compile. The step gets faster, and staged code that does not compile
	// reaches the project directory.
	FastIgnoreCompileErrors bool `json:"fast_ignore_compile_errors"`
	// Patterns are the package patterns the check loads. Empty means
	// "./...".
	Patterns []string `json:"patterns"`
	// BuildFlags are passed to the build system by the check, for example
	// "-tags=foo".
	BuildFlags []string `json:"build_flags"`
	// Tests makes the check load test packages too.
	Tests bool `json:"tests"`
}

// CopyDownProc copies every staged file of its in into the project
// directory.
//
// It first checks that the staged Go files compile: it loads the packages
// of the project with the staged files on top, as gopackages.Load does, and
// fails without copying anything when a loaded package has an error. The
// formatter of the filesystem rejects only source that does not parse, so
// this check is what keeps code with type errors off the disk. The check is
// skipped when no Go file is staged, and when
// CopyDownParam.FastIgnoreCompileErrors is set.
//
//	Param(CopyDownParam) error
//	In(*overlayfs.Fs)
//	Out()
type CopyDownProc struct{}

// NewCopyDownProc returns a new CopyDownProc.
func NewCopyDownProc() workflow.Proc { return CopyDownProc{} }

func (CopyDownProc) ParamType() reflect.Type   { return reflect.TypeFor[CopyDownParam]() }
func (CopyDownProc) InTypes() []reflect.Type   { return []reflect.Type{fsType} }
func (CopyDownProc) OutTypes() []reflect.Type  { return nil }
func (CopyDownProc) AsInputType() reflect.Type { return nil }

func (CopyDownProc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &copyDownInstance{}, nil
}

func (CopyDownProc) CleanUp(context.Context) error { return nil }

type copyDownInstance struct {
	param CopyDownParam
	in    *overlayfs.Fs
}

func (i *copyDownInstance) Param(p CopyDownParam) error {
	if slices.Contains(p.Patterns, "") {
		return errors.New("overlayfs: copy down: empty pattern")
	}

	if len(p.Patterns) == 0 {
		p.Patterns = []string{"./..."}
	}

	i.param = p

	return nil
}

func (i *copyDownInstance) In(in *overlayfs.Fs) { i.in = in }

func (i *copyDownInstance) Out() {}

func (i *copyDownInstance) Run(ctx context.Context, _ workflow.Reporter) error {
	if !i.param.FastIgnoreCompileErrors {
		if err := i.checkCompiles(ctx); err != nil {
			return err
		}
	}

	return i.in.CopyDown(ctx)
}

// checkCompiles fails when a package of the project, staged files included,
// has an error.
func (i *copyDownInstance) checkCompiles(ctx context.Context) error {
	staged, err := i.in.Overlay()
	if err != nil {
		return err
	}

	// A graph staging no Go file may run on a directory that is not a Go
	// module, where a load fails.
	stagesGo := opiter.WrapSeq(maps.Keys(staged)).
		Any(func(name string) bool { return strings.HasSuffix(name, ".go") })

	if !stagesGo {
		return nil
	}

	_, err = gopackages.Load(ctx, i.in, gopackages.Options{
		Patterns:   i.param.Patterns,
		BuildFlags: i.param.BuildFlags,
		Tests:      i.param.Tests,
	})
	if err != nil {
		return fmt.Errorf("overlayfs: copy down: staged files do not compile: %w", err)
	}

	return nil
}
