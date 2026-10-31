// Package gopackages loads Go packages through golang.org/x/tools/go/packages
// with the target filesystem's staged files applied as an overlay.
package gopackages

import (
	"context"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// Mode is the load mode Load uses: enough for type-checked syntax trees.
const Mode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo |
	packages.NeedModule

// Options configures Load.
type Options struct {
	// Patterns are package patterns.
	Patterns []string
	// BuildFlags are passed to the build system, for example "-tags=foo".
	BuildFlags []string
	// Tests also loads test packages.
	Tests bool
}

// Load loads the packages opts names from the project fsys shows, staged
// files included.
func Load(ctx context.Context, fsys *overlayfs.Fs, opts Options) ([]*packages.Package, error) {
	overlay, err := fsys.Overlay()
	if err != nil {
		return nil, err
	}

	cfg := &packages.Config{
		Context:    ctx,
		Mode:       Mode,
		Dir:        fsys.Root(),
		BuildFlags: opts.BuildFlags,
		Tests:      opts.Tests,
		Overlay:    overlay,
	}

	return packages.Load(cfg, opts.Patterns...)
}
