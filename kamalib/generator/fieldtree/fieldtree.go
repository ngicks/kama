package fieldtree

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/types"
	"iter"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"

	// The type supports a tree reaches into.
	_ "github.com/ngicks/kama/kamalib/generator/genext/opt"
	_ "github.com/ngicks/kama/kamalib/generator/genext/std"
)

// GeneratorId identifies the generator.
func GeneratorId() base.Id { return base.Id{Namespace: "kama", Name: "fieldtree"} }

// DefaultKey is the struct tag the key paths come from when a target names
// none.
const DefaultKey = "json"

var (
	// ErrDirective is wrapped by the errors about a directive that does not
	// parse or that stands on no single type spec.
	ErrDirective = errors.New("fieldtree: invalid directive")
	// ErrTarget is wrapped by the errors about a target that is no struct
	// type or that names no type.
	ErrTarget = errors.New("fieldtree: invalid target")
	// ErrDuplicateId is wrapped by the warning about an id given to several
	// targets.
	ErrDuplicateId = errors.New("fieldtree: duplicate id")
	// ErrUnknownKey is wrapped by the warning about a keyword of a directive
	// that names nothing.
	ErrUnknownKey = errors.New("fieldtree: unknown key")
)

// Texts maps the id of a target to its tree.
type Texts map[string]string

// Input is the input of Generate.
type Input struct {
	// Pkgs are the packages whose directives Generate reads, as
	// base.SourcePackages picks them. A struct of a package they import
	// is walked too.
	Pkgs []*packages.Package
	// Types maps an id to a target named by type, for a type that carries
	// no directive.
	Types map[string]Target
	// Registry holds the type supports that tell what a named type holds.
	// Nil means none.
	Registry genext.Supports
}

// Target names a struct type by the import path of its package and its
// name joined by a dot, as example.com/app/config.Config, with the options
// a directive gives.
type Target struct {
	Type string `json:"type"`
	// Key is the struct tag the key paths come from. Empty means DefaultKey.
	Key string `json:"key"`
	// Label is the first line of the tree. Empty means the name of the type.
	Label   string         `json:"label"`
	Unknown map[string]any `json:",embed"`
}

// Generate returns the tree of every target of in by id. The package doc
// tells the format.
//
// warn takes the warnings: an unknown keyword of a directive, and an id
// given to several targets. The first target of an id in the order of
// package path, file name and position, then the targets of Types in id
// order, takes the id. The error joins the errors of every target that
// failed. The returned texts hold the trees of the others.
func Generate(in Input, warn func(error)) (Texts, error) {
	out := Texts{}
	found := map[string][]string{}

	var errs []error

	for t, err := range targets(in, warn) {
		if err != nil {
			errs = append(errs, err)
			continue
		}

		found[t.id] = append(found[t.id], t.pos)
		if len(found[t.id]) > 1 {
			continue
		}

		text, err := tree(in, t)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		out[t.id] = text
	}

	for _, id := range slices.Sorted(maps.Keys(found)) {
		if pos := found[id]; len(pos) > 1 {
			warn(fmt.Errorf(
				"%w: %q is given at %s; %s takes it",
				ErrDuplicateId, id, strings.Join(pos, ", "), pos[0],
			))
		}
	}

	return out, errors.Join(errs...)
}

// targets yields the targets of the directives of in, then those of
// in.Types.
func targets(in Input, warn func(error)) iter.Seq2[target, error] {
	return func(yield func(target, error) bool) {
		for t, err := range directiveTargets(in.Pkgs, warn) {
			if !yield(t, err) {
				return
			}
		}

		for _, id := range slices.Sorted(maps.Keys(in.Types)) {
			p := in.Types[id]
			pos := "types." + id

			obj, err := lookupType(in.Pkgs, p.Type)
			if err != nil {
				err = fmt.Errorf("%w: %s: %w", ErrTarget, pos, err)
			}

			if !yield(target{id: id, obj: obj, key: p.Key, label: p.Label, pos: pos}, err) {
				return
			}
		}
	}
}

// lookupType returns the type name key, <import path>.<name>, names among
// pkgs and the packages they import.
func lookupType(pkgs []*packages.Package, key string) (*types.TypeName, error) {
	path, name, ok := splitTypeKey(key)
	if !ok {
		return nil, fmt.Errorf("%q is no <import path>.<type name>", key)
	}

	pkg := loadedPackage(pkgs, path)
	if pkg == nil || pkg.Types == nil {
		return nil, fmt.Errorf("%q: no loaded package has the path %s", key, path)
	}

	obj, ok := pkg.Types.Scope().Lookup(name).(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("%q: %s declares no type %s", key, path, name)
	}

	return obj, nil
}

// splitTypeKey splits key at the last dot after the last slash.
func splitTypeKey(key string) (string, string, bool) {
	slash := strings.LastIndex(key, "/")
	dot := strings.LastIndex(key, ".")

	if dot < slash || dot <= 0 || dot == len(key)-1 {
		return "", "", false
	}

	return key[:dot], key[dot+1:], true
}

// loadedPackage returns the package of path among pkgs and the packages they
// import, or nil.
func loadedPackage(pkgs []*packages.Package, path string) *packages.Package {
	var found *packages.Package

	packages.Visit(pkgs, func(p *packages.Package) bool {
		if found == nil && p.PkgPath == path {
			found = p
		}

		return found == nil
	}, nil)

	return found
}

// sourceFiles yields the syntax trees of pkgs, ordered by package path,
// then by file name.
func sourceFiles(pkgs []*packages.Package) iter.Seq2[*packages.Package, *ast.File] {
	sorted := slices.SortedFunc(slices.Values(pkgs), func(a, b *packages.Package) int {
		return cmp.Compare(a.PkgPath, b.PkgPath)
	})

	return func(yield func(*packages.Package, *ast.File) bool) {
		for _, pkg := range sorted {
			files := slices.SortedFunc(slices.Values(pkg.Syntax), func(a, b *ast.File) int {
				return cmp.Compare(fileName(pkg, a), fileName(pkg, b))
			})

			for _, f := range files {
				if !yield(pkg, f) {
					return
				}
			}
		}
	}
}

func fileName(pkg *packages.Package, f *ast.File) string {
	return pkg.Fset.Position(f.FileStart).Filename
}

// UnknownKeys returns the key paths of the unknown keys of t.
func (t *Target) UnknownKeys() [][]string { return unknownkey.Paths(t.Unknown) }
