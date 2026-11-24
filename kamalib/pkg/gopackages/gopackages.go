// Package gopackages loads Go packages through golang.org/x/tools/go/packages
// with the target filesystem's staged files applied as an overlay.
package gopackages

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ngicks/opiter"
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
//
// Unlike packages.Load, Load fails if any loaded package, dependencies
// included, has errors. The returned error then wraps PackageErrors.
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

	pkgs, err := packages.Load(cfg, opts.Patterns...)
	if err != nil {
		return nil, err
	}

	errs := PackageErrors{}

	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			// With Tests, a package and its test variants share PkgPath and
			// report the same errors.
			if !slices.Contains(errs[p.PkgPath], e) {
				errs[p.PkgPath] = append(errs[p.PkgPath], e)
			}
		}
	})

	if len(errs) > 0 {
		return nil, fmt.Errorf("gopackages: %w", errs)
	}

	return pkgs, nil
}

// IsTestVariant reports whether p is the test variant of a package, which
// Load gives with Options.Tests: the package compiled together with its
// _test.go files declaring the same package. Its ID names the test binary
// after the path, as in "x [x.test]".
func IsTestVariant(p *packages.Package) bool {
	return p.ID == p.PkgPath+" ["+p.PkgPath+".test]"
}

// IsExternalTest reports whether p is an external test package, which Load
// gives with Options.Tests: the _test.go files of the directory of the
// package x declaring the package x_test, with the ID "x_test [x.test]".
func IsExternalTest(p *packages.Package) bool {
	tested, ok := strings.CutSuffix(p.PkgPath, "_test")
	return ok && p.ID == p.PkgPath+" ["+tested+".test]"
}

// IsTestMain reports whether p is the main package of a test binary, which
// Load gives with Options.Tests, as "x.test". It is generated, lies in the
// build cache and shares the directory of x. A package main of a path
// ending in .test written by hand is taken for one too.
func IsTestMain(p *packages.Package) bool {
	return p.Name == "main" && p.ID == p.PkgPath && strings.HasSuffix(p.PkgPath, ".test")
}

// PackageErrors maps a package path to the errors reported for that package.
type PackageErrors map[string][]packages.Error

func (e PackageErrors) Error() string {
	return strings.Join(
		opiter.WrapSeq2(opiter.MapsSorted(e)).
			Assemble(packageErrorBlock).
			CollectSlice(),
		"\n",
	)
}

func packageErrorBlock(path string, errs []packages.Error) string {
	return strings.Join(
		opiter.WrapSeq(opiter.Once(path+":")).
			Concat(
				opiter.WrapSeq(slices.Values(errs)).
					Map(packages.Error.Error),
			).
			CollectSlice(),
		"\n\t",
	)
}
